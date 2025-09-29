// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("🚀 Starting OpenWebIF stub server...")
	log.Printf("📍 PID: %d", os.Getpid())
	log.Printf("📍 Working Directory: %s", func() string {
		wd, _ := os.Getwd()
		return wd
	}())

	// Check if port is available before starting
	log.Println("🔍 Checking if port 18080 is available...")
	conn, err := net.Dial("tcp", "127.0.0.1:18080")
	if err == nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("⚠️  Failed to close connection: %v", closeErr)
		}
		log.Fatal("❌ Port 18080 is already in use!")
	}
	log.Println("✅ Port 18080 is available")

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

	log.Println("stub_openwebif listening on 127.0.0.1:18080")
	log.Println("Available endpoints:")
	log.Println("  GET /api/status")
	log.Println("  GET /api/bouquets")
	log.Println("  GET /api/getallservices")
	log.Println("  GET /api/getservices")

	log.Println("🎯 Starting HTTP server...")
	if err = http.ListenAndServe("127.0.0.1:18080", mux); err != nil {
		log.Fatalf("❌ Failed to start OpenWebIF stub server: %v", err)
	}
}
