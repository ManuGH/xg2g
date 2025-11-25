// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("🚀 Starting OpenWebIF stub server...")
	log.Printf("📍 PID: %d", os.Getpid())
	log.Printf("📍 Working Directory: %s", func() string {
		wd, _ := os.Getwd()
		return wd
	}())

	// Flags for host/port configuration
	host := flag.String("host", "127.0.0.1", "host/IP to bind to")
	port := flag.Int("port", 18080, "port to listen on")
	flag.Parse()
	addr := fmt.Sprintf("%s:%d", *host, *port)

	// Check if port is available before starting
	log.Printf("🔍 Checking if address %s is available...", addr)
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Fatalf("❌ Failed to bind to %s: %v", addr, err)
	}
	log.Printf("✅ Address %s is available and bound", addr)

	// Close the test listener to reuse the port for HTTP server
	if closeErr := listener.Close(); closeErr != nil {
		log.Printf("⚠️  Failed to close test listener: %v", closeErr)
	}

	mux := http.NewServeMux()

	// Add health check endpoint
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Health check request from %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": "stub-1.0.0",
		})
	})

	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Bouquets request from %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{
				{"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet", "Premium"},
			},
		})
	})

	mux.HandleFunc("/api/getallservices", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GetAllServices request from %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "ORF1 HD", "servicereference": "1:0:19:132F:3EF:1:C00000:0:0:0:"},
				{"servicename": "ORF2N HD", "servicereference": "1:0:19:1334:3EF:1:C00000:0:0:0:"},
			},
		})
	})

	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GetServices request from %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "ORF1 HD", "servicereference": "1:0:19:132F:3EF:1:C00000:0:0:0:"},
				{"servicename": "ORF2N HD", "servicereference": "1:0:19:1334:3EF:1:C00000:0:0:0:"},
			},
		})
	})

	// Add EPG endpoints
	epgHandler := func(w http.ResponseWriter, r *http.Request) {
		log.Printf("EPG request (%s) from %s", r.URL.Path, r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// Return empty list of events for now
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []any{},
		})
	}
	mux.HandleFunc("/api/epgservice", epgHandler)
	mux.HandleFunc("/web/epgservice", epgHandler)

	// Add catch-all handler for debugging
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			log.Printf("Unknown request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		}
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte("Not Found")); err != nil {
			log.Printf("⚠️  Failed to write response: %v", err)
		}
	})

	log.Printf("stub_openwebif listening on %s", addr)
	log.Println("Available endpoints:")
	log.Println("  GET /api/status")
	log.Println("  GET /api/bouquets")
	log.Println("  GET /api/getallservices")
	log.Println("  GET /api/getservices")

	log.Printf("🎯 Starting HTTP server on %s...", addr)

	// Create server with explicit configuration
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("✅ OpenWebIF stub server is now listening on http://%s", addr)
	log.Println("📡 Server ready to accept connections")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("❌ Failed to start OpenWebIF stub server: %v", err)
	}
}
