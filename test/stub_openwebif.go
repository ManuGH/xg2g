// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{
				{"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet", "Premium"},
			},
		})
	})

	mux.HandleFunc("/api/getallservices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "ORF1 HD", "servicereference": "1:0:19:132F:3EF:1:C00000:0:0:0:"},
				{"servicename": "ORF2N HD", "servicereference": "1:0:19:1334:3EF:1:C00000:0:0:0:"},
			},
		})
	})

	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "ORF1 HD", "servicereference": "1:0:19:132F:3EF:1:C00000:0:0:0:"},
				{"servicename": "ORF2N HD", "servicereference": "1:0:19:1334:3EF:1:C00000:0:0:0:"},
			},
		})
	})

	log.Println("stub_openwebif listening on 127.0.0.1:18080")
	log.Fatal(http.ListenAndServe("127.0.0.1:18080", mux))
}
