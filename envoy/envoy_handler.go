package envoy_handler

import (
	"log"
	"net/http"
)

func Envoy_Handler(w http.ResponseWriter, r *http.Request) {
	hello := []byte("Hello World!!!")
	_, err := w.Write(hello)
	if err != nil {
		log.Fatal(err)
	}
}
