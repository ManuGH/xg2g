package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func runHealthcheckCLI(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	mode := fs.String("mode", "ready", "healthcheck mode: ready (default) or live")
	port := fs.Int("port", 8088, "API port to check")
	timeout := fs.Duration("timeout", 5*time.Second, "check timeout")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing healthcheck flags: %v\n", err)
		return 1
	}

	path := "/healthz"
	if *mode == "ready" {
		path = "/readyz"
	}

	url := fmt.Sprintf("http://localhost:%d%s", *port, path)
	client := http.Client{
		Timeout: *timeout,
	}

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Healthcheck failed (network): %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Healthcheck failed (status): %d %s\n", resp.StatusCode, resp.Status)
		return 1
	}

	fmt.Printf("Healthcheck successful (%s)\n", *mode)
	return 0
}
