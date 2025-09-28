#!/bin/bash
set -e

echo "🔄 Applying xg2g fixes..."

# 1. XMLTV Generator erstellen
mkdir -p internal/epg
cat > internal/epg/generator.go <<'GENEOF'
package epg

import (
	"encoding/xml"
	"os"
)

type TV struct {
	XMLName   xml.Name   `xml:"tv"`
	Generator string     `xml:"generator-info-name,attr"`
	Channels  []Channel  `xml:"channel"`
	Programs  []Programme `xml:"programme"`
}

type Channel struct {
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
	Icon        *Icon    `xml:"icon,omitempty"`
}

type Icon struct { Src string `xml:"src,attr"` }

type Programme struct {
	Start   string `xml:"start,attr"`
	Stop    string `xml:"stop,attr"`
	Channel string `xml:"channel,attr"`
	Title   Title  `xml:"title"`
	Desc    string `xml:"desc,omitempty"`
}

type Title struct {
	Lang  string `xml:"lang,attr"`
	Value string `xml:",chardata"`
}

func GenerateXMLTV(channels []Channel) *TV {
	return &TV{
		Generator: "xg2g",
		Channels:  channels,
		Programs:  []Programme{},
	}
}

func WriteXMLTV(channels []Channel, path string) error {
	tv := GenerateXMLTV(channels)
	out, err := xml.MarshalIndent(tv, "", "  ")
	if err != nil { return err }
	h := []byte(`<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	return os.WriteFile(path, append(h, out...), 0644)
}
GENEOF

# 2. Patch API to add mutex protection
echo "🔧 Patching API for thread safety..."
cat > internal/api/http_fixed.go <<'APIFIX'
package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/ManuGH/xg2g/internal/jobs"
)

type Server struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex  // NEW: serialize refreshes
	cfg       jobs.Config
	status    jobs.Status
}

func New(cfg jobs.Config) *Server {
	return &Server{
		cfg:    cfg,
		status: jobs.Status{},
	}
}

func (s *Server) routes() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/api/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/api/refresh", s.handleRefresh).Methods("GET", "POST") // CHANGED
	r.PathPrefix("/files/").Handler(http.StripPrefix("/files/", 
		http.FileServer(http.Dir(s.cfg.DataDir))))
	return r
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.status)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// NEW: only allow a single refresh at a time
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	ctx := r.Context()
	st, err := jobs.Refresh(ctx, s.cfg)
	
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		s.mu.Lock()
	s.status.LastRun = time.Now()  // NEW: record time even on errors
		s.status.Error = err.Error()
	s.status.Channels = 0          // NEW: reset channel count on error
		s.mu.Unlock()
		
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	s.mu.Lock()
	s.status = *st
	s.mu.Unlock()
	
	json.NewEncoder(w).Encode(st)
}

func (s *Server) Handler() http.Handler {
	return s.routes()
}
APIFIX

# Alte API ersetzen
mv internal/api/http_fixed.go internal/api/http.go

# 3. Extend jobs to generate XMLTV
echo "🔧 Adding XMLTV generation to jobs..."
cat >> internal/jobs/refresh.go <<'JOBSFIX'

	// NEW: generate XMLTV (channels only)
	if cfg.XMLTVPath != "" {
		xmlCh := make([]epg.Channel, 0, len(items))
		for _, it := range items {
			if it.TvgID != "" {
				ch := epg.Channel{ID: it.TvgID, DisplayName: []string{it.Name}}
				if it.TvgLogo != "" { ch.Icon = &epg.Icon{Src: it.TvgLogo} }
				xmlCh = append(xmlCh, ch)
			}
		}
		if err := epg.WriteXMLTV(xmlCh, cfg.XMLTVPath); err != nil {
			log.Printf("WARN: XMLTV generation failed: %v", err)
		} else {
			log.Printf("XMLTV generated at %s (%d channels)", cfg.XMLTVPath, len(xmlCh))
		}
	}
JOBSFIX

# 4. Improve main logging
echo "🔧 Improving main logging..."
sed -i '' 's/log.Printf("Config: data=%s, owi=%s, bouquet=%s, xmltv=%s, fuzzy=%d", /log.Printf("Config: data=%s, owi=%s, bouquet=%s, xmltv=%s, fuzzy=%d, picon=%s", /' cmd/daemon/main.go
sed -i '' 's/cfg.DataDir, cfg.OWIBase, cfg.Bouquet, cfg.XMLTVPath, cfg.FuzzyMax)/cfg.DataDir, cfg.OWIBase, cfg.Bouquet, cfg.XMLTVPath, cfg.FuzzyMax, cfg.PiconBase)/' cmd/daemon/main.go

# 5. Cleanup
echo "🧹 Cleaning up..."
rm -f test/test data/test 2>/dev/null || true
echo "# Test files" > test/README.md
echo "# Data volume for M3U and XMLTV" > data/README.md

echo "✅ All fixes applied!"
