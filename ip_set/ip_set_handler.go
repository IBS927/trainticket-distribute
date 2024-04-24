package ip_set

import (
	"log"
	"net/http"
)

func IP_Set_Handler(w http.ResponseWriter, r *http.Request) {
	hello := []byte("Hello World!!!")
	_, err := w.Write(hello)
	if err != nil {
		log.Fatal(err)
	}
}
