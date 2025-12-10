package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/Y3rnur/go-realtime-chat/backend"
)

func main() {
	fs := http.FileServer(http.Dir("./frontend"))

	mux := http.NewServeMux()
	mux.Handle("/", fs)

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "API Status: OK")
	})

	handler := backend.LoggingMiddleware(mux)

	const port = ":8080"
	log.Printf("Server starting on http://localhost%s", port)

	err := http.ListenAndServe(port, handler)
	if err != nil {
		log.Fatal(err)
	}
}
