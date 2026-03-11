// verify-app-config loads data/config.yaml via the production config loader
// and prints the key timeout and FFmpeg fields.
// Usage: go run ./backend/cmd/tools/verify-app-config (from repo root)
package main

import (
	"fmt"
	"os"

	"github.com/ManuGH/xg2g/internal/config"
)

func main() {
	loader := config.NewLoader("data/config.yaml", "3.1.5")
	cfg, err := loader.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("TranscodeStart: %v\n", cfg.Timeouts.TranscodeStart)
	fmt.Printf("TranscodeNoProgress: %v\n", cfg.Timeouts.TranscodeNoProgress)
	fmt.Printf("KillTimeout: %v\n", cfg.FFmpeg.KillTimeout)
}
