// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func resolveIdentityDBPath(explicitPath string) string {
	if strings.TrimSpace(explicitPath) != "" {
		return explicitPath
	}
	storePath := strings.TrimSpace(config.ParseString("XG2G_STORE_PATH", ""))
	if storePath != "" {
		if strings.HasSuffix(storePath, ".sqlite") || strings.HasSuffix(storePath, ".db") {
			return filepath.Join(filepath.Dir(storePath), "identity.sqlite")
		}
		return filepath.Join(storePath, "identity.sqlite")
	}
	return "identity.sqlite"
}

func printAdminUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xg2g admin - Server Identity and Emergency Access CLI")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g admin <command> [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  bootstrap-token          Generate a persistent, single-use 15-minute setup token")
	_, _ = fmt.Fprintln(w, "  generate-recovery-codes  Generate 10 fresh emergency recovery codes for an admin")
	_, _ = fmt.Fprintln(w, "  status                   Inspect local identity and Public-Ready state")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --db string              Path to identity.sqlite (defaults to XG2G_STORE_PATH or identity.sqlite)")
	_, _ = fmt.Fprintln(w, "  --user string            Target username (for generate-recovery-codes)")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  xg2g admin bootstrap-token")
	_, _ = fmt.Fprintln(w, "  xg2g admin generate-recovery-codes --user admin")
	_, _ = fmt.Fprintln(w, "  xg2g admin status")
}

func runAdminCLI(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printAdminUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "bootstrap-token":
		return runAdminBootstrapToken(args[1:])
	case "generate-recovery-codes":
		return runAdminGenerateRecoveryCodes(args[1:])
	case "status":
		return runAdminStatus(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown admin subcommand: %s\n\n", args[0])
		printAdminUsage(os.Stderr)
		return 2
	}
}

func runAdminBootstrapToken(args []string) int {
	fs := flag.NewFlagSet("admin bootstrap-token", flag.ContinueOnError)
	dbPathFlag := fs.String("db", "", "Path to identity.sqlite")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dbPath := resolveIdentityDBPath(*dbPathFlag)
	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to open identity database at %s: %v\n", dbPath, err)
		return 1
	}
	defer reportAdminCloseError(s)

	svc := identity.NewService(identity.Config{}, s)
	token, err := svc.GenerateBootstrapToken(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to generate bootstrap token: %v\n", err)
		return 1
	}

	fmt.Println("🔑 Initial Admin Setup Token generated:")
	fmt.Printf("   Token:      %s\n", token)
	fmt.Println("   Valid for:  15 minutes (single-use)")
	fmt.Println("   Header:     X-Setup-Token: " + token)
	fmt.Println("")
	fmt.Println("👉 Open your browser to complete initial Passkey registration.")
	return 0
}

func runAdminGenerateRecoveryCodes(args []string) int {
	fs := flag.NewFlagSet("admin generate-recovery-codes", flag.ContinueOnError)
	dbPathFlag := fs.String("db", "", "Path to identity.sqlite")
	userFlag := fs.String("user", "admin", "Target username")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *userFlag == "" {
		_, _ = fmt.Fprintln(os.Stderr, "❌ Username is required (--user <username>)")
		return 2
	}

	dbPath := resolveIdentityDBPath(*dbPathFlag)
	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to open identity database at %s: %v\n", dbPath, err)
		return 1
	}
	defer reportAdminCloseError(s)

	svc := identity.NewService(identity.Config{}, s)
	codes, err := svc.GenerateEmergencyRecoveryCodes(context.Background(), *userFlag)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to generate recovery codes for user '%s': %v\n", *userFlag, err)
		return 1
	}

	fmt.Printf("🔐 Generated 10 fresh recovery codes for '%s':\n", *userFlag)
	for i, code := range codes {
		fmt.Printf("   %2d. %s\n", i+1, code)
	}
	fmt.Println("")
	fmt.Println("⚠️  Store these codes in a safe place. Previous recovery codes for this user are now replaced.")
	return 0
}

func runAdminStatus(args []string) int {
	fs := flag.NewFlagSet("admin status", flag.ContinueOnError)
	dbPathFlag := fs.String("db", "", "Path to identity.sqlite")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dbPath := resolveIdentityDBPath(*dbPathFlag)
	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to open identity database at %s: %v\n", dbPath, err)
		return 1
	}
	defer reportAdminCloseError(s)

	svc := identity.NewService(identity.Config{}, s)
	ctx := context.Background()

	state, err := svc.GetBootstrapStatus(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to read bootstrap state: %v\n", err)
		return 1
	}
	identityReady, err := svc.IsIdentityReady(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to evaluate identity readiness: %v\n", err)
		return 1
	}
	publicReady, err := svc.IsPublicReady(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Failed to evaluate public readiness: %v\n", err)
		return 1
	}

	users, _ := s.ListUsers(ctx)
	meta, _ := s.GetBootstrapMeta(ctx)

	fmt.Printf("📊 xg2g Identity & Public Readiness Status (%s):\n", dbPath)
	fmt.Printf("   Bootstrap State:       %s\n", state)
	fmt.Printf("   Identity Ready:        %v\n", identityReady)
	fmt.Printf("   Public Ready:          %v\n", publicReady)
	fmt.Printf("   User Accounts:         %d\n", len(users))
	if meta != nil && meta.RecoveryCodesAcknowledgedAt != nil {
		fmt.Printf("   Recovery Acknowledged: %s\n", meta.RecoveryCodesAcknowledgedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Println("   Recovery Acknowledged: NO")
	}
	return 0
}

func reportAdminCloseError(closer interface{ Close() error }) {
	if err := closer.Close(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to close identity database: %v\n", err)
	}
}
