package sourceref

// NewSourceForTest creates a Source with arbitrary ID, rawURL, and canonicalURL for testing.
func NewSourceForTest(id ID, rawURL, canonicalURL string) Source {
	return Source{
		d: &sourceData{
			id:           id,
			rawURL:       func() string { return rawURL },
			canonicalURL: func() string { return canonicalURL },
		},
	}
}
