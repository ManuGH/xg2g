package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

func updateConfigDoc(root string, entries []config.ConfigEntry) error {
	path := filepath.Join(root, configDocPath)
	// #nosec G304 -- CLI tool, path provided by user argument
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config doc: %w", err)
	}

	generated := buildConfigDoc(entries)
	out, err := replaceGeneratedSection(string(raw), generated)
	if err != nil {
		return fmt.Errorf("update config doc: %w", err)
	}
	//nolint:gosec // G304, G703: CLI tool, paths are deterministic and within repo
	if err := os.WriteFile(path, []byte(out), 0600); err != nil {
		return fmt.Errorf("write config doc: %w", err)
	}
	return nil
}

func buildConfigDoc(entries []config.ConfigEntry) string {
	grouped := make(map[string][]config.ConfigEntry)
	for _, entry := range entries {
		group := entry.Path
		if idx := strings.Index(group, "."); idx != -1 {
			group = group[:idx]
		} else {
			group = "root"
		}
		grouped[group] = append(grouped[group], entry)
	}

	groups := make([]string, 0, len(grouped))
	for group := range grouped {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	var b strings.Builder
	b.WriteString(docBeginMarker)
	b.WriteString("\n## Registry Options (Generated)\n\n")
	b.WriteString("This section is generated from `internal/config/registry.go`. Do not edit by hand.\n\n")

	writeEssentialSection(&b, entries)

	for _, group := range groups {
		entries := grouped[group]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		fmt.Fprintf(&b, "### %s\n\n", group)
		if group == "enigma2" {
			b.WriteString("Legacy YAML section `openWebIF.*` is rejected at load time; use `enigma2.*`.\n\n")
		}
		b.WriteString("| Path | Env | Default | Status | Profile |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, entry := range entries {
			env := "-"
			if entry.Env != "" {
				env = fmt.Sprintf("`%s`", entry.Env)
			}
			def := "-"
			if entry.Default != nil {
				def = fmt.Sprintf("`%s`", formatDefault(entry.Default))
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
				entry.Path, env, def, entry.Status, entry.Profile)
		}
		b.WriteString("\n")
	}
	b.WriteString(docEndMarker)
	return b.String()
}

// writeEssentialSection emits a curated "start here" table of the Simple-profile
// keys (the core knobs a typical deployment sets) ahead of the full per-area
// reference. These keys also appear in their group sections below.
func writeEssentialSection(b *strings.Builder, entries []config.ConfigEntry) {
	var essential []config.ConfigEntry
	for _, entry := range entries {
		if entry.Profile == config.ProfileSimple && entry.Status == config.StatusActive {
			essential = append(essential, entry)
		}
	}
	if len(essential) == 0 {
		return
	}
	sort.Slice(essential, func(i, j int) bool { return essential[i].Path < essential[j].Path })

	b.WriteString("### Essential (start here)\n\n")
	b.WriteString("The core knobs for a typical deployment. Everything in the per-area sections below is advanced or optional; these same keys also appear in their group sections.\n\n")
	b.WriteString("| Path | Env | Default |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, entry := range essential {
		env := "-"
		if entry.Env != "" {
			env = fmt.Sprintf("`%s`", entry.Env)
		}
		def := "-"
		if entry.Default != nil {
			def = fmt.Sprintf("`%s`", formatDefault(entry.Default))
		}
		fmt.Fprintf(b, "| `%s` | %s | %s |\n", entry.Path, env, def)
	}
	b.WriteString("\n")
}

func replaceGeneratedSection(content string, generated string) (string, error) {
	start := strings.Index(content, docBeginMarker)
	end := strings.Index(content, docEndMarker)
	if start == -1 || end == -1 || end < start {
		return content + "\n\n" + generated + "\n", nil
	}
	end += len(docEndMarker)
	return content[:start] + generated + content[end:], nil
}
