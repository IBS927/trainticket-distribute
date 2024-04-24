package iptables

import (
	"log"
	"net/http"
)

func handler(w http.ResponseWriter, r *http.Request) {
	hello := []byte("Hello World!!!")
	_, err := w.Write(hello)
	if err != nil {
		log.Fatal(err)
	}
}
