// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"fmt"
	"io/fs"
	"net/url"
	"testing"

	"github.com/ManuGH/xg2g/internal/problemcode"
)

func TestWrapPlaylistWriteError_ClassifiesPermission(t *testing.T) {
	err := WrapPlaylistWriteError(fmt.Errorf("write playlist: %w", fs.ErrPermission))
	if got := JobErrorCode(err); got != problemcode.CodeJobPlaylistWritePerm {
		t.Fatalf("JobErrorCode() = %q, want %q", got, problemcode.CodeJobPlaylistWritePerm)
	}
	if JobErrorRetryable(err) {
		t.Fatal("permission write error must not be retryable")
	}
}

func TestWrapBouquetsFetchError_RetryableForUnavailableUpstream(t *testing.T) {
	err := WrapBouquetsFetchError(&url.Error{Op: "Get", URL: "http://receiver", Err: fmt.Errorf("dial tcp: connection refused")})
	if got := JobErrorCode(err); got != problemcode.CodeJobBouquetsFetchFailed {
		t.Fatalf("JobErrorCode() = %q, want %q", got, problemcode.CodeJobBouquetsFetchFailed)
	}
	if !JobErrorRetryable(err) {
		t.Fatal("bouquet fetch upstream error should be retryable")
	}
}
