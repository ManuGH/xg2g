package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func runHealthcheckCLI(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printHealthcheckUsage(fs.Output())
	}
	mode := fs.String("mode", "ready", "healthcheck mode: ready (default) or live")
	port := fs.Int("port", 8088, "API port to check")
	requireMetrics := fs.Bool("require-metrics", false, "probe /metrics endpoint as well")
	metricsPort := fs.Int("metrics-port", 9091, "metrics port to check")
	timeout := fs.Duration("timeout", 5*time.Second, "check timeout")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error parsing healthcheck flags: %v\n", err)
		return 2
	}

	client := http.Client{
		Timeout: *timeout,
	}

	// 1. API Health Probe
	path := "/healthz"
	if *mode == "ready" {
		path = "/readyz"
	}

	url := fmt.Sprintf("http://localhost:%d%s", *port, path)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Healthcheck failed (API network): %v\n", err)
		return 1
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Healthcheck failed (API status): %d %s\n", resp.StatusCode, resp.Status)
		return 1
	}

	// 2. Metrics Probe (Optional)
	if *requireMetrics {
		mUrl := fmt.Sprintf("http://localhost:%d/metrics", *metricsPort)
		mResp, err := client.Get(mUrl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Healthcheck failed (Metrics network): %v\n", err)
			return 1
		}
		_ = mResp.Body.Close()

		if mResp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Healthcheck failed (Metrics status): %d %s\n", mResp.StatusCode, mResp.Status)
			return 1
		}
	}

	fmt.Printf("Healthcheck successful (%s, metrics=%v)\n", *mode, *requireMetrics)
	return 0
}

func printHealthcheckUsage(ioW io.Writer) {
	// best-effort CLI output
	_, _ = fmt.Fprintln(ioW, "Usage:")
	_, _ = fmt.Fprintln(ioW, "  xg2g healthcheck [--mode=ready|live] [--port=8088] [--require-metrics] [--metrics-port=9091] [--timeout=5s]")
	_, _ = fmt.Fprintln(ioW, "")
	_, _ = fmt.Fprintln(ioW, "Flags:")
	_, _ = fmt.Fprintln(ioW, "  --mode string          healthcheck mode: ready or live (default: ready)")
	_, _ = fmt.Fprintln(ioW, "  --port int             API port to check (default: 8088)")
	_, _ = fmt.Fprintln(ioW, "  --require-metrics      probe Prometheus /metrics endpoint")
	_, _ = fmt.Fprintln(ioW, "  --metrics-port int     Prometheus metrics port (default: 9091)")
	_, _ = fmt.Fprintln(ioW, "  --timeout duration     check timeout (default: 5s)")
	_, _ = fmt.Fprintln(ioW, "")
	_, _ = fmt.Fprintln(ioW, "Examples:")
	_, _ = fmt.Fprintln(ioW, "  xg2g healthcheck --mode=ready --port=8088 --require-metrics")
}
