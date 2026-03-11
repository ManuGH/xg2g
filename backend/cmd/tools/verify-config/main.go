// verify-config parses config.yaml and prints the Timeouts fields.
// Usage: go run ./backend/cmd/tools/verify-config (from repo root)
package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type TimeoutsConfig struct {
	TranscodeStart      time.Duration `yaml:"transcode_start"`
	TranscodeNoProgress time.Duration `yaml:"transcode_no_progress"`
	KillGrace           time.Duration `yaml:"kill_grace"`
}

type FileConfig struct {
	Timeouts *TimeoutsConfig `yaml:"timeouts"`
}

func main() {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var cfg FileConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		fmt.Printf("Error unmarshalling: %v\n", err)
		os.Exit(1)
	}

	if cfg.Timeouts == nil {
		fmt.Println("Timeouts is nil")
	} else {
		fmt.Printf("TranscodeStart: %v (%d ns)\n", cfg.Timeouts.TranscodeStart, cfg.Timeouts.TranscodeStart)
		fmt.Printf("TranscodeNoProgress: %v (%d ns)\n", cfg.Timeouts.TranscodeNoProgress, cfg.Timeouts.TranscodeNoProgress)
	}
}
