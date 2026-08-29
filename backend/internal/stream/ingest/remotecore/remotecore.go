// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// startupTimeout bounds getting a core to the point where it can be asked
// something: spawned, connected, and past the handshake.
//
// One budget for all of it rather than one per step. Two independent budgets add
// up, and worse, the handshake had none at all when the caller passed a context
// without a deadline - so a core that connected and then said nothing pinned
// Start forever, which is exactly the core this is built to survive.
//
// Generous compared to what it takes: a process that is going to connect and
// answer does both in milliseconds.
const startupTimeout = 5 * time.Second

// termGrace is how long a core gets to leave after being asked politely.
//
// Long enough for a process that is finishing a write, short enough that shutdown
// is not something a caller notices. What follows it is not another request.
const termGrace = 2 * time.Second

// RemoteCore is a mediafacts.Core living in another process.
//
// It implements the contract and adds nothing to it. In particular it does not
// decide when it has failed too often, does not retry, and does not replace its
// own process: a core that failed is finished, and the ring is what knows that.
// Reviving one here would be a second answer to a question mediafacts already
// answers, and the two would eventually disagree.
type RemoteCore struct {
	cmd  *exec.Cmd
	conn *conn
	dir  string
	ln   net.Listener

	// identity names this core's process group for the rest of its life. Taken
	// before anything can reap the leader, released after the last group
	// operation - and it is the only way this type addresses the group.
	identity processIdentity

	// waitDone closes when the child has been reaped. There is exactly one Wait,
	// started here, so nothing else may call it and nothing is left unreaped.
	waitDone chan struct{}
	waitErr  error

	// waitStarted says whether that Wait is running. Between starting the process
	// and starting the goroutine there is a moment where nobody else will reap it.
	waitStarted bool

	closeOnce sync.Once
	closeErr  error
}

var _ mediafacts.Core = (*RemoteCore)(nil)

// Start launches one core process and completes the handshake with it.
//
// Everything it creates it owns: the socket directory, the listener, the process
// and the goroutine that reaps it. A failure anywhere in here tears all of that
// down before returning, because a half-started core is indistinguishable from a
// running one to whoever gets the error.
func Start(ctx context.Context, binaryPath string, targetProgram uint16) (*RemoteCore, error) {
	return start(ctx, binaryPath, nil, targetProgram)
}

// start is Start with room for arguments the tests need to put in front of
// --socket. Production never passes any.
func start(ctx context.Context, binaryPath string, extraArgs []string, targetProgram uint16) (core *RemoteCore, err error) {
	// 0700: the socket is between this process and its own child. Nothing else on
	// the machine has business connecting to it, and a socket in a world-readable
	// directory is an invitation to whatever else runs as this user.
	dir, err := os.MkdirTemp("", "xg2g-media-core-*")
	if err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	// #nosec G302 -- a directory needs its execute bit to be entered at all; 0700
	// is the tightest setting that still lets this process reach its own socket.
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("restrict socket directory: %w", err)
	}

	sock := filepath.Join(dir, "core.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen on %s: %w", sock, err)
	}

	// The 0700 directory already makes this unreachable to anything else, but a
	// socket left at whatever the umask produced is a second thing to reason about
	// if that directory is ever relaxed. Tightened so there is only one.
	if err := os.Chmod(sock, 0o600); err != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("restrict socket: %w", err)
	}

	r := &RemoteCore{dir: dir, ln: ln, waitDone: make(chan struct{})}
	defer func() {
		if err != nil {
			r.teardown()
		}
	}()

	args := append(append([]string{}, extraArgs...), "--socket", sock)
	// #nosec G204 -- the path is where the operator installed the media core, not
	// anything a request can influence. A core at a fixed literal path would be a
	// core that cannot be tested or relocated.
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// Its own process group, so a signal aimed at the core cannot reach anything
	// else and a core that spawns something of its own can still be cleaned up.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binaryPath, err)
	}
	r.cmd = cmd

	// Before the reaper, not after. A pidfd taken once something could already
	// have reaped the leader is a reference to a number the kernel may have
	// handed to somebody else - which is the whole failure this identity exists
	// to remove. Nothing reaps here yet, so this is the one moment where the pid
	// is unambiguously ours.
	id, idErr := acquireIdentity(cmd.Process.Pid)
	if idErr != nil {
		// The group, not just the leader: the core may already have spawned
		// something, and a core that will not be run is no reason to leave it
		// behind. See killTheGroupBeforeAnythingReapsIt for why addressing it by
		// number is correct in this one place and nowhere else - and why it has
		// to happen before the Wait below.
		killTheGroupBeforeAnythingReapsIt(cmd)
		_ = cmd.Wait()
		err = idErr
		return nil, err
	}
	r.identity = id

	go func() {
		r.waitErr = cmd.Wait()
		close(r.waitDone)
	}()
	r.waitStarted = true

	// Everything from here to a usable core shares one deadline.
	startCtx, cancelStart := context.WithTimeout(ctx, startupTimeout)
	defer cancelStart()

	c, err := r.accept(startCtx)
	if err != nil {
		return nil, err
	}
	r.conn = newConn(c)

	if err = r.handshake(startCtx, targetProgram); err != nil {
		return nil, err
	}
	return r, nil
}

// accept waits for the child to connect, and stops waiting if the child dies
// first - otherwise a core that exits immediately would be a five second pause
// followed by a timeout, rather than what it is.
func (r *RemoteCore) accept(ctx context.Context) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	got := make(chan result, 1)
	go func() {
		c, err := r.ln.Accept()
		got <- result{c, err}
	}()

	select {
	case res := <-got:
		if res.err != nil {
			return nil, fmt.Errorf("%w: accept: %v", mediafacts.ErrCoreGone, res.err)
		}
		return res.c, nil
	case <-r.waitDone:
		_ = r.ln.Close()
		return nil, fmt.Errorf("%w: exited before connecting: %v", mediafacts.ErrCoreCrashed, r.waitErr)
	case <-ctx.Done():
		_ = r.ln.Close()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: did not connect within %v", mediafacts.ErrCoreTimeout, startupTimeout)
		}
		return nil, ctx.Err()
	}
}

func (r *RemoteCore) handshake(ctx context.Context, targetProgram uint16) error {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, targetProgram)

	resp, err := r.conn.roundTrip(ctx, Frame{Type: MsgHandshake, Body: body})
	if err != nil {
		return err
	}
	if err := statusError(resp); err != nil {
		return err
	}
	return exactBody(resp, 1)
}

// Ingest hands one chunk to the core process.
func (r *RemoteCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	if startOffset < 0 {
		return mediafacts.ParseResult{}, fmt.Errorf("negative start offset %d", startOffset)
	}

	body := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(body[:8], uint64(startOffset))
	copy(body[8:], data)

	resp, err := r.conn.roundTrip(ctx, Frame{Type: MsgIngest, Body: body})
	if err != nil {
		return mediafacts.ParseResult{}, err
	}
	if err := statusError(resp); err != nil {
		return mediafacts.ParseResult{}, err
	}
	// Response body is one status byte, then the offset. Exactly that: extra bytes
	// mean the peer is sending something this build does not understand, and a
	// reader that ignores them cannot tell that from a peer it agrees with.
	if err := exactBody(resp, 1+8); err != nil {
		return mediafacts.ParseResult{}, err
	}
	// The peer supplied this number. A value past what an offset can be is not a
	// large offset, it is a peer that is failing - and converting it blindly would
	// hand the caller a negative one.
	through := binary.BigEndian.Uint64(resp.Body[1:9])
	if through > math.MaxInt64 {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: answered with offset %d",
			mediafacts.ErrCoreInvalidResponse, through)
	}
	return mediafacts.ParseResult{ProcessedThroughOffset: int64(through)}, nil
}

// SetTargetProgram selects the program the core follows.
func (r *RemoteCore) SetTargetProgram(ctx context.Context, programNumber uint16) (mediafacts.ParseResult, error) {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, programNumber)

	resp, err := r.conn.roundTrip(ctx, Frame{Type: MsgSetTargetProgram, Body: body})
	if err != nil {
		return mediafacts.ParseResult{}, err
	}
	if err := statusError(resp); err != nil {
		return mediafacts.ParseResult{}, err
	}
	if err := exactBody(resp, 1); err != nil {
		return mediafacts.ParseResult{}, err
	}
	return mediafacts.ParseResult{}, nil
}

// Close ends the core process and reaps it.
//
// Asked, then told, then reaped - and the reaping is not conditional on either of
// the first two working. A core that ignores SIGTERM is not a reason to leave a
// process behind, and a core that was already gone is not a reason to report a
// failure.
func (r *RemoteCore) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.shutdown() })
	return r.closeErr
}

func (r *RemoteCore) shutdown() error {
	if r.conn != nil {
		// Best effort, briefly, and only if the connection is free. A core that
		// will not take the message is about to be signalled anyway, and one whose
		// connection is held by a request that will never be answered must not be
		// waited on at all - the 200 ms below bounds the exchange, not the wait for
		// the mutex in front of it.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, _, _ = r.conn.tryRoundTrip(ctx, Frame{Type: MsgShutdown})
		cancel()
		// Outside the request serialisation on purpose. This is the lifecycle
		// ending, not a request being made, and it is what unblocks a read or
		// write that is still in progress.
		_ = r.conn.close()
	}
	if r.ln != nil {
		_ = r.ln.Close()
	}

	if r.cmd == nil || r.cmd.Process == nil {
		r.cleanupDir()
		return nil
	}

	// The group, not the leader - and the group by name, not by number. Setpgid
	// put the core in its own process group so that its descendants could be
	// ended with it; signalling only the leader gives that up, and addressing the
	// group by pgid gives away who is on the other end of it.
	_ = r.identity.SignalGroup(syscall.SIGTERM)

	// And the grace belongs to the group too.
	//
	// Waiting for the leader and stopping there is the same mistake one step
	// later: the core exits promptly when asked, the wait completes, and a
	// descendant that is halfway through its own SIGTERM handler - flushing a
	// buffer, closing a file, writing out state - is killed in the middle of it.
	// The leader is one member of the lifecycle unit, not the unit.
	//
	// So the grace ends when the group is empty, or when it runs out.
	grace := time.NewTimer(termGrace)
	defer grace.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()

	leaderDone := false
	waitDone := r.waitDone
	for done := false; !done; {
		// An error is not an answer, and the safe reading of no answer is "still
		// there": waiting the grace out costs two seconds, leaving early on a
		// guess costs a descendant the end of its shutdown.
		if alive, err := r.identity.GroupExists(); err == nil && !alive {
			break
		}
		select {
		case <-waitDone:
			// Noted, not acted on. Nilling the channel keeps this case from
			// spinning; the loop goes on waiting for the rest of the group.
			leaderDone = true
			waitDone = nil
		case <-poll.C:
		case <-grace.C:
			// It was asked. Now it is told.
			_ = r.identity.SignalGroup(syscall.SIGKILL)
			done = true
		}
	}
	if !leaderDone {
		<-r.waitDone
	}

	// After the last group operation, and only then.
	_ = r.identity.Close()

	r.cleanupDir()

	// A core that exited because it was killed did what it was told to do, late.
	// That is not an error to report to whoever called Close.
	if r.waitErr != nil && !errors.As(r.waitErr, new(*exec.ExitError)) {
		return r.waitErr
	}
	return nil
}

// teardown is the failure path of Start: everything created, in reverse.
func (r *RemoteCore) teardown() {
	if r.conn != nil {
		_ = r.conn.close()
	}
	if r.ln != nil {
		_ = r.ln.Close()
	}
	if r.cmd != nil && r.cmd.Process != nil && r.identity != nil {
		_ = r.identity.SignalGroup(syscall.SIGKILL)
	}
	if r.waitStarted {
		<-r.waitDone
	}
	if r.identity != nil {
		_ = r.identity.Close()
	}
	r.cleanupDir()
}

func (r *RemoteCore) cleanupDir() {
	if r.dir != "" {
		_ = os.RemoveAll(r.dir)
	}
}

// exactBody rejects an answer whose shape is not the one agreed for it.
func exactBody(resp Frame, want int) error {
	if len(resp.Body) != want {
		return fmt.Errorf("%w: answer carried %d body bytes, expected %d",
			mediafacts.ErrCoreInvalidResponse, len(resp.Body), want)
	}
	return nil
}

func statusError(resp Frame) error {
	if len(resp.Body) == 0 {
		return fmt.Errorf("%w: answer carried no status", mediafacts.ErrCoreInvalidResponse)
	}
	switch resp.Body[0] {
	case StatusOK:
		return nil
	case StatusProtocolVersion:
		return fmt.Errorf("%w: peer refused this build's version %d", mediafacts.ErrCoreProtocolVersion, Version)
	case StatusMalformed, StatusUnknownMessage:
		return fmt.Errorf("%w: peer rejected the request (status %d)", mediafacts.ErrCoreInvalidResponse, resp.Body[0])
	default:
		return fmt.Errorf("%w: peer reported status %d", mediafacts.ErrCoreInvalidResponse, resp.Body[0])
	}
}
