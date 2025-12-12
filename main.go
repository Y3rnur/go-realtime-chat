package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/Y3rnur/go-realtime-chat/backend"
	"github.com/Y3rnur/go-realtime-chat/backend/store"
)

func main() {
	fs := http.FileServer(http.Dir("./frontend"))

	ctx := context.Background()
	pool, err := store.NewPool(ctx)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.Handle("/", fs)

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "API Status: OK")
	})

	handler := backend.LoggingMiddleware(mux)

	const port = ":8080"
	log.Printf("Server starting on http://localhost%s", port)

	err = http.ListenAndServe(port, handler)
	if err != nil {
		log.Fatal(err)
	}
}
