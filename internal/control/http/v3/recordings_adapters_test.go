package v3

import (
	"encoding/json"
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

func TestOWIAdapter_FilesizePropagated(t *testing.T) {
	// Arrange: Create a movie with a specific non-zero filesize
	// We use the JSON unmarshaler to ensure the type matches what comes from the OWI client.
	rawJSON := `{
		"eventname": "Test Movie",
		"filesize": 123456789,
		"recordingtime": 1737293597
	}`

	var m openwebif.Movie
	err := json.Unmarshal([]byte(rawJSON), &m)
	assert.NoError(t, err)

	movies := []openwebif.Movie{m}

	// Act: Run the pure mapping helper
	mapped := mapMovies(movies)

	// Assert: Verify Filesize is exactly preserved
	assert.Len(t, mapped, 1)
	assert.Equal(t, "Test Movie", mapped[0].Title)

	// Filesize unmarshals as StringOrNumberString("123456789")
	assert.Equal(t, openwebif.StringOrNumberString("123456789"), mapped[0].Filesize)
}

func TestOWIAdapter_FilesizeZeroPreserved(t *testing.T) {
	// Arrange: Filesize 0
	rawJSON := `{"filesize": 0}`
	var m openwebif.Movie
	json.Unmarshal([]byte(rawJSON), &m)

	mapped := mapMovies([]openwebif.Movie{m})

	// Assert: Still preserved
	assert.Equal(t, openwebif.StringOrNumberString("0"), mapped[0].Filesize)
}

func TestOWIAdapter_FilesizeStringPreserved(t *testing.T) {
	// Arrange: Filesize as string (some OWI versions)
	rawJSON := `{"filesize": "987654321"}`
	var m openwebif.Movie
	json.Unmarshal([]byte(rawJSON), &m)

	mapped := mapMovies([]openwebif.Movie{m})

	// Assert: Still preserved
	assert.Equal(t, openwebif.StringOrNumberString("987654321"), mapped[0].Filesize)
}
