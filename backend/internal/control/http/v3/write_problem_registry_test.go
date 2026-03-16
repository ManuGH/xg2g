// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/require"
)

func TestWriteProblem_CommonCodesUseRegistryHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	banned := map[string]struct{}{
		problemcode.CodeInvalidInput:           {},
		problemcode.CodeNotFound:               {},
		problemcode.CodeForbidden:              {},
		problemcode.CodeUnauthorized:           {},
		problemcode.CodeUnavailable:            {},
		problemcode.CodeInternalError:          {},
		problemcode.CodeSaveFailed:             {},
		problemcode.CodeReadFailed:             {},
		problemcode.CodeMethodNotAllowed:       {},
		problemcode.CodeNotImplemented:         {},
		problemcode.CodeUpstreamUnavailable:    {},
		problemcode.CodeReceiverUnreachable:    {},
		problemcode.CodeInvalidTime:            {},
		problemcode.CodeInvalidPlaylistPath:    {},
		problemcode.CodeConflict:               {},
		problemcode.CodeReceiverInconsistent:   {},
		problemcode.CodeInvalidID:              {},
		problemcode.CodeProviderError:          {},
		problemcode.CodeReceiverError:          {},
		problemcode.CodeTokenMissing:           {},
		problemcode.CodeSecurityUnavailable:    {},
		problemcode.CodeClaimMismatch:          {},
		problemcode.CodeAdmissionUnavailable:   {},
		problemcode.CodeStoreError:             {},
		problemcode.CodeSessionDotNotFound:     {},
		problemcode.CodeSessionExpired:         {},
		problemcode.CodeSessionUpdateError:     {},
		problemcode.CodeDiffFailed:             {},
		problemcode.CodeEngineError:            {},
		problemcode.CodeRecordingPreparing:     {},
		problemcode.CodePreparing:              {},
		problemcode.CodeRemoteProbeUnsupported: {},
		problemcode.CodeUpstreamError:          {},
		problemcode.CodeInvalidSessionID:       {},
		problemcode.CodeStopFailed:             {},
		problemcode.CodeInvalidCapabilities:    {},
		problemcode.CodePanic:                  {},
		problemcode.CodeAddFailed:              {},
		problemcode.CodeDeleteFailed:           {},
		problemcode.CodeUpdateFailed:           {},
		problemcode.CodeClientUnavailable:      {},
		problemcode.CodeUpstreamEmpty:          {},
	}

	fset := token.NewFileSet()
	for _, path := range files {
		if matched, _ := filepath.Match("*_test.go", filepath.Base(path)); matched {
			continue
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, parseErr)

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun, ok := call.Fun.(*ast.Ident)
			if !ok || fun.Name != "writeProblem" {
				return true
			}
			if len(call.Args) < 6 {
				return true
			}
			lit, ok := call.Args[5].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			code, unquoteErr := strconv.Unquote(lit.Value)
			require.NoError(t, unquoteErr)
			if _, bannedCode := banned[code]; bannedCode {
				t.Fatalf("%s uses raw common writeProblem code %q; use writeRegisteredProblem", path, code)
			}
			return true
		})
	}
}
