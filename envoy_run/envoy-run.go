package envoy_run

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/user"
	"net/http"
	"strings"
	"strconv"
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

func Envoy_Run_Handler(w http.ResponseWriter, r *http.Request) {
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
		fmt.Printf("Service Name: %s, IP: %s, Node: %s\n", container_name, service.IP, service.Node)
		parts := strings.Split(service.IP, ".")
		lastPart := parts[len(parts)-1]
		n ,err := strconv.Atoi(lastPart)
		if err != nil {
			fmt.Println("failed to cast:", err)
			return
		}
		n_envoy := n+100
		IP_envoy := fmt.Sprintf("%s.%s.%s.%d",parts[0],parts[1],parts[2],n_envoy)
		// 秘密鍵を読み込む
		usr, err := user.Current()
   		if err != nil {
         		fmt.Println("unable to get user:%s",err)
			return
		}

    		// ユーザーのホームディレクトリにあるSSHキーのパスを作成
    		filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)

    		// ファイルを読み込む
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

		command1 := fmt.Sprintf("sudo docker pull kawanotatsuya/%s_trace", container_name)
		output_1, err := session_and_command(command1, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_1))

		command2 := fmt.Sprintf("sudo docker pull kawanotatsuya/%s_envoy", container_name)
		output_2, err := session_and_command(command2, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_2))

		command3 := fmt.Sprintf("sudo docker run --network none %s --name %s -d kawanotatsuya/%s_trace",dnsOptionString, container_name, container_name)
		output_3, err := session_and_command(command3, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_3))

		command4 := fmt.Sprintf("sudo docker run --network none %s --name %s_envoy -d kawanotatsuya/%s_envoy",dnsOptionString, container_name, container_name)
		output_4, err := session_and_command(command4, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_4))

		command5 := fmt.Sprintf("sudo ip link add veth%d type veth peer name eth%d", n, n)
		output_5, err := session_and_command(command5, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_5))

		command6 := fmt.Sprintf("sudo ip link add veth%d type veth peer name eth%d", n_envoy, n_envoy)
		output_6, err := session_and_command(command6, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_6))

		command7 := fmt.Sprintf("echo $(sudo docker inspect -f '{{.State.Pid}}' %s)", container_name)
		pid, err := session_and_command(command7, client)
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

		command7_2 := fmt.Sprintf("sudo ip link set veth%d netns %d", n, pid_d)
		output_7_2, err := session_and_command(command7_2,client)
		if err != nil {
			fmt.Println("unable to create session or execute command", err)
			return
		}
		fmt.Println(string(output_7_2))
		command8 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%d", pid_d, service.IP, n)
		output_8, err := session_and_command(command8, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_8))

		command9 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%d up", pid_d, n)
		output_9, err := session_and_command(command9, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_9))

		command10 := fmt.Sprintf("sudo ip link set eth%d master my_bridge", n)
		output_10, err := session_and_command(command10, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_10))

		command11 := fmt.Sprintf("sudo ip link set eth%d up", n)
		output_11, err := session_and_command(command11, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_11))

		command12 := fmt.Sprintf("echo $(sudo docker inspect -f '{{.State.Pid}}' %s_envoy)", container_name)
		pid_envoy, err := session_and_command(command12, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(pid_envoy))

		pid_s_envoy := string(pid_envoy)
		fmt.Println(pid_s_envoy)
		pid_d_envoy, err := strconv.Atoi(pid_s_envoy[:len(pid_s_envoy)-1])
		if err != nil {
			fmt.Println("cast error:", err)
			return
		}
		ns_path_envoy := fmt.Sprintf("/proc/%d/ns/net", pid_d_envoy)
		fmt.Println(ns_path_envoy)

		command13 := fmt.Sprintf("sudo ip link set veth%d netns %d", n_envoy, pid_d_envoy)
		output_13, err := session_and_command(command13, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_13))

		command14 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%d", pid_d_envoy, IP_envoy, n_envoy)
		output_14, err := session_and_command(command14, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_14))

		command15 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%d up", pid_d_envoy, n_envoy)
		output_15, err := session_and_command(command15, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_15))

		command16 := fmt.Sprintf("sudo ip link set eth%d master my_bridge", n_envoy)
		output_16, err := session_and_command(command16, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_16))

		command17 := fmt.Sprintf("sudo ip link set eth%d up", n_envoy)
		output_17, err := session_and_command(command17, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_17))

		command18 := fmt.Sprintf("sudo iptables -t nat -I PREROUTING -p tcp -d %s --dport %s -j DNAT --to-destination %s:15006", service.IP, service.Port, IP_envoy)
		output_18, err := session_and_command(command18, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_18))

		command19 := fmt.Sprintf("sudo iptables -t nat -I PREROUTING -p tcp -s %s -d %s --dport %s -j DNAT --to-destination %s:%s", IP_envoy, service.IP, service.Port, service.IP, service.Port)
		output_19, err := session_and_command(command19, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_19))

		command20 := fmt.Sprintf("sudo iptables -t nat -I PREROUTING -p tcp -s %s -j DNAT --to-destination %s:15001", service.IP, IP_envoy)
		output_20, err := session_and_command(command20, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_20))

		output_21, err := session_and_command("sudo iptables -t nat -I PREROUTING -p tcp --dport 3306 -j RETURN", client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_21))
	}
}
func Envoy_Del_Handler(w http.ResponseWriter, r *http.Request) {
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
                fmt.Printf("ServiceName: %s, IP: %s, Node: %s\n", container_name, service.IP, service.Node)
		// 秘密鍵を読み込む
		usr, err := user.Current()
    		if err != nil {
			fmt.Println("unable to get user:%s",err)
    		}

    		// ユーザーのホームディレクトリにあるSSHキーのパスを作成
    		filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)

    		// ファイルを読み込む
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
                        HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意: 実際の運用では安全ではな>い
                }

                // SSHサーバに接続
                ssh_server := fmt.Sprintf("%s:22", service.Node)
                client, err := ssh.Dial("tcp", ssh_server, config)
                if err != nil {
                        fmt.Println("unable to connect ssh:", err)
                        return
                }
                defer client.Close()

		command1 := fmt.Sprintf("sudo docker stop %s", container_name)
                output_1, err := session_and_command(command1, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
         
                }
                fmt.Println(string(output_1))

                command2 := fmt.Sprintf("sudo docker stop %s_envoy", container_name)
                output_2, err := session_and_command(command2, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
                        
                }
                fmt.Println(string(output_2))

                command3 := fmt.Sprintf("sudo docker rm %s", container_name)
                output_3, err := session_and_command(command3, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
                        
                }
                fmt.Println(string(output_3))

		command4 := fmt.Sprintf("sudo docker rm %s_envoy", container_name)
                output_4, err := session_and_command(command4, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
                        
                }
                fmt.Println(string(output_4))

		command5 := "sudo iptables -t nat -F PREROUTING"
                output_5, err := session_and_command(command5, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)

                }
                fmt.Println(string(output_5))
	}
}
