package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func runDiagnosticCLI(args []string) int {
	diagCmd := flag.NewFlagSet("diagnostic", flag.ContinueOnError)
	action := diagCmd.String("action", "refresh", "action to perform (refresh)")
	token := diagCmd.String("token", "", "API token for authentication")
	port := diagCmd.Int("port", 8088, "API port")

	if err := diagCmd.Parse(args); err != nil {
		return 1
	}

	// If token is empty, try to read from env
	if *token == "" {
		*token = os.Getenv("XG2G_API_TOKEN")
	}

	switch *action {
	case "refresh":
		return triggerV3Refresh(*port, *token)
	default:
		fmt.Printf("Unknown diagnostic action: %s\n", *action)
		return 1
	}
}

func triggerV3Refresh(port int, token string) int {
	url := fmt.Sprintf("http://localhost:%d/api/v3/system/refresh", port)
	req, _ := http.NewRequest(http.MethodPost, url, nil)

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
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		fmt.Println("SUCCESS: Activity triggered")
		return 0
	}

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("FAILED: HTTP %d\nBody: %s\n", resp.StatusCode, string(body))
	return 1
}
