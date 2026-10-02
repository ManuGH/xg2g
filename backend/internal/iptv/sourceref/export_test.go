package sourceref

// NewSourceForTest creates a Source with arbitrary ID, rawURL, canonicalURL and rawRef for testing.
func NewSourceForTest(id ID, rawURL, canonicalURL, rawRef string) Source {
	return Source{
		d: &sourceData{
			id:           id,
			rawURL:       func() string { return rawURL },
			canonicalURL: func() string { return canonicalURL },
			rawRef:       func() string { return rawRef },
		},
	}
}
