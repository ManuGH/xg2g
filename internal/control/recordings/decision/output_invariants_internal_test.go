package decision

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateOutputInvariants_Negative ensures that the invariant validator
// catches semantic lies even if the engine produced them.
// This meets the "Stop-the-line" requirement for checking the guard itself.
func TestValidateOutputInvariants_Negative(t *testing.T) {
	trueVal := true

	tests := []struct {
		Name        string
		Dec         *Decision
		Input       Input
		ExpectedErr string
	}{
		{
			Name: "Invariant #9: DirectPlay with Wrong Kind",
			Dec: &Decision{
				Mode:               ModeDirectPlay,
				SelectedOutputKind: "hls", // LIE: Should be file
			},
			Input: Input{
				Capabilities: Capabilities{SupportsRange: &trueVal},
				Source:       Source{Container: "mp4"},
			},
			ExpectedErr: "invariant #9 violation: direct_play requires kind='file'",
		},
		{
			Name: "Invariant #9: DirectPlay with Missing Range Support",
			Dec: &Decision{
				Mode:               ModeDirectPlay,
				SelectedOutputKind: "file",
			},
			Input: Input{
				Capabilities: Capabilities{SupportsRange: nil}, // LIE: Cap missing
				Source:       Source{Container: "mp4"},
			},
			ExpectedErr: "invariant #9 violation: direct_play requires strict range support",
		},
		{
			Name: "Invariant #9: DirectPlay with Bad Container",
			Dec: &Decision{
				Mode:               ModeDirectPlay,
				SelectedOutputKind: "file",
			},
			Input: Input{
				Capabilities: Capabilities{SupportsRange: &trueVal},
				Source:       Source{Container: "avi"}, // LIE: Not MP4/MOV
			},
			ExpectedErr: "invariant #9 violation: direct_play requires mp4/mov container",
		},
		{
			Name: "Invariant #10: Transcode with Wrong Kind",
			Dec: &Decision{
				Mode:               ModeTranscode,
				SelectedOutputKind: "file", // LIE: Transcode is always HLS
			},
			Input:       Input{},
			ExpectedErr: "invariant #10 violation: transcode requires kind='hls'",
		},
		{
			Name: "Invariant #11: Deny with Output URL",
			Dec: &Decision{
				Mode:               ModeDeny,
				SelectedOutputURL:  "http://foo.bar", // LIE: Deny has no URL
				SelectedOutputKind: "",
			},
			Input:       Input{},
			ExpectedErr: "invariant #11 violation: deny mode must have empty output URL",
		},
		{
			Name: "Invariant #11: Deny with Non-Empty Kind",
			Dec: &Decision{
				Mode:               ModeDeny,
				SelectedOutputURL:  "",
				SelectedOutputKind: "file", // LIE: Deny has empty kind
			},
			Input:       Input{},
			ExpectedErr: "invariant #11 violation: deny mode must have empty output kind",
		},
		{
			Name: "Invariant #11: Deny with Outputs List",
			Dec: &Decision{
				Mode:               ModeDeny,
				SelectedOutputURL:  "",
				SelectedOutputKind: "",
				Outputs:            []Output{{Kind: "file", URL: "foo"}}, // LIE: Output list not empty
			},
			Input:       Input{},
			ExpectedErr: "invariant #11 violation: deny mode must have zero outputs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			err := validateOutputInvariants(tt.Dec, tt.Input)
			if tt.ExpectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.ExpectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
