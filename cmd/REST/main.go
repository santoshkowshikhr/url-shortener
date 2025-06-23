package main

import (
	"context"
	"go-api-server/api"
	"log"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// gRPC Gateway mux
	gwMux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	err := api.RegisterShortenerServiceHandlerFromEndpoint(ctx, gwMux, "localhost:50051", opts)
	if err != nil {
		log.Fatalf("failed to start HTTP gateway: %v", err)
	}

	// Main HTTP mux
	mux := http.NewServeMux()

	// Mount gRPC-Gateway handler under /shorten
	mux.Handle("/shorten", gwMux)

	// Handle browser-style GET /<short_code> redirects
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/")
		if code == "" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		conn, err := grpc.DialContext(r.Context(), "localhost:50051", grpc.WithInsecure())
		if err != nil {
			http.Error(w, "cannot connect to gRPC backend", http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		client := api.NewShortenerServiceClient(conn)
		resp, err := client.RedirectShortener(r.Context(), &api.RedirectRequest{ShortUrl: code})
		if err != nil {
			http.NotFound(w, r)
			return
		}

		http.Redirect(w, r, resp.LongUrl, http.StatusFound) // 302 redirect
	})

	log.Println("REST gateway + redirect server listening on :8080")
	http.ListenAndServe(":8080", mux)
}
