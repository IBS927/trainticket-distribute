package main

import (
	"fmt"
	"net/http"
	ambient "github.com/IBS927/distributed_setting/ambient"
	ambient_cluster "github.com/IBS927/distributed_setting/ambient_cluster"
	"github.com/IBS927/distributed_setting/envoy_run"
	proxy_less "github.com/IBS927/distributed_setting/no_proxy_run"
	snic "github.com/IBS927/distributed_setting/snic_run"
	sidecar "github.com/IBS927/distributed_setting/sidecar"
	af_packet "github.com/IBS927/distributed_setting/af_packet"
	pf "github.com/IBS927/distributed_setting/pf"
	ld_preload "github.com/IBS927/distributed_setting/ld_preload"
	pf_preload "github.com/IBS927/distributed_setting/pf_preload"
	pf_lb "github.com/IBS927/distributed_setting/pf_lb"
	multi_preload "github.com/IBS927/distributed_setting/multi_preload"
	multi_service "github.com/IBS927/distributed_setting/multi_service"
)

func main() {
	http.HandleFunc("/envoy", envoy_run.Envoy_Run_Handler)
	http.HandleFunc("/envoy_del",envoy_run.Envoy_Del_Handler)
	http.HandleFunc("/no_proxy", proxy_less.ProxyLessHandler)
	http.HandleFunc("/no_proxy_mysql", proxy_less.MysqlProxyLessHandler)
	http.HandleFunc("/no_proxy_no_mysql", proxy_less.ServiceProxyLessHandler)
	http.HandleFunc("/af_packet", af_packet.ServiceProxyLessHandler)
	http.HandleFunc("/pf", pf.ServiceProxyLessHandler)
	http.HandleFunc("/ld_preload", ld_preload.ServiceProxyLessHandler)
	http.HandleFunc("/no_proxy_del", proxy_less.ProxyLessDeleteHandler)
	http.HandleFunc("/snic", snic.SnicHandler)
	http.HandleFunc("/snic_del",snic.SnicDelHandler)
	http.HandleFunc("/ambient",ambient.AmbientHandler)
	http.HandleFunc("/snic_ambient",ambient.SnicAmbientHandler)
	http.HandleFunc("/ambient_del",ambient.AmbientDeleteHandler)
	http.HandleFunc("/ambient_cluster",ambient_cluster.AmbientHandler)
	http.HandleFunc("/snic_ambient_cluster",ambient_cluster.SnicAmbientHandler)
	http.HandleFunc("/ambient_cluster_del",ambient_cluster.AmbientDeleteHandler)
	http.HandleFunc("/sidecar",sidecar.Proxy_Run_Handler)
	http.HandleFunc("/sidecar_del",sidecar.Proxy_Del_Handler)
	http.HandleFunc("/pf_preload", pf_preload.ServiceProxyLessHandler)
	http.HandleFunc("/pf_lb", pf_lb.ServiceProxyLessHandler)
	http.HandleFunc("/multi_preload", multi_preload.ServiceProxyLessHandler)
	http.HandleFunc("/multi_service", multi_service.ServiceProxyLessHandler)
	http.HandleFunc("/multi_del", multi_service.ProxyLessDeleteHandler)
	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
