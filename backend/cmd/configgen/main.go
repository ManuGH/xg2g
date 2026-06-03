package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ManuGH/xg2g/internal/config"
)

const (
	configDocPath              = "docs/guides/CONFIGURATION.md"
	configSchemaPath           = "docs/guides/config.schema.json"
	configGeneratedExamplePath = "config.generated.example.yaml"
	configInventory            = "docs/guides/CONFIG_SURFACES.md"
)

const (
	docBeginMarker = "<!-- BEGIN GENERATED CONFIG OPTIONS -->"
	docEndMarker   = "<!-- END GENERATED CONFIG OPTIONS -->"
)

func main() {
	allowCreate := flag.Bool("allow-create", false, "allow creating new schema nodes")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	paths, err := resolvePaths(cwd)
	if err != nil {
		fail(err)
	}

	registry, err := config.GetRegistry()
	if err != nil {
		fail(fmt.Errorf("get registry: %w", err))
	}
	entries := registryEntries(registry)

	if err := updateConfigDoc(paths.repoRoot, entries); err != nil {
		fail(err)
	}
	if err := updateSchemaDefaults(paths.repoRoot, entries, *allowCreate); err != nil {
		fail(err)
	}
	if err := writeGeneratedExample(paths.backendRoot, entries); err != nil {
		fail(err)
	}
	if err := writeConfigInventory(paths.repoRoot); err != nil {
		fail(err)
	}
}

type generatorPaths struct {
	repoRoot    string
	backendRoot string
}

func resolvePaths(cwd string) (generatorPaths, error) {
	cwd = filepath.Clean(cwd)
	repoRootCandidate := filepath.Join(cwd, "backend", "go.mod")
	if fileExists(repoRootCandidate) {
		return generatorPaths{
			repoRoot:    cwd,
			backendRoot: filepath.Join(cwd, "backend"),
		}, nil
	}

	parent := filepath.Dir(cwd)
	if fileExists(filepath.Join(cwd, "go.mod")) && fileExists(filepath.Join(parent, configDocPath)) {
		return generatorPaths{
			repoRoot:    parent,
			backendRoot: cwd,
		}, nil
	}

	return generatorPaths{}, fmt.Errorf("resolve paths from %q: expected repo root or backend root", cwd)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "configgen: %v\n", err)
	os.Exit(1)
}

func registryEntries(reg *config.Registry) []config.ConfigEntry {
	entries := make([]config.ConfigEntry, 0, len(reg.ByPath))
	for _, entry := range reg.ByPath {
		if entry.Path == "" {
			continue
		}
		if entry.Status == config.StatusInternal {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}
