package ambient

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
type ProxyInfo struct {
	IP   string `json:"ip"`
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

func setup_container(container_name string, service ServiceInfo, dnsOptionString string, proxy ProxyInfo)(error){
                fmt.Printf("Service Name: %s, IP: %s, Node: %s\n", container_name, service.IP, service.Node)
                parts := strings.Split(service.IP, ".")
                lastPart := parts[len(parts)-1]

                usr, err := user.Current()
                if err != nil {
                        return fmt.Errorf("unable to get user:%s", err)
                }
                filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
                // 秘密鍵を読み込む
                key, err := ioutil.ReadFile(filePath)
                if err != nil {
                        //fmt.Println("unable to read private key:", err)
                        return fmt.Errorf("unable to read private key:%s", err)
                }

                // 秘密鍵をSignerにパースする
                signer, err := ssh.ParsePrivateKey(key)
                if err != nil {
                        //fmt.Println("unable to parse private key:", err)
                        return fmt.Errorf("unable to parse private key:%s", err)
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
                        //fmt.Println("unable to connect ssh:", err)
                        return fmt.Errorf("unable to connect ssh:%s", err)
                }
                defer client.Close()

                // コマンドを実行
                /*command1 := fmt.Sprintf("sudo docker pull kawanotatsuya/%s", container_name)
                output, err := session_and_command(command1, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output))
                */
                command2 := fmt.Sprintf("sudo docker run --restart unless-stopped --network none %s --name %s -d kawanotatsuya/%s",dnsOptionString, container_name, container_name)
                output_2, err := session_and_command(command2, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_2))

                command3 := fmt.Sprintf("sudo ip link add veth%s type veth peer name eth%s", lastPart, lastPart)
                output_3, err := session_and_command(command3, client)
                if err != nil {
                        //fmt.Println("unable to cretae session or execute command:", err)
                        return fmt.Errorf("unable to cretae session or execute command:%s", err)
                }
                fmt.Println(string(output_3))

                command4 := fmt.Sprintf("echo $(sudo docker inspect -f '{{.State.Pid}}' %s)", container_name)
                pid, err := session_and_command(command4, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                pid_s := string(pid)
                fmt.Println(pid_s)
                pid_d, err := strconv.Atoi(pid_s[:len(pid_s)-1])
                if err != nil {
                        //fmt.Println("cast error:", err)
                        return fmt.Errorf("cast error:", err)
                }

                ns_path := fmt.Sprintf("/proc/%d/ns/net", pid_d)
                fmt.Println(ns_path)
                command5 := fmt.Sprintf("sudo ip link set veth%s netns %d", lastPart, pid_d)
                output_5, err := session_and_command(command5, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_5))

		command6 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%s", pid_d, service.IP, lastPart)
                output_6, err := session_and_command(command6, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_6))

                command7 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%s up", pid_d, lastPart)
                output_7, err := session_and_command(command7, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_7))

                command8 := fmt.Sprintf("sudo ip link set eth%s master my_bridge", lastPart)
                output_8, err := session_and_command(command8, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_8))

                command9 := fmt.Sprintf("sudo ip link set eth%s up", lastPart)
                output_9, err := session_and_command(command9, client)
                if err != nil {
                        //fmt.Println("unable to create session or execute command:", err)
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_9))

		command10 := fmt.Sprintf("sudo iptables -t nat -I PREROUTING -p tcp -s %s -j DNAT --to-destination %s:%s", service.IP, proxy.IP, proxy.Port)
                output_10, err := session_and_command(command10, client)
                if err != nil {
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_10))

                output_11, err := session_and_command("sudo iptables -t nat -I PREROUTING -p tcp --dport 3306 -j RETURN", client)
                if err != nil {
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_11))

		output_12, err := session_and_command("sudo iptables -t nat -I PREROUTING -p tcp --dport 9411 -j RETURN", client)
                if err != nil {
                        return fmt.Errorf("unable to create session or execute command:%s", err)
                }
                fmt.Println(string(output_12))

		return nil

}

func AmbientHandler(w http.ResponseWriter, r *http.Request) {
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

	// Ambient Proxy 情報取得
	response, err := http.Get("http://localhost:8000/ambient")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer response.Body.Close()

        // レスポンスのボディを読み取り
        body, err = ioutil.ReadAll(response.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var ambient_proxy ProxyInfo
        err = json.Unmarshal(body, &ambient_proxy)
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
                err := setup_container(container_name, service, dnsOptionString, ambient_proxy)
                if err != nil {
                        fmt.Println("unable to set up container:", err)
                        return
                }
        }
}

func SnicAmbientHandler(w http.ResponseWriter, r *http.Request) {
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

        // Ambient Proxy 情報取得
        response, err := http.Get("http://localhost:8000/snic_ambient")
        if err != nil {
                fmt.Println("Error fetching URL:", err)
                return
        }
        defer response.Body.Close()

        // レスポンスのボディを読み取り
        body, err = ioutil.ReadAll(response.Body)
        if err != nil {
                fmt.Println("Error reading response:", err)
                return
        }

        var snic_proxy ProxyInfo
        err = json.Unmarshal(body, &snic_proxy)
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
                err := setup_container(container_name, service, dnsOptionString, snic_proxy)
                if err != nil {
                        fmt.Println("unable to set up container:", err)
                        return
                }
        }
}

func AmbientDeleteHandler(w http.ResponseWriter, r *http.Request) {
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
                
		command3 := "sudo iptables -t nat -F PREROUTING"
                output_3, err := session_and_command(command3, client)
                if err != nil {
                        fmt.Println("unable to create session or execute command:", err)

                }
                fmt.Println(string(output_3))
	}
}

