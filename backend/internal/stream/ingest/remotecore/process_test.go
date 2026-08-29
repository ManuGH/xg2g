// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The misbehaving children are this test binary, re-executed in a mode named by
// an environment variable. Deliberately not the real core with extra flags: a
// production binary should not carry the ability to act broken, and a shell
// script cannot open a Unix socket.

const helperEnv = "XG2G_REMOTECORE_HELPER"

// helperMarkerEnv carries a path for a helper's child to write to, so a test can
// tell a descendant that was asked to leave from one that was killed.
const helperMarkerEnv = "XG2G_REMOTECORE_HELPER_MARKER"

// TestHelperProcess is not a test. It is the child.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("not a helper invocation")
	}

	sock := ""
	for i, a := range os.Args {
		if a == "--socket" && i+1 < len(os.Args) {
			sock = os.Args[i+1]
		}
	}

	switch mode {
	case "exit-before-connecting":
		os.Exit(3)

	case "hang-without-connecting":
		signal.Ignore(syscall.SIGTERM)
		// Sleeping rather than blocking forever: a goroutine with nothing to wait
		// for is a deadlock the runtime detects and exits on, which would make
		// this child die quickly instead of hanging - the opposite of the point.
		time.Sleep(10 * time.Minute)

	case "connect-then-ignore-sigterm":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		signal.Ignore(syscall.SIGTERM)
		// Answers the handshake so Start succeeds, then refuses to leave.
		serveOneHandshake(c)
		time.Sleep(10 * time.Minute)

	case "connect-then-never-answer-handshake":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		signal.Ignore(syscall.SIGTERM)
		// Connected, so accept succeeds - and then nothing. The socket stays open,
		// which is why no read on the parent side ever ends by itself.
		time.Sleep(10 * time.Minute)

	case "connect-then-never-answer-a-request":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		// Answers the handshake so Start succeeds, then takes a request and sits
		// on it. Leaves on SIGTERM: the point here is the hung request, not a
		// stubborn process.
		serveOneHandshake(c)
		time.Sleep(10 * time.Minute)

	case "connect-then-spawn-a-child-that-ignores-term":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		// No Setpgid: the child stays in this process group, which is the whole
		// point - it is what a core that shells out to something looks like.
		child := exec.Command("/bin/sh", "-c", `trap "" TERM; sleep 600`)
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
		serveOneHandshake(c)
		// Leaves when asked. The child does not.
		time.Sleep(10 * time.Minute)

	case "connect-then-spawn-a-child-that-notes-term":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		marker := os.Getenv(helperMarkerEnv)
		if marker == "" {
			os.Exit(6)
		}
		// "sleep & wait" rather than a plain sleep: a POSIX shell runs a trap only
		// once the current command finishes, so a foreground sleep would swallow
		// the signal for ten minutes.
		child := exec.Command("/bin/sh", "-c",
			`trap 'printf term > "$0"; exit 0' TERM; sleep 600 & wait`, marker)
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
		serveOneHandshake(c)
		time.Sleep(10 * time.Minute)

	case "connect-then-spawn-a-child-that-takes-its-time-leaving":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		marker := os.Getenv(helperMarkerEnv)
		if marker == "" {
			os.Exit(6)
		}
		// A descendant that does real work on the way out: notes that it started,
		// takes a while, notes that it finished. The leader leaves immediately, so
		// the only thing that can let this run to the end is a grace period that
		// waits for the group rather than for the leader.
		child := exec.Command("/bin/sh", "-c",
			`trap 'printf started >> "$0"; sleep 0.3; printf finished >> "$0"; exit 0' TERM; sleep 600 & wait`,
			marker)
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
		serveOneHandshake(c)
		time.Sleep(10 * time.Minute)

	case "connect-then-spawn-a-child-then-leave-when-asked":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		defer func() { _ = c.Close() }()
		// Spawns a descendant and then leaves on the goodbye message rather than
		// on a signal. That is what lets a test watch a shutdown in which no
		// signal was delivered at all, and check what survived it.
		child := exec.Command("/bin/sh", "-c", `sleep 600`)
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
		serveOneHandshake(c)
		serveOneHandshake(c)
		os.Exit(0)

	case "connect-then-die-mid-frame":
		c, err := dialHelper(sock)
		if err != nil {
			os.Exit(4)
		}
		serveOneHandshake(c)
		// Announces a frame and exits without it.
		_, _ = c.Write([]byte{0x00, 0x00, 0x00, 0x40, 0x01, 0x02})
		os.Exit(7)
	}
	os.Exit(0)
}

// startHelper is Start, with the child replaced by this binary running
// TestHelperProcess in a chosen mode.
//
// Skips where a core cannot be owned. RemoteCore refuses to start without a
// stable process identity, so off Linux there is no lifecycle here to test -
// only ErrCoreUnsupportedPlatform, which has its own test.
func startHelper(t *testing.T, ctx context.Context, mode string) (*RemoteCore, error) {
	t.Helper()
	requireOwnableCore(t)
	return startHelperAnywhere(t, ctx, mode)
}

// requireOwnableCore skips where no core can be started at all. Call it from the
// test body, not from a goroutine: a skip is a Goexit, and one of those in a
// goroutine ends the goroutine rather than the test.
func requireOwnableCore(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("RemoteCore needs Linux pidfd support; %v", ErrCoreUnsupportedPlatform)
	}
}

// startHelperAnywhere is startHelper without the platform skip, for the tests
// that are about a core refusing to start.
func startHelperAnywhere(t *testing.T, ctx context.Context, mode string) (*RemoteCore, error) {
	t.Helper()
	t.Setenv(helperEnv, mode)
	// The trailing "--" matters: this binary parses flags before any test runs,
	// and --socket is not one it knows. Everything after it is positional, which
	// the flag package ignores and os.Args still carries.
	return start(ctx, os.Args[0], []string{"-test.run=TestHelperProcess", "--"}, 1)
}

func dialHelper(sock string) (net.Conn, error) {
	if sock == "" {
		return nil, errors.New("no socket path")
	}
	return net.Dial("unix", sock)
}

// serveOneHandshake answers exactly the handshake, so the parent's Start
// succeeds and the test can get to the behaviour it is actually about.
func serveOneHandshake(c net.Conn) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		return
	}
	payload := make([]byte, binary.BigEndian.Uint32(lenBuf[:]))
	if _, err := io.ReadFull(c, payload); err != nil {
		return
	}
	f, err := DecodeHeader(payload)
	if err != nil {
		return
	}
	raw, err := Frame{Version: Version, Type: f.Type, RequestID: f.RequestID, Body: []byte{StatusOK}}.Encode()
	if err != nil {
		return
	}
	_, _ = c.Write(raw)
}

// containsZombieOf looks for a defunct process whose parent is pid.
func containsZombieOf(psOut string, pid int) bool {
	want := strconv.Itoa(pid)
	for _, line := range strings.Split(psOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == want && strings.HasPrefix(fields[1], "Z") {
			return true
		}
	}
	return false
}

// A core that exits before it ever connects is a crash, not a timeout - and the
// caller should not wait out the accept budget to be told so.
func TestProcess_ExitsBeforeConnecting(t *testing.T) {
	start := time.Now()
	core, err := startHelper(t, context.Background(), "exit-before-connecting")
	if core != nil {
		_ = core.Close()
	}
	if !errors.Is(err, mediafacts.ErrCoreCrashed) {
		t.Fatalf("Start err = %v, want ErrCoreCrashed", err)
	}
	if took := time.Since(start); took > startupTimeout {
		t.Errorf("Start took %v; a child that already exited should not be waited out", took)
	}
	assertNoZombieChildren(t)
}

// A core that starts, never connects, and ignores being asked to leave. Start
// must give up, and must not leave the process behind when it does.
func TestProcess_HangsWithoutConnecting(t *testing.T) {
	core, err := startHelper(t, context.Background(), "hang-without-connecting")
	if core != nil {
		_ = core.Close()
	}
	if !errors.Is(err, mediafacts.ErrCoreTimeout) {
		t.Fatalf("Start err = %v, want ErrCoreTimeout", err)
	}
	assertNoZombieChildren(t)
}

// The gap the accept budget alone left open. A core that connects has already
// passed that budget; if the handshake gets a fresh one - or, when the caller
// passes a context without a deadline, none at all - then a core that connects
// and then says nothing pins Start forever. Which is precisely the core this
// whole package exists to survive.
//
// So: one budget for spawn, connect and handshake together.
func TestProcess_ConnectsThenNeverAnswersTheHandshake(t *testing.T) {
	// Here rather than in startHelper alone: this one starts the core from a
	// goroutine, where a skip would end the goroutine and leave the test waiting.
	requireOwnableCore(t)
	began := time.Now()

	done := make(chan struct {
		core *RemoteCore
		err  error
	}, 1)
	go func() {
		// Background on purpose. The bound has to come from the package, not from
		// a caller who remembered to set one.
		c, err := startHelper(t, context.Background(), "connect-then-never-answer-handshake")
		done <- struct {
			core *RemoteCore
			err  error
		}{c, err}
	}()

	var res struct {
		core *RemoteCore
		err  error
	}
	select {
	case res = <-done:
	case <-time.After(startupTimeout + 20*time.Second):
		t.Fatal("Start never returned; a core that connected and went quiet pinned it")
	}
	if res.core != nil {
		_ = res.core.Close()
	}

	if !errors.Is(res.err, mediafacts.ErrCoreTimeout) {
		t.Fatalf("Start err = %v, want ErrCoreTimeout", res.err)
	}
	// The whole start, not the handshake alone: two budgets in sequence would
	// pass an assertion on either one of them separately.
	if took := time.Since(began); took > startupTimeout+3*time.Second {
		t.Errorf("Start took %v; spawn, connect and handshake share a budget of %v", took, startupTimeout)
	}
	assertNoZombieChildren(t)
}

// The one that matters most. A core that connects, works, and then refuses
// SIGTERM has to be killed and reaped - and Close has to return rather than
// waiting on a process that will never leave on its own.
func TestProcess_IgnoresSigtermAndIsKilledAndReaped(t *testing.T) {
	core, err := startHelper(t, context.Background(), "connect-then-ignore-sigterm")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	pid := core.cmd.Process.Pid

	done := make(chan error, 1)
	go func() { done <- core.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close err = %v, want nil - a killed core did what it was told, late", err)
		}
	case <-time.After(termGrace + 5*time.Second):
		t.Fatal("Close never returned; a core that ignores SIGTERM pinned it")
	}

	assertReaped(t, pid)
	assertNoZombieChildren(t)
}

// A core that dies in the middle of an answer is gone, and the caller learns it
// from the socket rather than from a wait.
func TestProcess_DiesMidFrame(t *testing.T) {
	core, err := startHelper(t, context.Background(), "connect-then-die-mid-frame")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = core.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = core.Ingest(ctx, 0, make([]byte, 188))
	if !errors.Is(err, mediafacts.ErrCoreGone) {
		t.Fatalf("Ingest err = %v, want ErrCoreGone", err)
	}
}

// Starting and stopping repeatedly must not accumulate anything - not processes,
// not sockets, not directories.
func TestProcess_RepeatedStartAndStopLeavesNothingBehind(t *testing.T) {
	for i := 0; i < 5; i++ {
		core, err := startHelper(t, context.Background(), "connect-then-ignore-sigterm")
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		if err := core.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
		if _, err := os.Stat(core.dir); !os.IsNotExist(err) {
			t.Errorf("round %d left %s behind", i, core.dir)
		}
	}
	assertNoZombieChildren(t)
}

// assertReaped fails if the pid is still a process this parent has not collected.
// A reaped child is gone entirely; a zombie answers signal 0.
func assertReaped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("pid %d is still present; it was not reaped", pid)
}

// assertNoChildren fails if this process has any child left, zombie or otherwise.
// assertNoZombieChildren looks for defunct children only. Named for what it
// checks: a live child of this process is not what this finds, and the earlier
// name promised otherwise.
func assertNoZombieChildren(t *testing.T) {
	t.Helper()
	// /proc first, because the runtime image has no ps and skipping there would
	// mean the acceptance run silently proves nothing - which is what it did
	// until this was written. ps stays as the fallback for hosts without /proc.
	if zombies, ok := zombieChildrenFromProc(os.Getpid()); ok {
		if zombies != "" {
			t.Errorf("a child of this process is defunct:\n%s", zombies)
		}
		return
	}
	out, err := exec.Command("ps", "-o", "ppid=,stat=", "-U", strconv.Itoa(os.Getuid())).Output()
	if err != nil {
		t.Skipf("cannot inspect processes here: %v", err)
	}
	if containsZombieOf(string(out), os.Getpid()) {
		t.Errorf("a child of this process is defunct:\n%s", out)
	}
}

// zombieChildrenFromProc lists defunct children of pid straight from /proc.
// Reports ok=false where there is no /proc to read, so the caller can fall back
// rather than mistake an unreadable directory for a clean one.
func zombieChildrenFromProc(pid int) (string, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", false
	}
	want := "PPid:\t" + strconv.Itoa(pid)
	var found []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		status, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			// Exited between listing and reading. Not a zombie: a zombie is
			// exactly the process that still has an entry.
			continue
		}
		text := string(status)
		if !strings.Contains(text, want+"\n") {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "State:") && strings.Contains(line, "Z") {
				found = append(found, e.Name()+" "+line)
			}
		}
	}
	return strings.Join(found, "\n"), true
}

// The zombie detector, tested against a real zombie.
//
// Without this the acceptance run inside the image would be back where it
// started: a check that always says "clean" is indistinguishable from a check
// that works, and the previous one skipped itself into exactly that.
func TestProcess_TheZombieDetectorSeesAZombie(t *testing.T) {
	if _, ok := zombieChildrenFromProc(os.Getpid()); !ok {
		t.Skip("no /proc here; this host uses the ps fallback")
	}

	// Started and deliberately not waited on: a child that has exited and not
	// been collected is the definition of the thing being looked for.
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if found, _ := zombieChildrenFromProc(os.Getpid()); strings.Contains(found, strconv.Itoa(cmd.Process.Pid)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	found, _ := zombieChildrenFromProc(os.Getpid())
	t.Fatalf("an unreaped exited child was not reported as defunct; detector saw %q", found)
}

// Close against a request that will never be answered.
//
// roundTrip serialises on a mutex, and the graceful shutdown message is itself a
// round trip. If it waits for that mutex, then a request in flight against a
// silent peer holds Close for as long as its own context allows - and an Ingest
// with a Background context holds it forever. The 200 ms budget on the shutdown
// exchange does nothing about a wait that happens before the exchange starts.
//
// So the graceful message is skipped when the connection is busy, and the socket
// is closed instead, outside the serialisation. That both bounds Close and frees
// the request that was stuck.
func TestProcess_CloseIsBoundedWhileARequestIsStuck(t *testing.T) {
	core, err := startHelper(t, context.Background(), "connect-then-never-answer-a-request")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := core.cmd.Process.Pid
	dir := core.dir

	ingestDone := make(chan error, 1)
	go func() {
		// Background on purpose: this is the caller who has no deadline to save
		// them, and Close is the only thing that can end this.
		_, err := core.Ingest(context.Background(), 0, make([]byte, 188))
		ingestDone <- err
	}()

	// Long enough for the Ingest to be inside roundTrip holding the mutex. Short
	// enough that the test is not measuring the sleep.
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-ingestDone:
		t.Fatalf("the helper answered after all; this test needs a stuck request (got %v)", err)
	default:
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- core.Close() }()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Errorf("Close err = %v, want nil", err)
		}
	case <-time.After(termGrace + 5*time.Second):
		t.Fatal("Close never returned; it waited on a request that will never finish")
	}

	select {
	case err := <-ingestDone:
		if err == nil {
			t.Error("the stuck Ingest returned success; closing the socket under it is a failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck Ingest never returned; closing the socket did not free it")
	}

	assertReaped(t, pid)
	assertNoZombieChildren(t)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s was left behind", dir)
	}
}

// A core that spawns something of its own.
//
// Setpgid exists so the core and its descendants can be ended together. Sending
// SIGTERM to the leader alone gives that up: the leader does as it is told, the
// wait completes, Close returns - and the grandchild is still running, now
// orphaned, with nothing left that knows about it.
//
// The process group is the unit. Checking it needs no knowledge of what the core
// spawned: while any member is alive the group id resolves, and signal 0 to the
// negated id succeeds. After Close it must not.
func TestProcess_ClosingTakesTheWholeProcessGroup(t *testing.T) {
	core, err := startHelper(t, context.Background(), "connect-then-spawn-a-child-that-ignores-term")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pgid := core.cmd.Process.Pid

	// The child is spawned before the handshake is answered, so by the time Start
	// has returned the group has more than one member. Asserted, because a test
	// that proves a group is gone when it never had anything in it proves nothing.
	if err := syscall.Kill(-pgid, 0); err != nil {
		t.Fatalf("the process group is not there to begin with: %v", err)
	}

	if err := core.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertReaped(t, core.cmd.Process.Pid)

	// The leader is reaped; the group must be empty. A surviving descendant keeps
	// the id resolvable, which is exactly the failure being looked for.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pgid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process group %d still has members after Close; the core left a descendant behind", pgid)
}

// The other half of the group question: not whether descendants are removed, but
// whether they are asked first.
//
// The test above passes on the closing sweep alone - a child that ignores SIGTERM
// is killed either way, so it cannot tell SIGTERM-to-the-leader from
// SIGTERM-to-the-group. This one can: the child handles the signal and records
// that it arrived. No marker means the group was never asked and the descendant
// was killed outright, which for anything holding state is a different outcome
// entirely.
func TestProcess_TheWholeGroupIsAskedBeforeItIsKilled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "term-received")
	t.Setenv(helperMarkerEnv, marker)

	core, err := startHelper(t, context.Background(), "connect-then-spawn-a-child-that-notes-term")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pgid := core.cmd.Process.Pid
	if err := syscall.Kill(-pgid, 0); err != nil {
		t.Fatalf("the process group is not there to begin with: %v", err)
	}

	if err := core.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The child writes the marker as it leaves, which races Close returning.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil && string(b) == "term" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the core's child never saw SIGTERM; it was killed rather than asked, " +
		"which means the signal went to the leader alone")
}

// The grace period is the group's, not the leader's.
//
// This is the same mistake as signalling only the leader, one step later. The
// core exits promptly when asked; if Close treats that as the end, a descendant
// that is halfway through its own SIGTERM handler is killed in the middle of it.
// For anything that flushes a buffer or writes out state on the way down, that
// is the difference between a clean stop and a torn one.
//
// The marker test above cannot see this: a handler that only records the signal
// finishes before the kill lands, so it stays green either way. This one takes
// long enough to be interrupted, and says so.
func TestProcess_TheGracePeriodBelongsToTheGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "term-handler")
	t.Setenv(helperMarkerEnv, marker)

	core, err := startHelper(t, context.Background(), "connect-then-spawn-a-child-that-takes-its-time-leaving")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pgid := core.cmd.Process.Pid
	if err := syscall.Kill(-pgid, 0); err != nil {
		t.Fatalf("the process group is not there to begin with: %v", err)
	}

	began := time.Now()
	if err := core.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	took := time.Since(began)

	// Close returns once the group is empty, so by then the child is done - no
	// polling here. If the handler had been cut short, Close would have returned
	// sooner, not later.
	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the child never ran its SIGTERM handler: %v", err)
	}
	switch string(b) {
	case "startedfinished":
	case "started":
		t.Error("the child was killed partway through its SIGTERM handler; " +
			"the grace period ended when the leader exited rather than when the group did")
	default:
		t.Errorf("unexpected handler trace %q", b)
	}

	// Waited for the group, not out the whole budget.
	if took >= termGrace {
		t.Errorf("Close took %v, the whole grace of %v; it should end when the group is empty", took, termGrace)
	}
	if took < 300*time.Millisecond {
		t.Errorf("Close returned after %v, before the child could have finished leaving", took)
	}

	if err := syscall.Kill(-pgid, 0); err == nil {
		t.Errorf("process group %d still has members after Close", pgid)
	}
	assertReaped(t, core.cmd.Process.Pid)
}
