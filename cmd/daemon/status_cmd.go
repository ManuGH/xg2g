package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/spf13/cobra"
)

var (
	statusToken string
	statusPort  int
	statusJSON  bool
)

func init() {
	statusCmd.Flags().StringVar(&statusToken, "token", "", "API token (env: XG2G_API_TOKEN)")
	statusCmd.Flags().IntVar(&statusPort, "port", 8088, "API port")
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output raw JSON")
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status",
	Long:  "Show verified system status via authenticated API.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Resolve Token
		if statusToken == "" {
			statusToken = config.ParseString("XG2G_API_TOKEN", "")
		}
		if statusToken == "" {
			return fmt.Errorf("authentication required: set --token or XG2G_API_TOKEN")
		}

		// 2. Fetch Status
		url := fmt.Sprintf("http://localhost:%d/api/v3/status", statusPort)
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+statusToken)

		resp, err := client.Do(req)
		if err != nil {
			if statusJSON {
				fmt.Println(`{"error": "unreachable"}`)
			} else {
				fmt.Printf("❌ Failed to reach daemon: %v\n", err)
			}
			os.Exit(2)
		}
		defer func() { _ = resp.Body.Close() }()

		// 3. Handle Errors
		if resp.StatusCode != 200 {
			if statusJSON {
				fmt.Printf(`{"error": "http_error", "code": %d}`+"\n", resp.StatusCode)
			} else {
				fmt.Printf("❌ API Error: HTTP %d\n", resp.StatusCode)
			}
			os.Exit(2)
		}

		// 4. Parse & Display
		body, _ := io.ReadAll(resp.Body)

		if statusJSON {
			fmt.Println(string(body))
			os.Exit(0)
		}

		// Pretty Print
		var s struct {
			Status  string `json:"status"`
			Release string `json:"release"`
			Digest  string `json:"digest"`
			Runtime struct {
				FFmpeg string `json:"ffmpeg"`
				Go     string `json:"go"`
			} `json:"runtime"`
			Drift *verification.DriftState `json:"drift"`
		}
		if err := json.Unmarshal(body, &s); err != nil {
			fmt.Printf("❌ Invalid JSON from API: %v\n", err)
			os.Exit(2)
		}

		if s.Status == "healthy" {
			fmt.Printf("✅ System Healthy (%s)\n", s.Release)
			fmt.Printf("   Runtime: Go %s / FFmpeg %s\n", s.Runtime.Go, s.Runtime.FFmpeg)

			// Show warnings even if status is healthy (e.g. minor drift or not detected yet?)
			// Status should be degraded if drift detected.
		} else {
			fmt.Printf("⚠️ System Status: %s (%s)\n", s.Status, s.Release)
			fmt.Printf("   Runtime: Go %s / FFmpeg %s\n", s.Runtime.Go, s.Runtime.FFmpeg)
		}

		if s.Drift != nil && s.Drift.Detected {
			fmt.Println("\n⚠️  DRIFT DETECTED:")
			fmt.Printf("   Last Check: %s\n", s.Drift.LastCheck.Format(time.RFC3339))
			for _, m := range s.Drift.Mismatches {
				fmt.Printf("   - [%s] %s: expected '%s', got '%s'\n", m.Kind, m.Key, m.Expected, m.Actual)
			}
			os.Exit(1)
		} else if s.Drift != nil {
			fmt.Printf("   Verification: Clean (Last check: %s)\n", s.Drift.LastCheck.Format(time.RFC822))
		}

		os.Exit(0)
		return nil
	},
}
