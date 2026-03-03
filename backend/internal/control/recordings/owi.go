package recordings

import "context"

// OWIClient defines the interface for receiver interactions without importing openwebif package.
type OWIClient interface {
	GetLocations(ctx context.Context) ([]OWILocation, error)
	GetRecordings(ctx context.Context, path string) (OWIRecordingsList, error)
	GetTimers(ctx context.Context) ([]OWITimer, error)
	DeleteRecording(ctx context.Context, serviceRef string) error
}

type OWILocation struct {
	Name string
	Path string
}

type OWIRecordingsList struct {
	Result    bool
	Movies    []OWIMovie
	Bookmarks []OWILocation
}

type OWIMovie struct {
	ServiceRef          string
	Title               string
	Description         string
	ExtendedDescription string
	Length              string
	Filename            string
	Begin               int
	Filesize            interface{} // Can be string or number, parsed by service/client
}

type OWITimer struct {
	ServiceRef string
	Name       string
	Begin      int
	End        int
	State      int
	JustPlay   int // 0 or 1
	Disabled   int // 0 or 1
}
