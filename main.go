package main

import (
	"fmt"
	"net/http"

	dockerfile "github.com/IBS927/distributed_setting/docker_file"
	envoy_handler "github.com/IBS927/distributed_setting/envoy"
	"github.com/IBS927/distributed_setting/envoy_run"
	"github.com/IBS927/distributed_setting/ip_set"
	"github.com/IBS927/distributed_setting/iptables"
	proxy_less "github.com/IBS927/distributed_setting/no_proxy_run"
	plugin "github.com/IBS927/distributed_setting/plugin_env"
	snic "github.com/IBS927/distributed_setting/snic_run"
)

func main() {
	http.HandleFunc("/iptables", iptables.Iptables_handler)
	http.HandleFunc("/docker_file", dockerfile.DockerHandler)
	http.HandleFunc("/envoy_file", envoy_handler.Envoy_Handler)
	http.HandleFunc("/envoy", envoy_run.Envoy_Run_Handler)
	http.HandleFunc("/ip_set", ip_set.IP_Set_Handler)
	http.HandleFunc("/no_proxy", proxy_less.ProxyLessHandler)
	http.HandleFunc("/plugin_env", plugin.PluginHandler)
	http.HandleFunc("/snic_run", snic.SnicHandler)
	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
