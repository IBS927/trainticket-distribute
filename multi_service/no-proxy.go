package proxy_less

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"regexp"
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

// 192.168.11.12 -> lb_192_168_11_12 のようなチェイン名にする
func lbChainNameForVIP(vip string) string {
	return "lb_" + strings.NewReplacer(".", "_", ":", "_").Replace(vip)
}

// nft -a list chain の出力から handle を削除する（vip向けの既存ルール掃除用）
func deleteRulesByHandleContaining(client *ssh.Client, table, chain string, mustContain []string) error {
	listCmd := fmt.Sprintf("sudo nft -a list chain %s %s %s 2>&1", "ip", table, chain)
	out, err := session_and_command(listCmd, client)
	if err != nil {
		return fmt.Errorf("list chain failed: %v (%s)", err, string(out))
	}

	// handle 123 を抜く
	reHandle := regexp.MustCompile(`\bhandle\s+(\d+)\b`)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		ok := true
		for _, s := range mustContain {
			if !strings.Contains(line, s) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		m := reHandle.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		h := m[1]
		delCmd := fmt.Sprintf("sudo nft delete rule %s %s %s handle %s 2>&1", "ip", table, chain, h)
		delOut, delErr := session_and_command(delCmd, client)
		if delErr != nil {
			return fmt.Errorf("delete rule handle=%s failed: %v (%s)", h, delErr, string(delOut))
		}
	}
	return nil
}

// ensureNatAndLB installs:
// - chain ip nat <lbChain>
// - rules inside lbChain: meta mark i -> dnat to backend return
// - PREROUTING: ip daddr VIP meta mark set numgen random mod N jump lbChain (insert at position 0)
//
// If enableOutput is true, also installs the same jump rule into OUTPUT (for local-origin traffic).
func ensureNatAndLB(
	client *ssh.Client,
	vip string,
	backends []string,
	enableOutput bool,
) error {
	if len(backends) == 0 {
		return nil
	}
	n := len(backends)
	lbChain := lbChainNameForVIP(vip)

	// 1) ensure table ip nat exists
	cmdTable := "sudo nft list table ip nat >/dev/null 2>&1 || sudo nft add table ip nat 2>&1"
	out, err := session_and_command(cmdTable, client)
	if err != nil {
		return fmt.Errorf("ensure table ip nat failed: %v (%s)", err, string(out))
	}

	// 2) ensure lb chain exists (regular chain; no hook)
	//    NOTE: wrap in single quotes because of braces etc.
	cmdAddChain := fmt.Sprintf("sudo nft 'add chain ip nat %s' 2>/dev/null || true", lbChain)
	out, err = session_and_command(cmdAddChain, client)
	if err != nil {
		return fmt.Errorf("ensure lb chain failed: %v (%s)", err, string(out))
	}

	// 3) flush lb chain (recreate rules every time; safe)
	cmdFlush := fmt.Sprintf("sudo nft 'flush chain ip nat %s' 2>&1", lbChain)
	out, err = session_and_command(cmdFlush, client)
	if err != nil {
		return fmt.Errorf("flush lb chain failed: %v (%s)", err, string(out))
	}

	// 4) add per-mark rules: mark i -> dnat to backends[i] return
	for i, dst := range backends {
		// return prevents later rules from overriding DNAT
		cmd := fmt.Sprintf(
			"sudo nft 'add rule ip nat %s meta mark %d counter dnat to %s' 2>&1",
			lbChain, i, dst,
		)
		out, err = session_and_command(cmd, client)
		if err != nil {
			return fmt.Errorf("add lb rule i=%d failed: %v (%s)", i, err, string(out))
		}
	}

	// 5) remove older jump rules for this VIP in PREROUTING/OUTPUT (optional but recommended)
	//    We delete rules that contain both "ip daddr <vip>" and "jump <lbChain>"
	_ = deleteRulesByHandleContaining(client, "nat", "PREROUTING", []string{"ip daddr " + vip, "jump " + lbChain})
	if enableOutput {
		_ = deleteRulesByHandleContaining(client, "nat", "OUTPUT", []string{"ip daddr " + vip, "jump " + lbChain})
	}

	// 6) insert jump rule at position 0 in PREROUTING
	//    generate random once -> store in mark -> jump
	cmdInsPre := fmt.Sprintf(
		"sudo nft 'insert rule ip nat PREROUTING position 0 ip daddr %s meta mark set numgen random mod %d jump %s' 2>&1",
		vip, n, lbChain,
	)
	out, err = session_and_command(cmdInsPre, client)
	if err != nil {
		return fmt.Errorf("insert PREROUTING jump failed: %v (%s)", err, string(out))
	}

	// 7) optionally insert into OUTPUT for local-origin traffic
	if enableOutput {
		cmdInsOut := fmt.Sprintf(
			"sudo nft 'insert rule ip nat OUTPUT position 0 ip daddr %s meta mark set numgen random mod %d jump %s' 2>&1",
			vip, n, lbChain,
		)
		out, err = session_and_command(cmdInsOut, client)
		if err != nil {
			return fmt.Errorf("insert OUTPUT jump failed: %v (%s)", err, string(out))
		}
	}

	return nil
}

func setup_container(containerBaseName string, service ServiceInfo, dnsOptionString string) error {
	fmt.Printf("Service Base Name: %s, VIP: %s, Node: %s\n",
		containerBaseName, service.IP, service.Node)
	
	numContainers := 8
	parts := strings.Split(service.IP, ".")
	if len(parts) != 4 {
		return fmt.Errorf("invalid VIP IP: %s", service.IP)
	}
	vipLast, err := strconv.Atoi(parts[3])
	if err != nil {
		return fmt.Errorf("invalid last part of VIP: %s (%v)", parts[3], err)
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

	// バックエンドIP一覧（DNAT先）を作る
	backends := make([]string, 0, numContainers)

	for i := 0; i < numContainers; i++ {
		// VIPの次(.VIP+1)から割り当て
		last := vipLast + i
		if last < 0 || last > 255 {
			return fmt.Errorf("generated lastPart out of range: %d", last)
		}

		ipParts := make([]string, 4)
		copy(ipParts, parts)
		ipParts[3] = strconv.Itoa(last)
		containerIP := strings.Join(ipParts, ".")
		backends = append(backends, containerIP)

		ifSuffix := strconv.Itoa(last)
		containerName := fmt.Sprintf("%s-%s", containerBaseName, ifSuffix)

		fmt.Printf("Creating container: %s, IP: %s, Node: %s\n", containerName, containerIP, service.Node)

		// docker run
		command2 := fmt.Sprintf(
			"sudo docker run --restart unless-stopped --network none %s --name %s -d kawanotatsuya/%s",
			dnsOptionString, containerName, containerBaseName,
		)
		output2, err := session_and_command(command2, client)
		if err != nil {
			return fmt.Errorf("docker run failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output2))

		// veth pair
		command3 := fmt.Sprintf("sudo ip link add veth%s type veth peer name eth%s", ifSuffix, ifSuffix)
		output3, err := session_and_command(command3, client)
		if err != nil {
			return fmt.Errorf("ip link add failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output3))

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

		// veth を netnsへ
		command5 := fmt.Sprintf("sudo ip link set veth%s netns %d", ifSuffix, pidInt)
		output5, err := session_and_command(command5, client)
		if err != nil {
			return fmt.Errorf("ip link set netns failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output5))

		// IPをコンテナ側vethに付与
		command6 := fmt.Sprintf("sudo nsenter -t %d -n ip addr add %s/24 dev veth%s", pidInt, containerIP, ifSuffix)
		output6, err := session_and_command(command6, client)
		if err != nil {
			return fmt.Errorf("ip addr add failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output6))

		// コンテナ側 veth up
		command7 := fmt.Sprintf("sudo nsenter -t %d -n ip link set veth%s up", pidInt, ifSuffix)
		output7, err := session_and_command(command7, client)
		if err != nil {
			return fmt.Errorf("ip link set up (netns) failed for %s: %v", containerName, err)
		}
		fmt.Println(string(output7))

		// ホスト側 eth を bridge に参加
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
	}

	vip := service.IP

        // 外部から来る通信だけなら false
	// このホスト自身が vip にアクセスする通信も分散したいなら true
	enableOutput := false

	if err := ensureNatAndLB(client, vip, backends, enableOutput); err != nil {
    		return fmt.Errorf("failed to install nft lb rules: %v", err)
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
	resp, err := http.Get("http://localhost:8000/all")
	if err != nil {
		fmt.Println("Error fetching URL:", err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	var services map[string]ServiceInfo
	if err := json.Unmarshal(body, &services); err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	// 例: コンテナ数はどこかの変数に入っている想定
	// リクエストパラメータ等から取るならここでセットしてください
	numContainers := 8 // <- ここをあなたの変数に置き換えてください

	usr, err := user.Current()
	if err != nil {
		fmt.Println("unable to get user:", err)
		return
	}

	filePath := fmt.Sprintf("%s/.ssh/id_ed25519", usr.HomeDir)
	key, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Println("unable to read private key:", err)
		return
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		fmt.Println("unable to parse private key:", err)
		return
	}

	config := &ssh.ClientConfig{
		User: usr.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}


	for baseName, service := range services {
		fmt.Printf("Service Base Name: %s, VIP: %s, Node: %s, Count: %d\n",
			baseName, service.IP, service.Node, numContainers)

		parts := strings.Split(service.IP, ".")
		if len(parts) != 4 {
			fmt.Println("invalid VIP:", service.IP)
			continue
		}
		vipLast, err := strconv.Atoi(parts[3])
		if err != nil {
			fmt.Println("invalid VIP last:", parts[3], err)
			continue
		}

		sshServer := fmt.Sprintf("%s:22", service.Node)
		client, err := ssh.Dial("tcp", sshServer, config)
		if err != nil {
			fmt.Println("unable to connect ssh:", err)
			continue
		}
		// NOTE: ループ内 defer は溜まるので Close() を明示的に呼ぶ
		// defer client.Close()

		// --- 1) コンテナ停止・削除 ---
		for i := 0; i < numContainers; i++ {
			last := vipLast + i
			if last < 0 || last > 255 {
				fmt.Println("last out of range:", last)
				continue
			}

			suffix := strconv.Itoa(last)
			containerName := fmt.Sprintf("%s-%s", baseName, suffix)

			command1 := fmt.Sprintf("sudo docker stop %s", containerName)
			out1, err := session_and_command(command1, client)
			if err != nil {
				fmt.Println("docker stop error:", err)
			}
			fmt.Println(string(out1))

			command2 := fmt.Sprintf("sudo docker rm %s", containerName)
			out2, err := session_and_command(command2, client)
			if err != nil {
				fmt.Println("docker rm error:", err)
			}
			fmt.Println(string(out2))

			// --- 2) veth 後始末 ---
			// コンテナを消した後は veth が残りやすいので消す
			// veth は netns に移していたので、残っている場合はホスト側 eth が残っている想定
			// まず eth を消して、ダメなら veth を消す（存在しない場合はエラーが出るが無視してよい）
			cmdDelEth := fmt.Sprintf("sudo ip link del eth%s 2>/dev/null || true", suffix)
			_, _ = session_and_command(cmdDelEth, client)

			cmdDelVeth := fmt.Sprintf("sudo ip link del veth%s 2>/dev/null || true", suffix)
			_, _ = session_and_command(cmdDelVeth, client)
		}

		// --- 3) NAT ルール削除 ---
		// 前に nft で add していた想定。
		// 「全部消す」なら: nft flush chain ip nat prerouting
		// ただし危険（他のPREROUTING natも消える）なので、VIPだけ消すのがおすすめ。

		// (A) 危険だが簡単：PREROUTING nat を全削除
		cmdFlush := "sudo nft flush chain ip nat prerouting"
		outFlush, err := session_and_command(cmdFlush, client)
		if err != nil {
			fmt.Println("nft flush error:", err)
		}
		fmt.Println(string(outFlush))

		// (B) 推奨：VIP宛ルールだけ削除（handleを取って消す）
		// まず nat/prerouting から vip を含むルールの handle を抽出して削除

		client.Close()
	}

	// HTTPレスポンス
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("deleted\n"))
}

