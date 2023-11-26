package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/ramdanks/webrtc-signalling/server"
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "3003"
	}
	address := "0.0.0.0:" + port
	fmt.Printf("Listening on: %v", address)

	err := http.ListenAndServe(address, r)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Printf("Listening at: %v", address)
}
