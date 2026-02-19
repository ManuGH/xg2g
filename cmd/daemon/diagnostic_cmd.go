package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ManuGH/xg2g/internal/config"
)

func runDiagnosticCLI(args []string) int {
	diagCmd := flag.NewFlagSet("diagnostic", flag.ContinueOnError)
	diagCmd.SetOutput(os.Stderr)
	diagCmd.Usage = func() {
		printDiagnosticUsage(diagCmd.Output())
	}
	action := diagCmd.String("action", "refresh", "action to perform (refresh)")
	token := diagCmd.String("token", "", "API token for authentication")
	port := diagCmd.Int("port", 8088, "API port")

	if err := diagCmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	// If token is empty, try to read from env
	if *token == "" {
		*token = config.ParseString("XG2G_API_TOKEN", "")
	}

	switch *action {
	case "refresh":
		return triggerV3Refresh(*port, *token)
	default:
		fmt.Printf("Unknown diagnostic action: %s\n", *action)
		return 1
	}
}

func printDiagnosticUsage(w io.Writer) {
	// best-effort CLI output
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic [--action=refresh] [--token=TOKEN] [--port=8088]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --action string  action to perform (refresh)")
	_, _ = fmt.Fprintln(w, "  --token string   API token for authentication (defaults to $XG2G_API_TOKEN)")
	_, _ = fmt.Fprintln(w, "  --port int       API port (default: 8088)")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic --action=refresh --token $XG2G_API_TOKEN")
}

func triggerV3Refresh(port int, token string) int {
	url := fmt.Sprintf("http://localhost:%d/api/v3/system/refresh", port)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		fmt.Printf("FAILED: create request: %v\n", err)
		return 1
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		// Also try X-API-Token for compatibility
		req.Header.Set("X-API-Token", token)
	}

	fmt.Printf("Triggering v3 refresh at %s...\n", url)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		fmt.Println("SUCCESS: Activity triggered")
		return 0
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("FAILED: read response body: %v\n", err)
		return 1
	}
	fmt.Printf("FAILED: HTTP %d\nBody: %s\n", resp.StatusCode, string(body))
	return 1
}
