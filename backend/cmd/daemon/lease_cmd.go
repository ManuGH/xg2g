// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/config"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	pipelinelease "github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// Operator surface for the one inconsistency that stops the daemon from
// starting: an ACTIVE lease intent whose backend lease is gone.
//
// The startup gate that refuses this is deliberate and stays. What this adds
// is a way to resolve the state it refuses on, so recovery is a documented
// operation rather than an improvised edit of intents.json.

func printLeaseUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: xg2g lease <command> [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  list       Show lease intents and whether the backend still holds each lease")
	_, _ = fmt.Fprintln(w, "  recover    Transition one stale ACTIVE intent to TERMINAL after proving the")
	_, _ = fmt.Fprintln(w, "             backend lease is gone")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Run with the daemon stopped: the intent store is single-writer.")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --data-dir string        Path to xg2g data dir (default: XG2G_DATA_DIR / XG2G_DATA)")
	_, _ = fmt.Fprintln(w, "  --intent-id string       recover: the exact intent to transition")
	_, _ = fmt.Fprintln(w, "  --expect-revision uint   recover: revision observed by the operator (CAS)")
	_, _ = fmt.Fprintln(w, "  --reason string          recover: why, recorded in the audit result")
	_, _ = fmt.Fprintln(w, "  --confirm                recover: required; states explicit operator intent")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "There is no --force. Every safety condition is a refusal, not a warning.")
}

func resolveLeaseDataDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := config.ParseString("XG2G_DATA_DIR", ""); v != "" {
		return v
	}
	return config.ParseString("XG2G_DATA", "")
}

// openLeaseSurfaces opens the same intent store and backend the daemon uses.
// Both are read from the real data directory: a recovery decided against a
// stub would prove nothing.
func openLeaseSurfaces(dataDir string) (pipelinelease.IntentStore, pipelinelease.ObservableLeaseBackend, func(), error) {
	storeBackend := config.ParseString("XG2G_STORE_BACKEND", "sqlite")
	storePath := config.ParseString("XG2G_STORE_PATH", "")
	if storePath == "" {
		storePath = filepath.Join(dataDir, "store")
	}

	intentStorePath, err := paths.ResolveDataFilePath(dataDir, "intents.json", true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve intent store path: %w", err)
	}
	intentStore, err := pipelinelease.NewFileIntentStore(intentStorePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open intent store %s: %w", intentStorePath, err)
	}

	sessions, err := sessionstore.OpenStateStore(storeBackend, filepath.Join(storePath, "sessions.sqlite"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open session store: %w", err)
	}
	backend := pipelinelease.SessionStoreObservableBackend{
		SessionStoreTunerLeaseController: pipelinelease.NewSessionStoreTunerLeaseController(sessions),
	}
	return intentStore, backend, func() {}, nil
}

func runLeaseCLI(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printLeaseUsage(os.Stdout)
		return 0
	}

	sub := args[0]
	fs := flag.NewFlagSet("lease "+sub, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		dataDirFlag string
		intentID    string
		expectRev   uint64
		reason      string
		confirm     bool
	)
	fs.StringVar(&dataDirFlag, "data-dir", "", "Path to xg2g data dir")
	fs.StringVar(&intentID, "intent-id", "", "Exact intent to transition")
	fs.Uint64Var(&expectRev, "expect-revision", 0, "Revision observed by the operator")
	fs.StringVar(&reason, "reason", "", "Why this recovery is being performed")
	fs.BoolVar(&confirm, "confirm", false, "Required: states explicit operator intent")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	dataDir := resolveLeaseDataDir(dataDirFlag)
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --data-dir (or XG2G_DATA_DIR / XG2G_DATA) is required.")
		return 2
	}

	ctx := context.Background()
	intentStore, backend, closeFn, err := openLeaseSurfaces(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer closeFn()

	switch sub {
	case "list":
		return runLeaseList(ctx, intentStore, backend)
	case "recover":
		return runLeaseRecover(ctx, intentStore, backend, pipelinelease.RecoverMissingIntentRequest{
			IntentID:         pipelinelease.ID(intentID),
			ExpectedRevision: expectRev,
			Confirmed:        confirm,
			Reason:           reason,
		})
	default:
		fmt.Fprintf(os.Stderr, "Unknown lease command: %s\n", sub)
		printLeaseUsage(os.Stderr)
		return 2
	}
}

func runLeaseList(ctx context.Context, store pipelinelease.IntentStore, backend pipelinelease.ObservableLeaseBackend) int {
	intents, err := store.ListIntents(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: list intents: %v\n", err)
		return 1
	}
	leases, leaseErr := backend.ListLeases(ctx)
	held := make(map[pipelinelease.ID]bool, len(leases))
	for i := range leases {
		held[leases[i].ID] = true
	}

	active := 0
	for _, in := range intents {
		if in.State != pipelinelease.IntentStateActive {
			continue
		}
		active++
		backendState := "backend-lease: present"
		switch {
		case leaseErr != nil:
			backendState = "backend-lease: UNVERIFIABLE (" + leaseErr.Error() + ")"
		case !held[in.LeaseID]:
			backendState = "backend-lease: MISSING -> recoverable"
		}
		fmt.Printf("%s  scope=%s  owner=%s  revision=%d  %s\n",
			in.IntentID, in.Scope, in.Owner, in.Revision, backendState)
	}

	fmt.Printf("\n%d intents total, %d ACTIVE\n", len(intents), active)
	if active > 0 && leaseErr == nil {
		fmt.Println("Recover a MISSING one with:")
		fmt.Println("  xg2g lease recover --intent-id <id> --expect-revision <n> --reason '<why>' --confirm")
	}
	return 0
}

func runLeaseRecover(
	ctx context.Context,
	store pipelinelease.IntentStore,
	backend pipelinelease.ObservableLeaseBackend,
	req pipelinelease.RecoverMissingIntentRequest,
) int {
	res, err := pipelinelease.RecoverMissingIntent(ctx, store, backend, req, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Refused: %v\n", err)
		switch {
		case errors.Is(err, pipelinelease.ErrRecoveryNotConfirmed):
			fmt.Fprintln(os.Stderr, "Pass --confirm to state explicit operator intent.")
		case errors.Is(err, pipelinelease.ErrRecoveryRevisionMismatch):
			fmt.Fprintln(os.Stderr, "Re-read the intent with 'xg2g lease list' and retry with the current revision.")
		case errors.Is(err, pipelinelease.ErrRecoveryBackendLeasePresent):
			fmt.Fprintln(os.Stderr, "The lease is still held. This is not a stale intent; do not recover it.")
		case errors.Is(err, pipelinelease.ErrRecoveryBackendUnverifiable):
			fmt.Fprintln(os.Stderr, "Backend state is unknown. Recovery stays refused until it can be read.")
		}
		return 1
	}

	out, mErr := json.MarshalIndent(res, "", "  ")
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "Error: render audit result: %v\n", mErr)
		return 1
	}
	fmt.Println(string(out))
	return 0
}
