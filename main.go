package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ramdanks/webrtc-signalling/server"
)

const (
	address string = ":3003"
)

func main() {

	r := mux.NewRouter()

	signalling := server.NewSignallingServer()
	signalling.LaunchTimeKeeper()

	servers := [...]server.WebSocketServerInterface{
		signalling,
	}

	for _, s := range servers {
		r.HandleFunc(s.EndpointPattern(), func(w http.ResponseWriter, r *http.Request) {
			server.ServeWebSocketFromHTTP(w, r, s)
		})
	}

	err := http.ListenAndServe(address, r)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Printf("Listening at: %v", address)
}
