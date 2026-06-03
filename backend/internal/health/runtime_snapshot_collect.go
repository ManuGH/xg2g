package health

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

func normalizeInstallRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "/"
	}
	return filepath.Clean(root)
}

func runtimeJoin(root, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if root == "" || root == "/" {
		if filepath.IsAbs(target) {
			return filepath.Clean(target)
		}
		return filepath.Clean("/" + target)
	}
	target = strings.TrimPrefix(filepath.Clean(target), string(filepath.Separator))
	return filepath.Join(root, target)
}

func snapshotFile(path string) LifecycleRuntimeFileSnapshot {
	path = strings.TrimSpace(path)
	snapshot := LifecycleRuntimeFileSnapshot{Path: path}
	if path == "" {
		return snapshot
	}
	info, err := os.Stat(path)
	if err != nil {
		return snapshot
	}
	snapshot.Exists = true
	snapshot.Mode = fmt.Sprintf("%03o", info.Mode().Perm())
	if info.Mode().IsRegular() {
		// #nosec G304 -- snapshot paths come from the fixed runtime snapshot inventory, not user input.
		if data, readErr := os.ReadFile(filepath.Clean(path)); readErr == nil {
			sum := sha256.Sum256(data)
			snapshot.SHA256 = hex.EncodeToString(sum[:])
		}
	}
	return snapshot
}

func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if key != "" {
			out[key] = val
		}
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func splitComposeFiles(raw string) []string {
	parts := strings.Split(raw, ":")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func collectOptionalComposeFiles(installRoot string) []LifecycleRuntimeFileSnapshot {
	paths := []string{
		runtimeJoin(installRoot, "/srv/xg2g/docker-compose.gpu.yml"),
		runtimeJoin(installRoot, "/srv/xg2g/docker-compose.nvidia.yml"),
	}
	out := make([]LifecycleRuntimeFileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot := snapshotFile(path)
		if snapshot.Exists {
			out = append(out, snapshot)
		}
	}
	return out
}

func selectedComposeFiles(installRoot string, compose LifecycleRuntimeComposeSnapshot) []string {
	if len(compose.ConfiguredFiles) > 0 {
		out := make([]string, 0, len(compose.ConfiguredFiles))
		for _, file := range compose.ConfiguredFiles {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			if filepath.IsAbs(file) {
				out = append(out, filepath.Clean(runtimeJoin(installRoot, file)))
				continue
			}
			out = append(out, runtimeJoin(installRoot, filepath.Join("/srv/xg2g", file)))
		}
		return out
	}

	out := []string{}
	if strings.TrimSpace(compose.BaseFile.Path) != "" {
		out = append(out, compose.BaseFile.Path)
	}
	for _, optional := range compose.OptionalFiles {
		if optional.Exists && strings.TrimSpace(optional.Path) != "" {
			out = append(out, optional.Path)
		}
	}
	return out
}

func readJSONVersion(path string) (int, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, false
	}
	var payload struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false
	}
	return payload.Version, true
}

func collectSQLiteSchemas(installRoot string, cfg config.AppConfig) []LifecycleRuntimeSQLiteSchema {
	if strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") {
		return nil
	}

	dbs := []struct {
		id   string
		path string
	}{
		{id: "sessions", path: filepath.Join(cfg.Store.Path, "sessions.sqlite")},
		{id: "deviceauth", path: filepath.Join(cfg.Store.Path, "deviceauth.sqlite")},
		{id: "resume", path: filepath.Join(cfg.Store.Path, "resume.sqlite")},
		{id: "capabilities", path: filepath.Join(cfg.Store.Path, "capabilities.sqlite")},
		{id: "decision_audit", path: filepath.Join(cfg.Store.Path, "decision_audit.sqlite")},
		{id: "capability_registry", path: filepath.Join(cfg.Store.Path, "capability_registry.sqlite")},
		{id: "entitlements", path: filepath.Join(cfg.Store.Path, "entitlements.sqlite")},
		{id: "household", path: filepath.Join(cfg.Store.Path, "household.sqlite")},
	}
	if strings.TrimSpace(cfg.Library.DBPath) != "" {
		dbs = append(dbs, struct {
			id   string
			path string
		}{id: "library", path: cfg.Library.DBPath})
	}

	out := make([]LifecycleRuntimeSQLiteSchema, 0, len(dbs))
	for _, dbInfo := range dbs {
		actualPath := runtimeJoin(installRoot, dbInfo.path)
		schema := LifecycleRuntimeSQLiteSchema{
			ID:     dbInfo.id,
			Path:   actualPath,
			Exists: false,
		}
		if _, err := os.Stat(actualPath); err != nil {
			out = append(out, schema)
			continue
		}
		schema.Exists = true
		if version, ok := readSQLiteUserVersion(actualPath); ok {
			schema.UserVersion = &version
		}
		out = append(out, schema)
	}
	return out
}

func readSQLiteUserVersion(path string) (int, bool) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, false
	}
	defer func() { _ = db.Close() }()

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, false
	}
	return version, true
}
