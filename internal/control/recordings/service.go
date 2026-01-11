package recordings

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

type Service interface {
	List(ctx context.Context, in ListInput) (ListResult, error)
	GetPlaybackInfo(ctx context.Context, in PlaybackInfoInput) (PlaybackInfoResult, error)
	GetStatus(ctx context.Context, in StatusInput) (StatusResult, error)
	Stream(ctx context.Context, in StreamInput) (StreamResult, error)
	Delete(ctx context.Context, in DeleteInput) (DeleteResult, error)
}

type ResumeData struct {
	PosSeconds      int64
	DurationSeconds int64
	Finished        bool
	UpdatedAt       time.Time
}

type ResumeStore interface {
	GetResume(ctx context.Context, principalID, serviceRef string) (ResumeData, bool, error)
}

type service struct {
	cfg         *config.AppConfig
	vodManager  *vod.Manager
	resolver    Resolver
	owiClient   OWIClient
	resumeStore ResumeStore
}

func NewService(cfg *config.AppConfig, manager *vod.Manager, resolver Resolver, owi OWIClient, resume ResumeStore) Service {
	return &service{
		cfg:         cfg,
		vodManager:  manager,
		resolver:    resolver,
		owiClient:   owi,
		resumeStore: resume,
	}
}

func (s *service) List(ctx context.Context, in ListInput) (ListResult, error) {
	roots := make(map[string]string)

	for k, v := range s.cfg.RecordingRoots {
		id := s.normalizeRootID(k)
		s.addRootWithCollision(roots, id, v)
	}

	if locs, err := s.owiClient.GetLocations(ctx); err == nil {
		for _, loc := range locs {
			id := loc.Name
			if id == "" {
				id = path.Base(loc.Path)
			}
			s.addRootWithCollision(roots, s.normalizeRootID(id), loc.Path)
		}
	}

	const standardHddPath = "/media/hdd/movie"
	hddFound := false
	for _, p := range roots {
		if p == standardHddPath {
			hddFound = true
			break
		}
	}
	if !hddFound {
		roots["hdd"] = standardHddPath
	}

	rootList := make([]RecordingRoot, 0, len(roots))
	for id, p := range roots {
		rootList = append(rootList, RecordingRoot{ID: id, Name: path.Base(p)})
	}
	sort.Slice(rootList, func(i, j int) bool { return rootList[i].ID < rootList[j].ID })

	qRootID := in.RootID
	if qRootID == "" {
		if _, ok := roots["hdd"]; ok {
			qRootID = "hdd"
		} else if len(rootList) > 0 {
			qRootID = rootList[0].ID
		}
	}

	rootAbs, ok := roots[qRootID]
	if !ok {
		return ListResult{}, ErrInvalidArgument{Field: "root", Reason: "invalid root ID"}
	}

	cleanRel, blocked := SanitizeRecordingRelPath(in.Path)
	if blocked {
		return ListResult{}, ErrForbidden{}
	}

	qPath := cleanRel
	if qPath == "." {
		qPath = ""
	}

	cleanTarget := path.Join(rootAbs, cleanRel)
	list, err := s.owiClient.GetRecordings(ctx, cleanTarget)
	if err != nil {
		return ListResult{}, ErrUpstream{Op: "GetRecordings", Cause: err}
	}

	if !list.Result && len(list.Movies) == 0 {
		list.Movies = []OWIMovie{}
		list.Bookmarks = []OWILocation{}
	}

	recordingsList := make([]RecordingItem, 0, len(list.Movies))
	for _, m := range list.Movies {
		durationSeconds, _ := ParseRecordingDurationSeconds(m.Length)
		if durationSeconds <= 0 && s.vodManager != nil {
			if meta, ok := s.vodManager.GetMetadata(m.ServiceRef); ok && meta.Duration > 0 {
				durationSeconds = int64(meta.Duration)
			}
		}

		var durationPtr *int64
		if durationSeconds > 0 {
			durationPtr = &durationSeconds
		}

		recItem := RecordingItem{
			ServiceRef:       m.ServiceRef,
			RecordingID:      EncodeRecordingID(m.ServiceRef),
			Title:            m.Title,
			Description:      s.combineDescription(m.Description, m.ExtendedDescription),
			BeginUnixSeconds: int64(m.Begin),
			DurationSeconds:  durationPtr,
			Length:           m.Length,
			Filename:         m.Filename,
		}

		if in.PrincipalID != "" && s.resumeStore != nil {
			if res, ok, _ := s.resumeStore.GetResume(ctx, in.PrincipalID, m.ServiceRef); ok {
				recItem.Resume = &ResumeSummary{
					PosSeconds:      res.PosSeconds,
					DurationSeconds: res.DurationSeconds,
					Finished:        res.Finished,
					UpdatedAt:       &res.UpdatedAt,
				}
			}
		}

		recordingsList = append(recordingsList, recItem)
	}

	directoriesList := make([]DirectoryItem, 0, len(list.Bookmarks))
	rootTrimmed := strings.TrimSuffix(rootAbs, "/")
	for _, b := range list.Bookmarks {
		if b.Path == rootAbs || !strings.HasPrefix(b.Path, rootTrimmed+"/") {
			continue
		}
		rel := strings.TrimPrefix(b.Path, rootTrimmed+"/")
		if rel == "" || strings.HasPrefix(rel, "/") {
			continue
		}
		directoriesList = append(directoriesList, DirectoryItem{Name: b.Name, Path: rel})
	}

	breadcrumbsList := make([]Breadcrumb, 0)
	if qPath != "" {
		parts := strings.Split(qPath, "/")
		built := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			built = path.Join(built, p)
			breadcrumbsList = append(breadcrumbsList, Breadcrumb{Name: p, Path: built})
		}
	}

	return ListResult{
		Roots:       rootList,
		CurrentRoot: qRootID,
		CurrentPath: qPath,
		Recordings:  recordingsList,
		Directories: directoriesList,
		Breadcrumbs: breadcrumbsList,
	}, nil
}

func (s *service) GetPlaybackInfo(ctx context.Context, in PlaybackInfoInput) (PlaybackInfoResult, error) {
	serviceRef, ok := DecodeRecordingID(in.RecordingID)
	if !ok {
		return PlaybackInfoResult{}, ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}
	}
	return s.resolver.Resolve(ctx, serviceRef, PlaybackIntent(in.Intent), in.Profile)
}

func (s *service) GetStatus(ctx context.Context, in StatusInput) (StatusResult, error) {
	serviceRef, ok := DecodeRecordingID(in.RecordingID)
	if !ok {
		return StatusResult{}, ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}
	}

	cacheDir, err := RecordingCacheDir(s.cfg.HLS.Root, serviceRef)
	if err != nil {
		return StatusResult{}, ErrUpstream{Op: "CacheDir", Cause: err}
	}

	job, jobOk := s.vodManager.Get(ctx, cacheDir)
	meta, metaOk := s.vodManager.GetMetadata(serviceRef)

	state := s.mapState(job, jobOk, &meta, metaOk)
	var errStr *string
	if jobOk {
		if job.Reason != "" {
			errStr = &job.Reason
		}
	} else if metaOk && meta.Error != "" {
		errStr = &meta.Error
	}

	return StatusResult{
		State: state,
		Error: errStr,
	}, nil
}

func (s *service) Stream(ctx context.Context, in StreamInput) (StreamResult, error) {
	serviceRef, ok := DecodeRecordingID(in.RecordingID)
	if !ok {
		return StreamResult{}, ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}
	}

	// 1. Artifact State Machine Check
	meta, exists := s.vodManager.GetMetadata(serviceRef)

	if !exists || (meta.State != vod.ArtifactStateReady) {
		// Not ready: Trigger probe/build and return Not Ready
		s.vodManager.TriggerProbe(serviceRef, "")

		state := "UNKNOWN"
		if exists {
			state = string(meta.State)
		}

		return StreamResult{
			Ready:      false,
			State:      state,
			RetryAfter: 5,
		}, nil
	}

	// 2. Validate Path
	cachePath := meta.ArtifactPath
	if cachePath == "" {
		s.vodManager.MarkUnknown(serviceRef)
		s.vodManager.TriggerProbe(serviceRef, "")
		return StreamResult{
			Ready:      false,
			State:      "REPAIR",
			RetryAfter: 5,
		}, nil
	}

	// 3. Verify File Accessibility (Fail-Closed)
	// We check existence/openability here to ensure the handler receives a guaranteed-ready file.
	// We return an error if this fails, distinguishing "Broken" from "Preparing".
	f, err := os.Open(cachePath)
	if err != nil {
		s.vodManager.DemoteOnOpenFailure(serviceRef, err)
		return StreamResult{}, ErrUpstream{Op: "OpenArtifact", Cause: err}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return StreamResult{}, ErrUpstream{Op: "StatArtifact", Cause: err}
	}
	if info.IsDir() {
		return StreamResult{}, ErrUpstream{Op: "ValidateArtifact", Cause: fmt.Errorf("path is a directory")}
	}

	return StreamResult{
		Ready:       true,
		LocalPath:   cachePath,
		CachePolicy: CacheSegments,
	}, nil
}

func (s *service) Delete(ctx context.Context, in DeleteInput) (DeleteResult, error) {
	serviceRef, ok := DecodeRecordingID(in.RecordingID)
	if !ok {
		return DeleteResult{}, ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}
	}

	err := s.owiClient.DeleteRecording(ctx, serviceRef)
	if err != nil {
		return DeleteResult{}, ErrUpstream{Op: "DeleteRecording", Cause: err}
	}

	return DeleteResult{Deleted: true}, nil
}

// Helpers

func (s *service) normalizeRootID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, " ", "_"))
}

func (s *service) addRootWithCollision(roots map[string]string, id, path string) {
	baseID := id
	counter := 2
	for {
		if _, exists := roots[id]; !exists {
			break
		}
		id = fmt.Sprintf("%s-%d", baseID, counter)
		counter++
	}
	roots[id] = path
}

func (s *service) combineDescription(d, ed string) string {
	if ed == "" {
		return d
	}
	if d == "" {
		return ed
	}
	return d + "\n\n" + ed
}

func (s *service) mapState(job *vod.JobStatus, jobOk bool, meta *vod.Metadata, metaOk bool) string {
	// 1. Check Active Job
	if jobOk {
		switch job.State {
		case vod.JobStateBuilding, vod.JobStateFinalizing:
			return "RUNNING"
		case vod.JobStateSucceeded:
			return "READY"
		case vod.JobStateFailed:
			return "FAILED"
		}
	}

	// 2. Check Metadata Persistence
	if metaOk {
		switch meta.State {
		case vod.ArtifactStateReady:
			return "READY"
		case vod.ArtifactStateFailed:
			return "FAILED"
		}
		// If meta says Ready/Failed, we trust it.
		// For others (Preparing, Unknown), we treat as IDLE if no job is running.
	}

	return "IDLE"
}
