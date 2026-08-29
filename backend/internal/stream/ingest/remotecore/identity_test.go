// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recordingIdentity is the real thing with a notebook: every group operation is
// written down and then performed, so a test can assert what the lifecycle asked
// for without changing what happens.
type recordingIdentity struct {
	inner processIdentity

	mu      sync.Mutex
	signals []syscall.Signal
	asked   int
	closes  int
}

func (r *recordingIdentity) SignalGroup(sig syscall.Signal) error {
	r.mu.Lock()
	r.signals = append(r.signals, sig)
	r.mu.Unlock()
	return r.inner.SignalGroup(sig)
}

func (r *recordingIdentity) GroupExists() (bool, error) {
	r.mu.Lock()
	r.asked++
	r.mu.Unlock()
	return r.inner.GroupExists()
}

func (r *recordingIdentity) Close() error {
	r.mu.Lock()
	r.closes++
	r.mu.Unlock()
	return r.inner.Close()
}

func (r *recordingIdentity) sent() []syscall.Signal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]syscall.Signal(nil), r.signals...)
}

// swallowingIdentity accepts every group operation and performs none of them.
//
// Whatever still dies during a shutdown under this identity was reached some
// other way - which is the only way left, and the one this package is not
// allowed to have.
type swallowingIdentity struct {
	mu      sync.Mutex
	signals []syscall.Signal
}

func (s *swallowingIdentity) SignalGroup(sig syscall.Signal) error {
	s.mu.Lock()
	s.signals = append(s.signals, sig)
	s.mu.Unlock()
	return nil
}

// GroupExists says the group is gone so the grace ends at once: this test is
// about what was signalled, not about waiting two seconds for it.
func (s *swallowingIdentity) GroupExists() (bool, error) { return false, nil }
func (s *swallowingIdentity) Close() error               { return nil }

func (s *swallowingIdentity) sent() []syscall.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]syscall.Signal(nil), s.signals...)
}

// useIdentity puts an ownership layer under the next Start and takes it out
// again afterwards.
func useIdentity(t *testing.T, f func(pid int) (processIdentity, error)) {
	t.Helper()
	original := acquireIdentity
	acquireIdentity = f
	t.Cleanup(func() { acquireIdentity = original })
}

// collectOrphans plays init, but only where this process actually is one.
//
// A core's descendants are reparented when their leader leaves. In a private
// pid namespace - a container without an init, or the adversary below - that
// means they come back to this process, and nothing else will ever collect
// them. Anywhere else they belong to the real init, and waiting blindly here
// would collect a child somebody else is waiting for.
func collectOrphans(t *testing.T) {
	t.Helper()
	if os.Getpid() != 1 {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		for {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
			if pid <= 0 || err != nil {
				break
			}
		}
		if left, ok := zombieChildrenFromProc(os.Getpid()); !ok || left == "" {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// groupMembers reports every process in pgid, with its state, straight from
// /proc. Tests may look at numbers; the lifecycle may not.
func groupMembers(t *testing.T, pgid int) map[int]string {
	t.Helper()
	out := map[int]string{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skipf("no /proc here: %v", err)
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		text := string(raw)
		end := strings.LastIndex(text, ") ")
		if end < 0 {
			continue
		}
		fields := strings.Fields(text[end+2:])
		if len(fields) < 3 {
			continue
		}
		if fields[2] == strconv.Itoa(pgid) {
			out[pid] = fields[0]
		}
	}
	return out
}

// A host that cannot name a process group does not get to run a core.
//
// The tempting alternative is a fallback to kill(-pgid, ...) "just for old
// kernels", which would put the race back exactly where this identity was
// introduced to remove it. So: no core, and an error that says why.
func TestIdentity_StartRefusesWithoutAStableIdentity(t *testing.T) {
	useIdentity(t, func(int) (processIdentity, error) {
		return nil, ErrCoreUnsupportedPlatform
	})

	core, err := startHelperAnywhere(t, context.Background(), "connect-then-ignore-sigterm")
	if core != nil {
		_ = core.Close()
		t.Fatal("Start returned a core on a platform that cannot own one")
	}
	if !errors.Is(err, ErrCoreUnsupportedPlatform) {
		t.Fatalf("Start err = %v, want ErrCoreUnsupportedPlatform", err)
	}
	// Refusing is not enough: the process was already started when the refusal
	// happened, so it has to be gone too.
	assertNoZombieChildren(t)
}

// Asked, then told - and both through the identity.
func TestIdentity_TermAndKillGoThroughTheIdentity(t *testing.T) {
	var rec *recordingIdentity
	useIdentity(t, func(pid int) (processIdentity, error) {
		inner, err := acquireProcessIdentity(pid)
		if err != nil {
			return nil, err
		}
		rec = &recordingIdentity{inner: inner}
		return rec, nil
	})

	core, err := startHelper(t, context.Background(), "connect-then-ignore-sigterm")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := core.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
	got := rec.sent()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("signals through the identity = %v, want %v", got, want)
	}
	if rec.asked == 0 {
		t.Error("the grace period never asked the identity whether the group was still there")
	}
	if rec.closes != 1 {
		t.Errorf("identity closed %d times, want exactly 1", rec.closes)
	}
}

// The mutation test: an identity that delivers nothing must leave the core's
// descendant alive. Anything that still reaches it was addressed by number.
func TestIdentity_NothingIsSignalledByNumber(t *testing.T) {
	swallow := &swallowingIdentity{}
	useIdentity(t, func(int) (processIdentity, error) { return swallow, nil })

	// A core that leaves on the goodbye message rather than on a signal, so the
	// shutdown can complete without anything being delivered.
	core, err := startHelper(t, context.Background(), "connect-then-spawn-a-child-then-leave-when-asked")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pgid := core.cmd.Process.Pid

	before := groupMembers(t, pgid)
	if len(before) < 2 {
		t.Fatalf("expected a leader and a descendant in group %d, got %v", pgid, before)
	}

	if err := core.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Whatever the test leaves behind is the test's to clean up - by number,
	// which is allowed here and nowhere in the package.
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		collectOrphans(t)
	}()

	after := groupMembers(t, pgid)
	alive := 0
	for pid, state := range after {
		if pid != pgid && state != "Z" {
			alive++
		}
	}
	if alive == 0 {
		t.Errorf("the core's descendant did not survive a shutdown that signalled nothing: "+
			"before=%v after=%v - the group was reached by number", before, after)
	}
	if sent := swallow.sent(); len(sent) == 0 || sent[0] != syscall.SIGTERM {
		t.Errorf("signals offered to the identity = %v, want SIGTERM first", sent)
	}
}

// A gate rather than a test: the numeric form must not come back by accident.
//
// Exactly one file may name a process group by number, for the one moment where
// the number is provably still this core's - see start_failure_cleanup.go. The
// exception is written down here so it cannot quietly spread, and so that
// moving it silently is not an option either.
func TestIdentity_ThePackageNeverSignalsAProcessGroupByNumber(t *testing.T) {
	const exception = "start_failure_cleanup.go"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	exceptionUses, sources := 0, 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		sources++
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		uses := strings.Count(string(body), "Kill(-")
		if name == exception {
			exceptionUses = uses
			continue
		}
		if uses > 0 {
			t.Errorf("%s signals a process group by number %d time(s); the running lifecycle has an identity for that", name, uses)
		}
	}
	if sources == 0 {
		// A compiled test binary run somewhere else - the image acceptance, for
		// one - has no sources to read. Saying nothing would be worse than saying
		// that: a gate that silently inspects an empty directory always passes.
		t.Skip("no package sources here; this gate reads the files it guards")
	}
	if exceptionUses != 1 {
		t.Errorf("%s names a process group by number %d time(s), want exactly 1: "+
			"if the pre-identity cleanup moved, this gate has to move with it", exception, exceptionUses)
	}
}
