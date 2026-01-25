package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/spf13/cobra"
)

var (
	reportOut   string
	reportToken string
	reportPort  int
)

func init() {
	reportCmd.Flags().StringVar(&reportOut, "out", "", "Output file (default: stdout)")
	reportCmd.Flags().StringVar(&reportToken, "token", "", "API token (env: XG2G_API_TOKEN)")
	reportCmd.Flags().IntVar(&reportPort, "port", 8088, "API port")
}

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate evidence bundle",
	Long:  "Generates a standardized, redacted system report including status and environment fingerprint.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Resolve Auth
		if reportToken == "" {
			reportToken = config.ParseString("XG2G_API_TOKEN", "")
		}
		if reportToken == "" {
			return fmt.Errorf("authentication required: set --token or XG2G_API_TOKEN")
		}

		// 2. Data Gathering
		report := make(map[string]interface{})

		// A. Status (API)
		url := fmt.Sprintf("http://localhost:%d/api/v3/status", reportPort)
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+reportToken)

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			var status interface{}
			json.NewDecoder(resp.Body).Decode(&status)
			report["status"] = status
			resp.Body.Close()
		} else {
			report["status_error"] = fmt.Sprintf("failed to fetch status: %v", err)
		}

		// B. Fingerprint (Local)
		report["fingerprint"] = map[string]interface{}{
			"os":            runtime.GOOS,
			"arch":          runtime.GOARCH,
			"cpus":          runtime.NumCPU(),
			"go_version":    runtime.Version(),
			"timestamp_utc": time.Now().UTC(),
		}

		// 3. Serialize & Output
		data, _ := json.MarshalIndent(report, "", "  ")

		if reportOut != "" {
			err := os.WriteFile(reportOut, data, 0644)
			if err != nil {
				return err
			}
			fmt.Printf("✅ Report generated: %s\n", reportOut)
		} else {
			fmt.Println(string(data))
		}

		return nil
	},
}
