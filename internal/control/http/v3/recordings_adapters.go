package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
)

// OWIAdapter bridges openwebif.Client to recordings.OWIClient
type OWIAdapter struct {
	client *openwebif.Client
}

func NewOWIAdapter(client *openwebif.Client) *OWIAdapter {
	return &OWIAdapter{client: client}
}

func (a *OWIAdapter) GetLocations(ctx context.Context) ([]recordings.OWILocation, error) {
	locs, err := a.client.GetLocations(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]recordings.OWILocation, len(locs))
	for i, l := range locs {
		res[i] = recordings.OWILocation{Name: l.Name, Path: l.Path}
	}
	return res, nil
}

func (a *OWIAdapter) GetRecordings(ctx context.Context, path string) (recordings.OWIRecordingsList, error) {
	list, err := a.client.GetRecordings(ctx, path)
	if err != nil {
		return recordings.OWIRecordingsList{}, err
	}
	res := recordings.OWIRecordingsList{
		Result:    list.Result,
		Movies:    mapMovies(list.Movies),
		Bookmarks: mapLocations(list.Bookmarks),
	}
	return res, nil
}

func mapMovies(movies []openwebif.Movie) []recordings.OWIMovie {
	res := make([]recordings.OWIMovie, len(movies))
	for i, m := range movies {
		res[i] = recordings.OWIMovie{
			ServiceRef:          m.ServiceRef,
			Title:               m.Title,
			Description:         m.Description,
			ExtendedDescription: m.ExtendedDescription,
			Length:              m.Length,
			Filename:            m.Filename,
			Begin:               int(m.Begin),
			Filesize:            m.Filesize,
		}
	}
	return res
}

func mapLocations(locs []openwebif.MovieLocation) []recordings.OWILocation {
	res := make([]recordings.OWILocation, len(locs))
	for i, l := range locs {
		res[i] = recordings.OWILocation{Name: l.Name, Path: l.Path}
	}
	return res
}

func (a *OWIAdapter) DeleteRecording(ctx context.Context, serviceRef string) error {
	return a.client.DeleteMovie(ctx, serviceRef)
}

func (a *OWIAdapter) GetTimers(ctx context.Context) ([]recordings.OWITimer, error) {
	timers, err := a.client.GetTimers(ctx)
	if err != nil {
		return nil, err
	}
	return mapTimers(timers), nil
}

func mapTimers(timers []openwebif.Timer) []recordings.OWITimer {
	res := make([]recordings.OWITimer, len(timers))
	for i, t := range timers {
		res[i] = recordings.OWITimer{
			ServiceRef: t.ServiceRef,
			Name:       t.Name,
			Begin:      int(t.Begin),
			End:        int(t.End),
			State:      t.State,
			JustPlay:   t.JustPlay,
			Disabled:   t.Disabled,
		}
	}
	return res
}

// ResumeAdapter bridges resume.Store to recordings.ResumeStore
type ResumeAdapter struct {
	store resume.Store
}

func NewResumeAdapter(store resume.Store) *ResumeAdapter {
	return &ResumeAdapter{store: store}
}

func (a *ResumeAdapter) GetResume(ctx context.Context, principalID, serviceRef string) (recordings.ResumeData, bool, error) {
	if a.store == nil {
		return recordings.ResumeData{}, false, nil
	}
	res, err := a.store.Get(ctx, principalID, serviceRef)
	if err != nil {
		return recordings.ResumeData{}, false, err
	}
	if res == nil {
		return recordings.ResumeData{}, false, nil
	}
	return recordings.ResumeData{
		PosSeconds:      res.PosSeconds,
		DurationSeconds: res.DurationSeconds,
		Finished:        res.Finished,
		UpdatedAt:       res.UpdatedAt,
	}, true, nil
}
