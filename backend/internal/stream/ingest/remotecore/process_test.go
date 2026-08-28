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
func startHelper(t *testing.T, ctx context.Context, mode string) (*RemoteCore, error) {
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
	assertNoChildren(t)
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
	assertNoChildren(t)
}

// The gap the accept budget alone left open. A core that connects has already
// passed that budget; if the handshake gets a fresh one - or, when the caller
// passes a context without a deadline, none at all - then a core that connects
// and then says nothing pins Start forever. Which is precisely the core this
// whole package exists to survive.
//
// So: one budget for spawn, connect and handshake together.
func TestProcess_ConnectsThenNeverAnswersTheHandshake(t *testing.T) {
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
	assertNoChildren(t)
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
	assertNoChildren(t)
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
	assertNoChildren(t)
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
func assertNoChildren(t *testing.T) {
	t.Helper()
	out, err := exec.Command("ps", "-o", "ppid=,stat=", "-U", strconv.Itoa(os.Getuid())).Output()
	if err != nil {
		t.Skipf("cannot inspect processes here: %v", err)
	}
	// A defunct entry parented by this test is the failure this looks for.
	if containsZombieOf(string(out), os.Getpid()) {
		t.Errorf("a child of this process is defunct:\n%s", out)
	}
}
