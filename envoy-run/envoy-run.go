package envoy_run

import (
	"log"
	"net/http"
)

func Envoy_Run_Handler(w http.ResponseWriter, r *http.Request) {
	hello := []byte("Hello World!!!")
	_, err := w.Write(hello)
	if err != nil {
		log.Fatal(err)
	}
}
