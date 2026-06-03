package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type composeFileSpec struct {
	Services map[string]composeServiceSpec `yaml:"services"`
}

type composeServiceSpec struct {
	Image       string
	EnvFile     []string
	Environment map[string]string
	Volumes     []LifecycleRuntimeVolumeSnapshot
}

func readComposeSpec(path string) (composeFileSpec, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return composeFileSpec{}, err
	}
	var raw struct {
		Services map[string]struct {
			Image       string `yaml:"image"`
			EnvFile     any    `yaml:"env_file"`
			Environment any    `yaml:"environment"`
			Volumes     any    `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return composeFileSpec{}, err
	}

	spec := composeFileSpec{Services: map[string]composeServiceSpec{}}
	for name, svc := range raw.Services {
		spec.Services[name] = composeServiceSpec{
			Image:       strings.TrimSpace(svc.Image),
			EnvFile:     normalizeComposeStringList(svc.EnvFile),
			Environment: normalizeComposeEnvironment(svc.Environment),
			Volumes:     normalizeComposeVolumes(svc.Volumes),
		}
	}
	return spec, nil
}

func normalizeComposeStringList(raw any) []string {
	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func normalizeComposeEnvironment(raw any) map[string]string {
	out := map[string]string{}
	switch value := raw.(type) {
	case nil:
		return out
	case map[string]any:
		for key, rawValue := range value {
			out[strings.TrimSpace(key)] = strings.TrimSpace(fmt.Sprint(rawValue))
		}
	case []any:
		for _, item := range value {
			entry := strings.TrimSpace(fmt.Sprint(item))
			if entry == "" {
				continue
			}
			if idx := strings.Index(entry, "="); idx > 0 {
				out[strings.TrimSpace(entry[:idx])] = strings.TrimSpace(entry[idx+1:])
			} else {
				out[entry] = ""
			}
		}
	}
	return out
}

func normalizeComposeVolumes(raw any) []LifecycleRuntimeVolumeSnapshot {
	switch value := raw.(type) {
	case nil:
		return nil
	case []any:
		out := make([]LifecycleRuntimeVolumeSnapshot, 0, len(value))
		for _, item := range value {
			entry := strings.TrimSpace(fmt.Sprint(item))
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			switch len(parts) {
			case 1:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Target: parts[0]})
			case 2:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Source: parts[0], Target: parts[1]})
			default:
				out = append(out, LifecycleRuntimeVolumeSnapshot{Source: parts[0], Target: parts[1], Mode: strings.Join(parts[2:], ":")})
			}
		}
		return out
	default:
		return nil
	}
}
