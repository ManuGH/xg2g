package ports

import (
	"testing"
	"time"
)

func at(base time.Time, d time.Duration) time.Time { return base.Add(d) }

// TestScrambleObserver_ReceiverWideWhenNothingDescrambles reproduces the morning
// of 2026-08-08: two different services on the same transponder read 86% and 93%
// scrambled within half an hour and nothing came through clear. Both played fine
// after the receiver was restarted, so the honest attribution is the receiver —
// not the two services, and not the subscription.
func TestScrambleObserver_ReceiverWideWhenNothingDescrambles(t *testing.T) {
	now := time.Now()
	o := NewScrambleObserver(15*time.Minute, 2)

	o.Observe("1:0:19:83:6:85:C00000:0:0:0", true, at(now, 0))
	if got := o.Scope(at(now, time.Second)); got != ScrambleScopeService {
		t.Fatalf("one failing service is not enough to blame the receiver; got %q", got)
	}

	o.Observe("1:0:19:81:6:85:C00000:0:0:0", true, at(now, 2*time.Minute))
	if got := o.Scope(at(now, 2*time.Minute)); got != ScrambleScopeReceiver {
		t.Fatalf("two distinct services failing with nothing clear is a receiver fault; got %q", got)
	}
}

// TestScrambleObserver_OneClearProvesTheReceiverWorks is the guard against the
// worse error. Telling someone their receiver is broken when it is not sends them
// rebooting hardware over a channel they simply are not entitled to, so a single
// successful descramble anywhere in the window ends the receiver verdict.
func TestScrambleObserver_OneClearProvesTheReceiverWorks(t *testing.T) {
	now := time.Now()
	o := NewScrambleObserver(15*time.Minute, 2)

	o.Observe("svc-a", true, at(now, 0))
	o.Observe("svc-b", true, at(now, time.Minute))
	if got := o.Scope(at(now, time.Minute)); got != ScrambleScopeReceiver {
		t.Fatalf("precondition: expected a receiver verdict, got %q", got)
	}

	o.Observe("svc-c", false, at(now, 2*time.Minute))
	if got := o.Scope(at(now, 2*time.Minute)); got != ScrambleScopeService {
		t.Fatalf("a service that descrambled proves the receiver works; got %q", got)
	}
}

// TestScrambleObserver_RecoveryRetractsTheVerdict covers a service coming back:
// only the latest outcome per service counts, so a recovered service stops
// arguing for a fault that no longer exists.
func TestScrambleObserver_RecoveryRetractsTheVerdict(t *testing.T) {
	now := time.Now()
	o := NewScrambleObserver(15*time.Minute, 2)

	o.Observe("svc-a", true, at(now, 0))
	o.Observe("svc-b", true, at(now, time.Minute))
	if got := o.Scope(at(now, time.Minute)); got != ScrambleScopeReceiver {
		t.Fatalf("precondition: expected a receiver verdict, got %q", got)
	}

	o.Observe("svc-a", false, at(now, 3*time.Minute))
	if got := o.Scope(at(now, 3*time.Minute)); got != ScrambleScopeService {
		t.Fatalf("the same service descrambling later must retract the verdict; got %q", got)
	}
}

// TestScrambleObserver_EvidenceExpires keeps yesterday's outage from explaining
// today's failure.
func TestScrambleObserver_EvidenceExpires(t *testing.T) {
	now := time.Now()
	o := NewScrambleObserver(10*time.Minute, 2)

	o.Observe("svc-a", true, at(now, 0))
	o.Observe("svc-b", true, at(now, time.Minute))
	if got := o.Scope(at(now, time.Minute)); got != ScrambleScopeReceiver {
		t.Fatalf("precondition: expected a receiver verdict, got %q", got)
	}
	if got := o.Scope(at(now, 30*time.Minute)); got != ScrambleScopeService {
		t.Fatalf("stale evidence must not carry a receiver verdict; got %q", got)
	}
}

// TestScrambleObserver_IgnoresBlankRefsAndNilReceiver keeps the call sites simple.
func TestScrambleObserver_IgnoresBlankRefsAndNilReceiver(t *testing.T) {
	now := time.Now()
	o := NewScrambleObserver(0, 0) // defaults
	o.Observe("   ", true, now)
	o.Observe("", true, now)
	if got := o.Scope(now); got != ScrambleScopeService {
		t.Fatalf("blank refs carry no evidence; got %q", got)
	}

	var nilObs *ScrambleObserver
	nilObs.Observe("svc", true, now) // must not panic
	if got := nilObs.Scope(now); got != ScrambleScopeUnknown {
		t.Fatalf("a nil observer reports unknown; got %q", got)
	}
}
