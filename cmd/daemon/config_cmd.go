// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"gopkg.in/yaml.v3"
)

func runConfigCLI(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printConfigUsage()
		return 0
	}

	switch args[0] {
	case "validate":
		return runConfigValidate(args[1:])
	case "dump":
		return runConfigDump(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", args[0])
		printConfigUsage()
		return 2
	}
}

func printConfigUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  xg2g config validate [--file|-f config.yaml]")
	fmt.Fprintln(os.Stderr, "  xg2g config dump --effective [--file|-f config.yaml] [--format=yaml|json]")
}

func resolveDefaultConfigPath() string {
	dataDir := strings.TrimSpace(os.Getenv("XG2G_DATA"))
	if dataDir == "" {
		dataDir = "/tmp"
	}
	autoPath := filepath.Join(dataDir, "config.yaml")
	if _, err := os.Stat(autoPath); err == nil {
		return autoPath
	}
	return ""
}

func runConfigValidate(args []string) int {
	fs := flag.NewFlagSet("xg2g config validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	fs.StringVar(&file, "file", "", "path to YAML configuration file")
	fs.StringVar(&file, "f", "", "path to YAML configuration file (shorthand)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	configPath := strings.TrimSpace(file)
	if configPath == "" {
		configPath = resolveDefaultConfigPath()
	}
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --file is required (no default config.yaml found in $XG2G_DATA)")
		return 2
	}

	loader := config.NewLoader(configPath, version)
	if _, err := loader.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error in %s:\n  %v\n", configPath, err)
		return 1
	}

	fmt.Printf("✓ %s is valid\n", configPath)
	return 0
}

func runConfigDump(args []string) int {
	fs := flag.NewFlagSet("xg2g config dump", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	var format string
	var effective bool

	fs.StringVar(&file, "file", "", "path to YAML configuration file")
	fs.StringVar(&file, "f", "", "path to YAML configuration file (shorthand)")
	fs.StringVar(&format, "format", "yaml", "output format: yaml or json")
	fs.BoolVar(&effective, "effective", false, "dump effective configuration (defaults + file + env)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !effective {
		fmt.Fprintln(os.Stderr, "Error: --effective is required")
		return 2
	}

	configPath := strings.TrimSpace(file)
	if configPath == "" {
		configPath = resolveDefaultConfigPath()
	}
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --file is required (no default config.yaml found in $XG2G_DATA)")
		return 2
	}

	loader := config.NewLoader(configPath, version)
	cfg, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error in %s:\n  %v\n", configPath, err)
		return 1
	}

	fileCfg := fileConfigFromAppConfig(cfg)
	redactFileConfigSecrets(&fileCfg)

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "yaml", "yml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(fileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode YAML: %v\n", err)
			return 1
		}
		_ = enc.Close()
		return 0
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unsupported format: %s (use yaml or json)\n", format)
		return 2
	}
}

func fileConfigFromAppConfig(cfg config.AppConfig) config.FileConfig {
	epgEnabled := cfg.EPGEnabled
	epgDays := cfg.EPGDays
	epgMaxConcurrency := cfg.EPGMaxConcurrency
	epgTimeoutMS := cfg.EPGTimeoutMS
	epgRetries := cfg.EPGRetries
	epgFuzzyMax := cfg.FuzzyMax

	metricsEnabled := cfg.MetricsEnabled
	useWebIFStreams := cfg.UseWebIFStreams

	return config.FileConfig{
		Version:  cfg.Version,
		DataDir:  cfg.DataDir,
		LogLevel: cfg.LogLevel,
		OpenWebIF: config.OpenWebIFConfig{
			BaseURL:    cfg.OWIBase,
			Username:   cfg.OWIUsername,
			Password:   cfg.OWIPassword,
			Timeout:    cfg.OWITimeout.String(),
			Retries:    cfg.OWIRetries,
			Backoff:    cfg.OWIBackoff.String(),
			MaxBackoff: cfg.OWIMaxBackoff.String(),
			StreamPort: cfg.StreamPort,
			UseWebIF:   &useWebIFStreams,
		},
		Bouquets: splitCSVString(cfg.Bouquet),
		EPG: config.EPGConfig{
			Enabled:        &epgEnabled,
			Days:           &epgDays,
			MaxConcurrency: &epgMaxConcurrency,
			TimeoutMS:      &epgTimeoutMS,
			Retries:        &epgRetries,
			FuzzyMax:       &epgFuzzyMax,
			XMLTVPath:      cfg.XMLTVPath,
			Source:         cfg.EPGSource,
		},
		API: config.APIConfig{
			Token:      cfg.APIToken,
			ListenAddr: cfg.APIListenAddr,
		},
		Metrics: config.MetricsConfig{
			Enabled:    &metricsEnabled,
			ListenAddr: cfg.MetricsAddr,
		},
		Picons: config.PiconsConfig{
			BaseURL: cfg.PiconBase,
		},
	}
}

func splitCSVString(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func redactFileConfigSecrets(cfg *config.FileConfig) {
	if cfg == nil {
		return
	}
	if cfg.OpenWebIF.Password != "" {
		cfg.OpenWebIF.Password = "***"
	}
	if cfg.API.Token != "" {
		cfg.API.Token = "***"
	}
}
