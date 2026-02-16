package main

import "testing"

func TestNormalizeScenarioResult_UnimplementedStrict(t *testing.T) {
	in := ScenarioResult{
		Name:   "cpu_pressure",
		Pass:   true,
		Status: scenarioStatusUnimplemented,
	}

	got := normalizeScenarioResult(in, false)
	if got.Status != scenarioStatusUnimplemented {
		t.Fatalf("status=%q, want %q", got.Status, scenarioStatusUnimplemented)
	}
	if got.Pass {
		t.Fatalf("pass=%v, want false", got.Pass)
	}
	if got.Reason != "unimplemented" {
		t.Fatalf("reason=%q, want unimplemented", got.Reason)
	}
}

func TestNormalizeScenarioResult_UnimplementedAllowed(t *testing.T) {
	in := ScenarioResult{
		Name:   "chaos_injection",
		Pass:   true,
		Status: scenarioStatusUnimplemented,
	}

	got := normalizeScenarioResult(in, true)
	if got.Status != scenarioStatusSkipped {
		t.Fatalf("status=%q, want %q", got.Status, scenarioStatusSkipped)
	}
	if got.Pass {
		t.Fatalf("pass=%v, want false", got.Pass)
	}
	if got.Reason != "unimplemented" {
		t.Fatalf("reason=%q, want unimplemented", got.Reason)
	}
}

func TestNormalizeScenarioResult_DefaultsToPassFail(t *testing.T) {
	pass := normalizeScenarioResult(ScenarioResult{Name: "ok", Pass: true}, false)
	if pass.Status != scenarioStatusPass {
		t.Fatalf("pass.status=%q, want %q", pass.Status, scenarioStatusPass)
	}

	fail := normalizeScenarioResult(ScenarioResult{Name: "nok", Pass: false}, false)
	if fail.Status != scenarioStatusFail {
		t.Fatalf("fail.status=%q, want %q", fail.Status, scenarioStatusFail)
	}
}

func TestUnimplementedScenarioHelper(t *testing.T) {
	got := unimplementedScenario("cpu_pressure")
	if got.Status != scenarioStatusUnimplemented {
		t.Fatalf("status=%q, want %q", got.Status, scenarioStatusUnimplemented)
	}
	if got.Pass {
		t.Fatalf("pass=%v, want false", got.Pass)
	}
	if got.Reason != "unimplemented" {
		t.Fatalf("reason=%q, want unimplemented", got.Reason)
	}
	if len(got.Failures) == 0 {
		t.Fatal("expected failure entry for unimplemented scenario")
	}
}
