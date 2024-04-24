package main

import (
	"fmt"
	"net/http"

	"github.com/IBS927/distributed_setting/iptables"
)

func main() {
	http.HandleFunc("/iptables", iptables.Iptables_handler)
	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
