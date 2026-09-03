// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build linux

package remotecore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// pidReuseEnv gates the adversary that deliberately recycles a pid. It needs a
// private pid namespace, so it is an acceptance run on the Linux host rather
// than something every CI job is entitled to do.
const pidReuseEnv = "XG2G_REMOTECORE_PID_REUSE"

// pidReuseChildEnv marks this binary re-executed as that adversary's child.
const pidReuseChildEnv = "XG2G_REMOTECORE_PID_REUSE_CHILD"

func procStateOf(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "gone"
	}
	text := string(raw)
	end := strings.LastIndex(text, ") ")
	if end < 0 {
		return "?"
	}
	return strings.Fields(text[end+2:])[0]
}

// startGroup starts a leader in its own process group.
//
// args go to /bin/sh, so callers can pass either -c and a line or the path of a
// script file - the second one where a nested quote would otherwise be the most
// likely thing to break.
func startGroup(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { collectOrphans(t) })
	return cmd
}

// leaderWithARecordingDescendant writes the script for a core that spawns
// something and leaves at once, and whose descendant writes down the signal it
// was given.
//
// The descendant announces itself once its handler is installed. Without that
// the test would be racing the shell: a TERM that arrives before the trap is in
// place takes the default action, and the descendant dies quietly instead of
// recording anything.
//
// The record itself is published by rename. The shell's > creates and truncates
// before printf writes, so a handler that wrote the marker in place would put an
// empty file at that path first, and a reader watching for the path to exist can
// win that race and read nothing.
func leaderWithARecordingDescendant(t *testing.T) (script, marker, ready string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "leader.sh")
	marker = filepath.Join(dir, "reached")
	ready = filepath.Join(dir, "ready")
	body := "#!/bin/sh\n" +
		"# $1: where the descendant records the signal. $2: its handler is armed.\n" +
		"( trap 'printf reached > \"${1}.part\"; mv \"${1}.part\" \"$1\"; exit 0' TERM\n" +
		"  : > \"$2\"\n" +
		"  sleep 300 & wait ) &\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- it is executed
		t.Fatalf("write leader script: %v", err)
	}
	return script, marker, ready
}

// waitForFile blocks until path exists, or fails the test.
//
// Existence is the whole observation, so a marker whose content is then read has
// to arrive complete or not at all - written somewhere else and renamed into
// place. A marker written straight to its path is observable, and empty, from
// the moment it is created. A marker that only signals by existing - the ready
// handshake below is written with `: >` and holds nothing - has nothing to tear.
func waitForFile(t *testing.T, path, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s never appeared", what, path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// socketDirs lists the socket directories a core would have created, so a test
// can tell one that was cleaned up from one that was left behind.
func socketDirs(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "xg2g-media-core-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		out[m] = true
	}
	return out
}

// A start that cannot own the core still has to take away everything it made.
//
// By the time the identity fails, the core has already spawned a descendant -
// which is the whole point: killing the leader alone leaves it running, and the
// leader is reaped a line later, after which the group can no longer be named
// safely at all. So the failure path takes the group, and takes it first.
func TestIdentity_AFailedAcquisitionTakesTheWholeGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant")
	t.Setenv(helperMarkerEnv, marker)
	before := socketDirs(t)

	var pgid int
	useIdentity(t, func(pid int) (processIdentity, error) {
		pgid = pid
		// Not before the core has a group to leave behind.
		waitForFile(t, marker, "the core's descendant")
		return nil, ErrCoreUnsupportedPlatform
	})

	core, err := startHelper(t, context.Background(), "spawn-a-descendant-then-wait-to-be-owned")
	if core != nil {
		_ = core.Close()
		t.Fatal("Start returned a core it could not own")
	}
	if !errors.Is(err, ErrCoreUnsupportedPlatform) {
		t.Fatalf("Start err = %v, want ErrCoreUnsupportedPlatform", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read the descendant's pid: %v", err)
	}
	descendant, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("descendant pid %q: %v", raw, err)
	}

	if state := procStateOf(pgid); state != "gone" {
		t.Errorf("the leader is %q after a refused start; it should have been reaped", state)
	}

	// Nothing of that group may still be running. Whether the corpses have been
	// collected is the deployment's init contract and not this one, so a zombie
	// counts as dealt with here.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var running []int
		for pid, state := range groupMembers(t, pgid) {
			if state != "Z" {
				running = append(running, pid)
			}
		}
		if len(running) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still running in group %d after a refused start: %v - "+
				"the failure path took the leader and left what it had spawned", pgid, running)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state := procStateOf(descendant); state != "gone" && state != "Z" {
		t.Errorf("the descendant %d is %q; a start that refuses the core must not leave behind what it spawned",
			descendant, state)
	}

	// Where this process is its namespace's init - a container without one - the
	// descendant we just killed comes back to us as a zombie. Play init before
	// asking whether init did its job; anywhere else this does nothing.
	collectOrphans(t)
	assertNoZombieChildren(t)
	for dir := range socketDirs(t) {
		if !before[dir] {
			t.Errorf("socket directory left behind: %s", dir)
		}
	}
}

// The kernel property the whole design rests on, stated as a test.
//
// A pidfd is not just a stable name for the leader: it keeps naming the process
// group the leader had, after the leader has been reaped. That is why the wait
// in this package can go on reaping immediately, why no zombie has to be held
// open to pin a number, and why WNOWAIT is not in this design.
//
// The proof is the descendant's own record of the signal rather than the group
// emptying afterwards: whether a dead descendant then disappears depends on
// whoever reaps it, and that is the deployment's contract, not this one.
func TestIdentity_TheGroupOutlivesTheLeader(t *testing.T) {
	script, marker, ready := leaderWithARecordingDescendant(t)
	cmd := startGroup(t, script, marker, ready)
	pid := cmd.Process.Pid
	waitForFile(t, ready, "the descendant's TERM handler")

	id, err := acquireProcessIdentity(pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = id.Close() }()

	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if procStateOf(pid) != "gone" {
		t.Fatalf("leader %d was not reaped; this test is about what happens after it is", pid)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the descendant recorded a signal before one was sent")
	}

	alive, err := id.GroupExists()
	if err != nil || !alive {
		t.Fatalf("GroupExists after the leader was reaped = %v, %v; the descendant is still there", alive, err)
	}
	if err := id.SignalGroup(syscall.SIGTERM); err != nil {
		t.Fatalf("SignalGroup(TERM) after the leader was reaped: %v", err)
	}

	waitForFile(t, marker, "the descendant never saw the signal; the group reference did not survive the leader")
	if b, err := os.ReadFile(marker); err != nil || string(b) != "reached" {
		t.Fatalf("descendant wrote %q (%v)", b, err)
	}
}

// And a group that is gone is gone - never a group that merely holds the number
// now. No descendant here, so the group empties the moment the leader is reaped.
func TestIdentity_AnEmptyGroupAnswersAsEmpty(t *testing.T) {
	cmd := startGroup(t, "-c", "exit 0")
	pid := cmd.Process.Pid

	id, err := acquireProcessIdentity(pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = id.Close() }()

	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	alive, err := id.GroupExists()
	if err != nil {
		t.Fatalf("GroupExists: %v", err)
	}
	if alive {
		t.Error("an empty group reported as present")
	}
	if err := id.SignalGroup(syscall.SIGTERM); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("SignalGroup on an empty group = %v, want ESRCH", err)
	}
}

// The identity has to be taken while the leader is still the kernel's to name.
//
// The helper exits at once, so the only reason it is still visible when the
// identity is acquired is that nothing has reaped it yet. Start the waiter first
// and this sees a process that is gone.
func TestIdentity_IsAcquiredBeforeAnythingCanReapTheLeader(t *testing.T) {
	observed := "never called"
	useIdentity(t, func(pid int) (processIdentity, error) {
		deadline := time.Now().Add(3 * time.Second)
		observed = "still running at the deadline"
		for time.Now().Before(deadline) {
			if st := procStateOf(pid); st == "Z" || st == "gone" {
				observed = st
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return acquireProcessIdentity(pid)
	})

	core, err := startHelper(t, context.Background(), "exit-before-connecting")
	if core != nil {
		_ = core.Close()
	}
	if observed != "Z" {
		t.Errorf("the leader was %q when the identity was taken, want \"Z\": "+
			"something reaped it first, and a reaped pid is the kernel's to hand out again", observed)
	}
	if !errors.Is(err, mediafacts.ErrCoreCrashed) {
		t.Errorf("Start err = %v, want ErrCoreCrashed", err)
	}
}

// The pidfd must name the group leader. Anything else answers ESRCH for the
// group, which would leave the lifecycle signalling nothing at all.
func TestIdentity_RefusesAProcessThatIsNotItsOwnGroupLeader(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 5")
	if err := cmd.Start(); err != nil { // no Setpgid: it stays in this test's group
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	id, err := acquireProcessIdentity(cmd.Process.Pid)
	if err == nil {
		_ = id.Close()
		t.Fatal("acquired a group identity for a process that does not lead a group")
	}
}

// One fd, closed once. A pidfd closed twice is a file descriptor number closed
// twice, and the second one belongs to whoever opened something in between.
func TestIdentity_CloseIsIdempotent(t *testing.T) {
	cmd := startGroup(t, "-c", "sleep 5")
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	id, err := acquireProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := id.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := id.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := id.SignalGroup(syscall.SIGTERM); !errors.Is(err, errIdentityClosed) {
		t.Errorf("SignalGroup after Close = %v, want errIdentityClosed", err)
	}
}

// The adversary the numeric form cannot survive.
//
// A real pid wraparound would take hours and prove nothing repeatable, so the
// pid is recycled deliberately: in a private pid namespace, ns_last_pid decides
// what the next fork gets. An unrelated process group then holds the number the
// core used to have, and the two signalling paths are asked to deal with it.
func TestIdentity_TheOriginalGroupIsNeverAnotherGroup(t *testing.T) {
	if os.Getenv(pidReuseEnv) == "" {
		t.Skipf("%s not set; this is the LXC acceptance run, not a unit test", pidReuseEnv)
	}
	if os.Geteuid() != 0 {
		t.Skip("a private pid namespace needs CAP_SYS_ADMIN")
	}

	cmd := exec.Command("unshare", "--pid", "--fork", "--mount-proc",
		os.Args[0], "-test.run=TestIdentity_PidReuseAdversaryChild", "-test.v")
	cmd.Env = append(os.Environ(), pidReuseChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	t.Logf("adversary output:\n%s", out)
	if err != nil {
		t.Fatalf("the pid-reuse adversary failed: %v", err)
	}
}

// TestIdentity_PidReuseAdversaryChild is not a test on its own. It is the body
// of the adversary above, running as pid 1 of a private pid namespace.
func TestIdentity_PidReuseAdversaryChild(t *testing.T) {
	if os.Getenv(pidReuseChildEnv) == "" {
		t.Skip("not the adversary child")
	}
	if os.Getpid() != 1 {
		t.Fatalf("running as pid %d; the adversary needs to be pid 1 of its own namespace", os.Getpid())
	}

	core := startGroup(t, "-c", "sleep 300 & exit 0")
	pid := core.Process.Pid
	id, err := acquireProcessIdentity(pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// The early reap, exactly as the lifecycle does it, and then the group after
	// it - so the number is free for someone else.
	if err := core.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := id.SignalGroup(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("clearing the original group: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		collectOrphans(t)
		alive, err := id.GroupExists()
		if err != nil {
			t.Fatalf("GroupExists: %v", err)
		}
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the original group never emptied")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Hand the number to somebody else.
	if err := os.WriteFile("/proc/sys/kernel/ns_last_pid", []byte(strconv.Itoa(pid-1)), 0o644); err != nil {
		t.Fatalf("steer the next pid: %v", err)
	}
	bystander := startGroup(t, "-c", "sleep 300 & sleep 300")
	defer func() {
		_ = syscall.Kill(-bystander.Process.Pid, syscall.SIGKILL)
		_ = bystander.Wait()
	}()
	if bystander.Process.Pid != pid {
		t.Skipf("could not place an unrelated group on pid %d (got %d)", pid, bystander.Process.Pid)
	}
	time.Sleep(150 * time.Millisecond)
	before := groupMembers(t, pid)
	if len(before) < 2 {
		t.Fatalf("the bystander group is not there to be protected: %v", before)
	}

	// The identity is about the group the core had, not about the number.
	if err := id.SignalGroup(syscall.SIGTERM); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("SignalGroup on the recycled number = %v, want ESRCH", err)
	}
	if err := id.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	for p, state := range before {
		if now := procStateOf(p); now != state {
			t.Errorf("bystander %d went from %q to %q; the identity reached a group that was not its own", p, state, now)
		}
	}

	// And the mutation this adversary exists to catch: by number, the same
	// shutdown kills a process group it never started.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatalf("numeric kill: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	hit := false
	for p := range before {
		if procStateOf(p) != before[p] {
			hit = true
		}
	}
	if !hit {
		t.Error("kill(-pgid) left the bystander alone; then this adversary cannot catch the mutation it exists for")
	}
}
