// verify-bin prints the configured FFmpeg binary path from data/config.yaml.
// Usage: go run ./cmd/tools/verify-bin (from repo root)
package main

import (
	"fmt"

	"github.com/ManuGH/xg2g/internal/config"
)

func main() {
	loader := config.NewLoader("data/config.yaml", "3.1.5")
	cfg, _ := loader.Load()
	fmt.Println(cfg.FFmpeg.Bin)
}
