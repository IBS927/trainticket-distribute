package proxy_less

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"os/user"
	"golang.org/x/crypto/ssh"
)

type ServiceInfo struct {
	IP   string `json:"ip"`
	Node string `json:"node"`
	Port string `json:"port"`
}

func session_and_command(command string, client *ssh.Client) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %s", err)
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	if err != nil {
		return nil, fmt.Errorf("failer to execute command: %s, error: %w", command, err)
	}
	return output, nil
}

func setup_container(baseContainerName string, service ServiceInfo, dnsOptionString string) error {
	fmt.Printf("Base Service Name: %s, IP: %s, Node: %s\n",
		baseContainerName, service.IP, service.Node)
	numContainers := 8
	// IPを分解して末尾を数値化
	parts := strings.Split(service.IP, ".")
	if len(parts) != 4 {
		return fmt.Errorf("invalid IP: %s", service.IP)
	}

	baseLast, err := strconv.Atoi(parts[3])
	if err != nil {
		return fmt.Errorf("invalid last part of IP: %s (%v)", parts[3], err)
	}

	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("unable to get user:%s", err)
	}

	filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
	key, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("unable to read private key:%s", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("unable to parse private key:%s", err)
	}

	config := &ssh.ClientConfig{
		User: usr.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshServer := fmt.Sprintf("%s:22", service.Node)
	client, err := ssh.Dial("tcp", sshServer, config)
	if err != nil {
		return fmt.Errorf("unable to connect ssh:%s", err)
	}
	defer client.Close()

	// ここからコンテナを複数作る
	for i := 0; i < numContainers; i++ {
		last := baseLast + i
		if last < 0 || last > 255 {
			return fmt.Errorf("lastPart out of range: %d", last)
		}

		// 各コンテナの情報（IP末尾だけ増加）
		ipParts := make([]string, 4)
		copy(ipParts, parts)
		ipParts[3] = strconv.Itoa(last)
		ip := strings.Join(ipParts, ".")

		// veth/eth suffix
		ifSuffix := strconv.Itoa(last)

		// コンテナ名はユニークに
		containerName := fmt.Sprintf("%s-%s", baseContainerName, ifSuffix)

		fmt.Printf("Creating container: %s, IP: %s, Node: %s\n", containerName, ip, service.Node)

		// veth pair 作成
		command3 := fmt.Sprintf("sudo ip link add veth%s type veth peer name eth%s", ifSuffix, ifSuffix)
		output3, err := session_and_command(command3, client)
		if err != nil {
			return fmt.Errorf("unable to create veth pair for %s: %v", containerName, err)
		}
		fmt.Println(string(output3))

		// docker run
		command2 := fmt.Sprintf(
			"sudo docker run --restart unless-stopped --cap-add=NET_RAW --network none %s --name %s -d "+
				"-e UDPWRAP_DEBUG=1 -e UDPWRAP_SKIP_MAIN_DRAIN=1 -e UDPWRAP_FANOUT=LB "+
				"-e UDPWRAP_IFACE=veth%s -e UDPWRAP_IFACE_WAIT_MS=5000 "+
				"kawanotatsuya/%s",
			dnsOptionString, containerName, ifSuffix, baseContainerName,
		)
		output2, err := session_and_command(command2, client)
		if err != nil {
			return fmt.Errorf("docker run failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output2))

		// PID取得
		command4 := fmt.Sprintf("sudo docker inspect -f '{{.State.Pid}}' %s", containerName)
		pidBytes, err := session_and_command(command4, client)
		if err != nil {
			return fmt.Errorf("unable to get pid for %s: %v", containerName, err)
		}

		pidStr := strings.TrimSpace(string(pidBytes))
		pidInt, err := strconv.Atoi(pidStr)
		ns_path := fmt.Sprintf("/proc/%d/ns/net", pidInt)
                fmt.Println(ns_path)
		if err != nil {
			return fmt.Errorf("cast error pid=%q for %s: %v", pidStr, containerName, err)
		}

		// veth をコンテナのnetnsへ
		command5 := fmt.Sprintf("sudo ip link set veth%s netns %d", ifSuffix, pidInt)
		output5, err := session_and_command(command5, client)
		if err != nil {
			return fmt.Errorf("ip link set netns failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output5))

		// （必要なら）コンテナ側にIP付与：元コードではコメントアウトされていたので、ここもコメントのまま残します
		// command6 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%s", pidInt, ip, ifSuffix)
		// output6, err := session_and_command(command6, client)
		// if err != nil {
		// 	return fmt.Errorf("ip addr add failed for %s: %v", containerName, err)
		// }
		// fmt.Println(string(output6))

		// コンテナ側 veth up
		command7 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%s up", pidInt, ifSuffix)
		output7, err := session_and_command(command7, client)
		if err != nil {
			return fmt.Errorf("ip link set up (netns) failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output7))

		// ホスト側 eth を bridge へ
		command8 := fmt.Sprintf("sudo ip link set eth%s master my_bridge", ifSuffix)
		output8, err := session_and_command(command8, client)
		if err != nil {
			return fmt.Errorf("ip link set master failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output8))

		// ホスト側 eth up
		command9 := fmt.Sprintf("sudo ip link set eth%s up", ifSuffix)
		output9, err := session_and_command(command9, client)
		if err != nil {
			return fmt.Errorf("ip link set up (host) failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output9))

		command10 := fmt.Sprintf("sudo nsenter -t %d -n ethtool -L veth%s rx 8 tx 8", pidInt, ifSuffix)
                output10, err := session_and_command(command10, client)
                if err != nil {
                        return fmt.Errorf("ip link set up (host) failed for %s: %v", containerName, err)
                }
                fmt.Println(string(output10))

		command11 := fmt.Sprintf("sudo ethtool -L eth%s rx 8 tx 8", ifSuffix)
                output11, err := session_and_command(command11, client)
                if err != nil {
                        return fmt.Errorf("ethtool -L rx 8 tx 8 failed for %s: %v", containerName, err)
                }
                fmt.Println(string(output11))
	}

	return nil
}

func ProxyLessHandler(w http.ResponseWriter, r *http.Request) {
	// HTTP GETリクエスト
	resp, err := http.Get("http://localhost:8000/all")
	if err != nil {
		fmt.Println("Error fetching URL:", err)
		return
	}
	defer resp.Body.Close()

	// レスポンスのボディを読み取り
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	var services map[string]ServiceInfo
	err = json.Unmarshal(body, &services)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	var dnsOptions []string
   	for name, info := range services {
        	dnsOptions = append(dnsOptions, fmt.Sprintf("--add-host %s:%s", name, info.IP))
    	}
    	dnsOptionString := strings.Join(dnsOptions, " ")

	for container_name, service := range services {
		err := setup_container(container_name, service, dnsOptionString)
		if err != nil {
			fmt.Println("unable to set up container:", err)
			return
		}
	}
}

func MysqlProxyLessHandler(w http.ResponseWriter, r *http.Request) {
        // HTTP GETリクエスト
        resp, err := http.Get("http://localhost:8000/all")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer resp.Body.Close()

        // レスポンスのボディを読み取り
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var services map[string]ServiceInfo
        err = json.Unmarshal(body, &services)
        if err != nil {
                fmt.Println("Error parsing JSON:", err)
                return
        }

        var dnsOptions []string
        for name, info := range services {
                dnsOptions = append(dnsOptions, fmt.Sprintf("--add-host %s:%s", name, info.IP))
        }
        dnsOptionString := strings.Join(dnsOptions, " ")

	resp, err = http.Get("http://localhost:8000/all_mysql")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer resp.Body.Close()

        // レスポンスのボディを読み取り
        body, err = ioutil.ReadAll(resp.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var services_mysql map[string]ServiceInfo
        err = json.Unmarshal(body, &services_mysql)
        if err != nil {
                fmt.Println("Error parsing JSON:", err)
                return
        }

        for container_name, service := range services_mysql {
                err := setup_container(container_name, service, dnsOptionString)
                if err != nil {
                        fmt.Println("unable to set up container:", err)
                        return
                }
	}
}
func ServiceProxyLessHandler(w http.ResponseWriter, r *http.Request) {
        // HTTP GETリクエスト
        resp, err := http.Get("http://localhost:8000/all")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer resp.Body.Close()

        // レスポンスのボディを読み取り
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var services map[string]ServiceInfo
        err = json.Unmarshal(body, &services)
        if err != nil {
                fmt.Println("Error parsing JSON:", err)
                return
        }

        var dnsOptions []string
        for name, info := range services {
                dnsOptions = append(dnsOptions, fmt.Sprintf("--add-host %s:%s", name, info.IP))
        }
        dnsOptionString := strings.Join(dnsOptions, " ")

        resp, err = http.Get("http://localhost:8000/all_service")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer resp.Body.Close()
	// レスポンスのボディを読み取り
        body, err = ioutil.ReadAll(resp.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var services_no_mysql map[string]ServiceInfo
        err = json.Unmarshal(body, &services_no_mysql)
        if err != nil {
                fmt.Println("Error parsing JSON:", err)
                return
        }

        for container_name, service := range services_no_mysql {
                err := setup_container(container_name, service, dnsOptionString)
                if err != nil {
                        fmt.Println("unable to set up container:", err)
                        return
                }
        }
}
		/*fmt.Printf("Service Name: %s, IP: %s, Node: %s\n", container_name, service.IP, service.Node)
		parts := strings.Split(service.IP, ".")
		lastPart := parts[len(parts)-1]
		// 秘密鍵を読み込む
		key, err := ioutil.ReadFile("/home/appleuser/.ssh/id_rsa")
		if err != nil {
			fmt.Println("unable to read private key:", err)
			return
		}

		// 秘密鍵をSignerにパースする
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			fmt.Println("unable to parse private key:", err)
			return
		}

		// SSHクライアント設定
		config := &ssh.ClientConfig{
			User: "appleuser",
			Auth: []ssh.AuthMethod{
				ssh.PublicKeys(signer),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意: 実際の運用では安全ではない
		}

		// SSHサーバに接続
		ssh_server := fmt.Sprintf("%s:22", service.Node)
		client, err := ssh.Dial("tcp", ssh_server, config)
		if err != nil {
			fmt.Println("unable to connect ssh:", err)
			return
		}
		defer client.Close()

		// コマンドを実行
		command1 := fmt.Sprintf("sudo docker pull kawanotatsuya/%s", container_name)
		output, err := session_and_command(command1, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			//return
		}
		fmt.Println(string(output))

		command2 := fmt.Sprintf("sudo docker run --restart unless-stopped --network none %s --name %s -d kawanotatsuya/%s",dnsOptionString, container_name, container_name)
		output_2, err := session_and_command(command2, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_2))

		command3 := fmt.Sprintf("sudo ip link add veth%s type veth peer name eth%s", lastPart, lastPart)
		output_3, err := session_and_command(command3, client)
		if err != nil {
			fmt.Println("unable to cretae session or execute command:", err)
			return
		}
		fmt.Println(string(output_3))

		command4 := fmt.Sprintf("echo $(sudo docker inspect -f '{{.State.Pid}}' %s)", container_name)
		pid, err := session_and_command(command4, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		pid_s := string(pid)
		fmt.Println(pid_s)
		pid_d, err := strconv.Atoi(pid_s[:len(pid_s)-1])
		if err != nil {
			fmt.Println("cast error:", err)
			return
		}

		ns_path := fmt.Sprintf("/proc/%d/ns/net", pid_d)
		fmt.Println(ns_path)
		command5 := fmt.Sprintf("sudo ip link set veth%s netns %d", lastPart, pid_d)
		output_5, err := session_and_command(command5, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_5))

		command6 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%s", pid_d, service.IP, lastPart)
		output_6, err := session_and_command(command6, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_6))

		command7 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%s up", pid_d, lastPart)
		output_7, err := session_and_command(command7, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_7))

		command8 := fmt.Sprintf("sudo ip link set eth%s master my_bridge", lastPart)
		output_8, err := session_and_command(command8, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_8))

		command9 := fmt.Sprintf("sudo ip link set eth%s up", lastPart)
		output_9, err := session_and_command(command9, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_9))
	}

}*/

func ProxyLessDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// HTTP GETリクエスト
	resp, err := http.Get("http://localhost:8000/all")
	if err != nil {
		fmt.Println("Error fetching URL:", err)
		return
	}
	defer resp.Body.Close()

	// レスポンスのボディを読み取り
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	var services map[string]ServiceInfo
	err = json.Unmarshal(body, &services)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	for container_name, service := range services {
		fmt.Printf("Service Name: %s, IP: %s, Node: %s\n", container_name, service.IP, service.Node)
		usr, err := user.Current()
    		if err != nil {
			fmt.Println("unable to get user:%s", err)
			return
    		}
		// 秘密鍵を読み込む
		filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
		key, err := ioutil.ReadFile(filePath)
		if err != nil {
			fmt.Println("unable to read private key:", err)
			return
		}

		// 秘密鍵をSignerにパースする
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			fmt.Println("unable to parse private key:", err)
			return
		}

		// SSHクライアント設定
		config := &ssh.ClientConfig{
			User: usr.Username,
			Auth: []ssh.AuthMethod{
				ssh.PublicKeys(signer),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意: 実際の運用では安全ではない
		}

		// SSHサーバに接続
		ssh_server := fmt.Sprintf("%s:22", service.Node)
		client, err := ssh.Dial("tcp", ssh_server, config)
		if err != nil {
			fmt.Println("unable to connect ssh:", err)
			return
		}
		defer client.Close()

		// コマンドを実行
		command1 := fmt.Sprintf("sudo docker stop %s", container_name)
		output, err := session_and_command(command1, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
		}
		fmt.Println(string(output))

		command2 := fmt.Sprintf("sudo docker rm %s", container_name)
		output_2, err := session_and_command(command2, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
		}
		fmt.Println(string(output_2))
	}
}
