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



// NICを指定してコンテナを1個デプロイする
func setup_container_on_iface(baseContainerName string, service ServiceInfo, dnsOptionString string, iface string) error {
	// コンテナ名はNICごとにユニークにする（同名で2回docker runできないため）
	containerName := fmt.Sprintf("%s-%s", baseContainerName, iface)

	fmt.Printf("Service Name: %s, IP: %s, Node: %s, IFACE: %s\n", containerName, service.IP, service.Node, iface)

	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("unable to get user: %w", err)
	}

	filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
	key, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("unable to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("unable to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: usr.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意: 実運用では危険
	}

	sshServer := fmt.Sprintf("%s:22", service.Node)
	client, err := ssh.Dial("tcp", sshServer, config)
	if err != nil {
		return fmt.Errorf("unable to connect ssh: %w", err)
	}
	defer client.Close()

	// 既に同名コンテナが居たら消す（必要なら外してOK）
	_, _ = session_and_command(fmt.Sprintf("sudo docker rm -f %s >/dev/null 2>&1 || true", containerName), client)

	// docker run（UDPWRAP_IFACEをifaceに）
	commandRun := fmt.Sprintf(
		"sudo docker run --restart unless-stopped --cap-add=NET_RAW --network none %s --name %s -d "+
			"-e UDPWRAP_DEBUG=1 -e UDPWRAP_IFACE=%s -e L2_PACKET_PIN_THREADS=1 -e UDPWRAP_IFACE_WAIT_MS=5000 "+
			"kawanotatsuya/%s",
		dnsOptionString, containerName, iface, baseContainerName,
	)
	outputRun, err := session_and_command(commandRun, client)
	if err != nil {
		return err
	}
	fmt.Println(string(outputRun))

	// PID取得（改行/空白をTrim）
	commandPID := fmt.Sprintf("sudo docker inspect -f '{{.State.Pid}}' %s", containerName)
	pidBytes, err := session_and_command(commandPID, client)
	if err != nil {
		return err
	}
	pidStr := strings.TrimSpace(string(pidBytes))
	fmt.Println("PID:", pidStr)

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("cast error: %w", err)
	}

	// NICをコンテナのnetnsへ移動
	commandMove := fmt.Sprintf("sudo ip link set %s netns %d", iface, pid)
	if _, err := session_and_command(commandMove, client); err != nil {
		return err
	}

	// IP付与（devもifaceに）
	/*commandIP := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev %s", pid, service.IP, iface)
	if _, err := session_and_command(commandIP, client); err != nil {
		return err
	}
	*/
	// IF up
	commandUp := fmt.Sprintf("sudo nsenter -t %d -n ip link set %s up", pid, iface)
	if _, err := session_and_command(commandUp, client); err != nil {
		return err
	}

	return nil
}

// enp2s0f0 と enp2s0f1 にそれぞれ1個ずつ同じイメージ（baseContainerName）をデプロイ
func setup_container_on_both_ifaces(baseContainerName string, service ServiceInfo, dnsOptionString string) error {
	if err := setup_container_on_iface(baseContainerName, service, dnsOptionString, "enp2s0f0"); err != nil {
		return err
	}
	if err := setup_container_on_iface(baseContainerName, service, dnsOptionString, "enp2s0f1"); err != nil {
		return err
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
		err := setup_container_on_both_ifaces(container_name, service, dnsOptionString)
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
                err := setup_container_on_both_ifaces(container_name, service, dnsOptionString)
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
                err := setup_container_on_both_ifaces(container_name, service, dnsOptionString)
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
