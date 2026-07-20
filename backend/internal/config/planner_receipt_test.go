package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlannerReceiptEnvironmentOverrides(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	SetRequiredTestSecrets(t)
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_PLANNER_RECEIPT_ENABLED", "true")
	t.Setenv("XG2G_PLANNER_RECEIPT_REQUIRED", "true")
	t.Setenv("XG2G_PLANNER_RECEIPT_CAPACITY", "321")
	t.Setenv("XG2G_PLANNER_RECEIPT_TTL", "45s")

	cfg, err := NewLoader("", "test").Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !cfg.PlannerReceipt.Enabled || !cfg.PlannerReceipt.Required {
		t.Fatalf("planner receipt flags not applied: %+v", cfg.PlannerReceipt)
	}
	if cfg.PlannerReceipt.Capacity != 321 {
		t.Fatalf("capacity=%d, want 321", cfg.PlannerReceipt.Capacity)
	}
	if cfg.PlannerReceipt.TTL != 45*time.Second {
		t.Fatalf("ttl=%v, want 45s", cfg.PlannerReceipt.TTL)
	}
}

func TestPlannerConfigurationLoadsFromStrictYAML(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	SetRequiredTestSecrets(t)
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`
plannerShadow:
  enabled: true
  queueCapacity: 77
plannerReceipt:
  enabled: true
  required: true
  capacity: 123
  ttl: 45s
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := NewLoader(path, "test").Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !cfg.PlannerShadow.Enabled || cfg.PlannerShadow.QueueCapacity != 77 {
		t.Fatalf("planner shadow YAML not applied: %+v", cfg.PlannerShadow)
	}
	if !cfg.PlannerReceipt.Enabled || !cfg.PlannerReceipt.Required || cfg.PlannerReceipt.Capacity != 123 || cfg.PlannerReceipt.TTL != 45*time.Second {
		t.Fatalf("planner receipt YAML not applied: %+v", cfg.PlannerReceipt)
	}
}

func TestPlannerConfigurationSurvivesFileProjection(t *testing.T) {
	cfg := AppConfig{
		PlannerShadow:  PlannerShadowConfig{Enabled: true, QueueCapacity: 77},
		PlannerReceipt: PlannerReceiptConfig{Enabled: true, Required: true, Capacity: 123, TTL: 45 * time.Second},
	}
	file := ToFileConfig(&cfg)
	if file.PlannerShadow == nil || file.PlannerReceipt == nil {
		t.Fatal("planner configuration missing from file projection")
	}
	if !*file.PlannerShadow.Enabled || *file.PlannerShadow.QueueCapacity != 77 {
		t.Fatalf("planner shadow projection mismatch: %+v", file.PlannerShadow)
	}
	if !*file.PlannerReceipt.Enabled || !*file.PlannerReceipt.Required || *file.PlannerReceipt.Capacity != 123 || *file.PlannerReceipt.TTL != 45*time.Second {
		t.Fatalf("planner receipt projection mismatch: %+v", file.PlannerReceipt)
	}
}
