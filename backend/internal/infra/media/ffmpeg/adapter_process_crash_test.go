package ffmpeg

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// exitErrorFromSignal runs a real child process and kills it with sig, so the
// classification is exercised against a genuine exec.ExitError rather than a
// hand-built one — the whole defect being fixed lived in how the real thing was
// read.
func exitErrorFromSignal(t *testing.T, sig syscall.Signal) error {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("signal child: %v", err)
	}
	err := cmd.Wait()
	if err == nil {
		t.Fatalf("expected the child to die on %s", sig)
	}
	return err
}

// TestSummarizeProcessExit_DistinguishesCrashFromDeliberateKill pins the defect
// that made six real ffmpeg faults on this deployment invisible: ExitCode() is -1
// for every signal death, so a segfault and the stall watchdog's own SIGKILL
// produced the identical detail string.
func TestSummarizeProcessExit_DistinguishesCrashFromDeliberateKill(t *testing.T) {
	crash := summarizeProcessExit(exitErrorFromSignal(t, syscall.SIGSEGV))
	if !strings.HasPrefix(crash, ports.DetailEncoderCrashed) {
		t.Fatalf("a SIGSEGV must be reported as a crash, got %q", crash)
	}
	if !strings.Contains(crash, "segmentation fault") {
		t.Fatalf("the crash detail must name the signal, got %q", crash)
	}

	killed := summarizeProcessExit(exitErrorFromSignal(t, syscall.SIGKILL))
	if strings.HasPrefix(killed, ports.DetailEncoderCrashed) {
		t.Fatalf("a deliberate SIGKILL must not be reported as a crash, got %q", killed)
	}
	if killed == crash {
		t.Fatalf("crash and deliberate kill must not produce the same detail (%q)", killed)
	}
}

func TestIsProcessCrash_OnlyFaultSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGSEGV, syscall.SIGABRT, syscall.SIGBUS} {
		if _, crashed := isProcessCrash(exitErrorFromSignal(t, sig)); !crashed {
			t.Fatalf("%s must count as a crash", sig)
		}
	}
	for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
		if _, crashed := isProcessCrash(exitErrorFromSignal(t, sig)); crashed {
			t.Fatalf("%s is a deliberate termination and must not count as a crash", sig)
		}
	}
	// A non-zero exit status is not a crash either.
	err := exec.Command("sh", "-c", "exit 3").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if _, crashed := isProcessCrash(err); crashed {
		t.Fatal("a plain non-zero exit must not count as a crash")
	}
	if got := summarizeProcessExit(err); got != "process exit code 3" {
		t.Fatalf("expected the exit code to survive, got %q", got)
	}
}

// TestProcessDetailPriority_CrashOutranksSymptoms keeps the crash as the reported
// cause: a fault takes the encode path down, so the stall it also produces is a
// consequence and must not win the detail slot.
func TestProcessDetailPriority_CrashOutranksSymptoms(t *testing.T) {
	crash := processDetailPriority(ports.DetailEncoderCrashed + " (segmentation fault)")
	for _, symptom := range []string{
		"transcode stalled - no progress detected",
		"runtime path correctness failed - black output detected",
		"copy output missing codec parameters",
		"process exited unexpectedly",
	} {
		if got := processDetailPriority(symptom); got >= crash {
			t.Fatalf("crash priority %d must outrank %q (%d)", crash, symptom, got)
		}
	}
}
