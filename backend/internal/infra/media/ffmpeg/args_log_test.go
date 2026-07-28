package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgsWithSecretValuesRedacted(t *testing.T) {
	args := []string{
		"-headers", "Authorization: Basic cm9vdDpzZWNyZXQ=\r\n",
		"-i", "http://root:rK8pN4sV6mQ2xT9@10.10.55.64:17999/1:0:19:83:6:85:C00000:0:0:0",
		"-c:a", "aac",
		"-b:a", "320k",
		"-var_stream_map", "v:0,agroup:audio a:0,agroup:audio,default:yes,language:GER",
	}

	got := argsWithSecretValuesRedacted(args)

	require.Len(t, got, len(args))
	assert.Equal(t, redactedArgValuePlaceholder, got[1], "the -headers value carries an Authorization header")
	assert.NotContains(t, got[3], "rK8pN4sV6mQ2xT9", "stream URL userinfo must not be logged")
	assert.Equal(t, "http://10.10.55.64:17999/1:0:19:83:6:85:C00000:0:0:0", got[3])
	// Everything diagnostically useful survives untouched — that is the point of the log.
	assert.Equal(t, []string{"-c:a", "aac", "-b:a", "320k"}, got[4:8])
	assert.Equal(t, args[9], got[9])
	assert.NotSame(t, &args, &got, "must not mutate the caller's slice")
	assert.Equal(t, "Authorization: Basic cm9vdDpzZWNyZXQ=\r\n", args[1], "input slice stays intact")
}

func TestArgsWithSecretValuesRedacted_TrailingHeadersFlag(t *testing.T) {
	// A malformed vector must not panic on the missing value.
	assert.Equal(t, []string{"-i", "pipe:0", "-headers"},
		argsWithSecretValuesRedacted([]string{"-i", "pipe:0", "-headers"}))
}
