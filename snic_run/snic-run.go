package snic

import (
	"encoding/json"
	"fmt"
	"os/user"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
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
		return nil, fmt.Errorf("failed to execute command: %s, error: %w", command, err)
	}
	return output, nil
}

func SnicHandler(w http.ResponseWriter, r *http.Request) {
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
		usr, err := user.Current()
    		if err != nil {
			fmt.Println("unable to get user:%s")
			return
    		}
		filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
		// 秘密鍵を読み込む
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
		//command1 := fmt.Sprintf("sudo docker pull kawanotatsuya/%s_trace", container_name)
		//output, err := session_and_command(command1, client)
		//if err != nil {
		//	fmt.Println("unable to create session or execute command:", err)
		//	return
		//}
		//fmt.Println(string(output))

		command2 := fmt.Sprintf("sudo docker run --network none %s --name %s -d kawanotatsuya/%s",dnsOptionString, container_name, container_name)
		output_2, err := session_and_command(command2, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_2))

		command3 := fmt.Sprintf("echo $(sudo docker inspect -f '{{.State.Pid}}' %s)", container_name)
		pid, err := session_and_command(command3, client)
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

		command4 := fmt.Sprintf("cd /home/%s/smartnic_cni_plugin && sudo CNI_COMMAND=ADD CNI_NETNS=%s CONTAINER_NAME=%s CNI_IFNAME=veth1 CNI_PATH=%s CNI_CONTAINERID=%d go run listen_req.go connect_reg.go snic.go < dock.conf",usr.Username, ns_path, container_name, ns_path, pid_d)
		output_4, err := session_and_command(command4, client)
		if err != nil {
			fmt.Println("unable to create session or execute command:", err)
			return
		}
		fmt.Println(string(output_4))

	}
}

func SnicDelHandler(w http.ResponseWriter, r *http.Request) {
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
			fmt.Println("unable to get user:%s",err)
			return
    		}

    		// ユーザーのホームディレクトリにあるSSHキーのパスを作成
    		filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)

    		// ファイルを読み込む
    		key, err := ioutil.ReadFile(filePath)
		// 秘密鍵を読み込む
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

		command3 := fmt.Sprintf("sudo iptables -t nat -F PREROUTING")
                output_3, err := session_and_command(command3, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
                }
                fmt.Println(string(output_3))
	}
}
