package main

import (
	"fmt"
	"net/http"

	"github.com/IBS927/distributed_setting/envoy_run"
	proxy_less "github.com/IBS927/distributed_setting/no_proxy_run"
	snic "github.com/IBS927/distributed_setting/snic_run"
)

func main() {
	http.HandleFunc("/envoy", envoy_run.Envoy_Run_Handler)
	http.HandleFunc("/envoy_del",envoy_run.Envoy_Del_Handler)
	http.HandleFunc("/no_proxy", proxy_less.ProxyLessHandler)
	http.HandleFunc("/no_proxy_mysql", proxy_less.MysqlProxyLessHandler)
	http.HandleFunc("/no_proxy_no_mysql", proxy_less.ServiceProxyLessHandler)
	http.HandleFunc("/no_proxy_del", proxy_less.ProxyLessDeleteHandler)
	http.HandleFunc("/snic", snic.SnicHandler)
	http.HandleFunc("/snic_del",snic.SnicDelHandler)
	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
