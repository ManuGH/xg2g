package recordings

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/recordings/model"
)

// PlaybackResolution represents the truthful resolution of how to play a recording.
type PlaybackResolution struct {
	// Strategy: "hls" or "direct"
	Strategy string

	// CanSeek: Whether the stream supports seeking (e.g. valid duration/index)
	CanSeek bool

	// DurationSec: Authoritative duration in seconds (nil if unknown)
	DurationSec *int64

	// DurationSource: "store", "probe", "cache" (nil if unknown)
	DurationSource *DurationSource

	// Codec Truth (nil if unknown)
	Container  *string
	VideoCodec *string
	AudioCodec *string

	// Reason: Decision engine reason code
	Reason string
}

const (
	StrategyHLS    = "hls"
	StrategyDirect = "direct"
)

type Service interface {
	ResolvePlayback(ctx context.Context, recordingID, profile string) (PlaybackResolution, error)
	List(ctx context.Context, in ListInput) (ListResult, error)
	GetPlaybackInfo(ctx context.Context, in PlaybackInfoInput) (PlaybackInfoResult, error)
	GetStatus(ctx context.Context, in StatusInput) (StatusResult, error)
	GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error)
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
	truth       playback.MediaTruthProvider
}

func NewService(cfg *config.AppConfig, manager *vod.Manager, resolver Resolver, owi OWIClient, resume ResumeStore, truth playback.MediaTruthProvider) (Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("NewService: cfg is nil")
	}
	if manager == nil {
		return nil, fmt.Errorf("NewService: manager is nil")
	}
	if resolver == nil {
		return nil, fmt.Errorf("NewService: resolver is nil")
	}
	if truth == nil {
		return nil, fmt.Errorf("NewService: truth is nil")
	}

	return &service{
		cfg:         cfg,
		vodManager:  manager,
		resolver:    resolver,
		owiClient:   owi,
		resumeStore: resume,
		truth:       truth,
	}, nil
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
	// Determine if standard HDD path is already covered
	hddFound := false
	for _, p := range roots {
		if p == standardHddPath {
			hddFound = true
			break
		}
	}

	// Only add default "hdd" -> "/media/hdd/movie" if:
	// 1. That path isn't already mapped by another key (hddFound)
	// 2. The key "hdd" isn't already used by config (e.g. mapping to "/media/hdd")
	if _, ok := roots["hdd"]; !ok && !hddFound {
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

	// P3-3: Fetch Timers for Truth Derivation
	// We ignore errors to allow partial availability (fail open for creating scheduled items? No, list is driven by files).
	// If GetTimers fails, we just don't match any active timers (so no RECORDING status if derived solely from timer).
	// But ClassifyFilePresence handles file truth.
	timers, _ := s.owiClient.GetTimers(ctx)

	if !list.Result && len(list.Movies) == 0 {
		list.Movies = []OWIMovie{}
		list.Bookmarks = []OWILocation{}
	}

	recordingsList := make([]RecordingItem, 0, len(list.Movies))
	for _, m := range list.Movies {
		var meta vod.Metadata
		var metaOk bool
		if s.vodManager != nil {
			meta, metaOk = s.vodManager.GetMetadata(m.ServiceRef)
		}

		// A4: Building State Gate
		isBuilding := metaOk && meta.State == vod.ArtifactStatePreparing

		// A1: Store Wins
		durationSeconds, err := ParseRecordingDurationSeconds(m.Length)
		if err != nil && m.Length != "" { //nolint:staticcheck
			// Intentionally swallowed
		}

		// A2/A3: Probe Fallback
		if durationSeconds <= 0 && metaOk && meta.Duration > 0 {
			durationSeconds = meta.Duration
		}

		if isBuilding {
			durationSeconds = 0
		}

		var durationPtr *int64
		if durationSeconds > 0 {
			durationPtr = &durationSeconds
		}

		// P3-3: Truth Logic Integration
		matchedTimer, _ := s.matchTimer(m, timers)
		fileClass := s.classifyFilePresence(m)
		// Derive status. We ignore the reason string for DTO consumption (internal audit only).
		status, _ := model.DeriveRecordingStatus(time.Now(), fileClass, matchedTimer)

		recItem := RecordingItem{
			ServiceRef:       m.ServiceRef,
			RecordingID:      EncodeRecordingID(m.ServiceRef),
			Title:            m.Title,
			Description:      s.combineDescription(m.Description, m.ExtendedDescription),
			BeginUnixSeconds: int64(m.Begin),
			DurationSeconds:  durationPtr,
			Length:           m.Length,
			Filename:         m.Filename,
			Status:           status,
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

func (s *service) ResolvePlayback(ctx context.Context, recordingID, profile string) (PlaybackResolution, error) {
	// 1. Get raw domain decision
	res, err := s.GetPlaybackInfo(ctx, PlaybackInfoInput{
		RecordingID: recordingID,
		Intent:      "stream", // We are resolving for streaming
		Profile:     PlaybackProfile(profile),
	})
	if err != nil {
		return PlaybackResolution{}, err
	}

	// 2. Map to Resolution
	strategy := StrategyDirect
	if res.Decision.Artifact == playback.ArtifactHLS {
		strategy = StrategyHLS
	}

	// CanSeek?
	// Logic:
	// - Direct+MP4+FastPath = Seekable
	// - HLS+Ready = Seekable
	// Note: If HLS and Preparing, simple "GetPlaybackInfo" returns OK, but
	// ResolvePlayback wrapper should ideally arguably fail-closed or return restricted status.
	// But our contract says "ResolvePlayback" returns success if strategy is decided.
	// We handle transient states via "GetPlaybackInfo" errors (ErrPreparing) which GetPlaybackInfo ALREADY returns!
	// So if we are here, we are NOT preparing (unless resolver didn't check job, but it does).

	canSeek := false
	if strategy == StrategyHLS {
		canSeek = true // VOD HLS is seekable if playlist exists (implied by success here)
	} else if res.MediaInfo.IsMP4FastPathEligible {
		canSeek = true
	}

	return PlaybackResolution{
		Strategy:       strategy,
		CanSeek:        canSeek,
		DurationSec:    res.DurationSeconds, // Pass-through pointer
		DurationSource: res.DurationSource,  // Pass-through pointer
		Container:      res.Container,
		VideoCodec:     res.VideoCodec,
		AudioCodec:     res.AudioCodec,
		Reason:         res.Reason,
	}, nil
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

func (s *service) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	serviceRef, ok := DecodeRecordingID(recordingID)
	if !ok {
		return playback.MediaTruth{}, ErrInvalidArgument{Field: "recordingID", Reason: "invalid format"}
	}
	return s.truth.GetMediaTruth(ctx, serviceRef)
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
	// #nosec G304 - cachePath is trusted from internal metadata
	f, err := os.Open(cachePath)
	if err != nil {
		s.vodManager.DemoteOnOpenFailure(serviceRef, err)
		return StreamResult{}, ErrUpstream{Op: "OpenArtifact", Cause: err}
	}
	defer func() { _ = f.Close() }()

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

// matchTimer finds the best matching timer for a recording file using strict priority.
// Priority 1: Exact EventID Match (if available - OWI currently doesn't expose EventID on files easily, so we rely on ServiceRef+Time)
// Priority 2: ServiceRef + Time Window Match (Start time within tolerance)
func (s *service) matchTimer(m OWIMovie, timers []OWITimer) (*model.TimerTruth, string) {
	// Tolerance for start time matching (clock drift, pre-record padding)
	// 5 minutes is generous but safe for "this is the same show".
	const timeMatchTolerance = 5 * time.Minute

	var bestMatch *OWITimer
	matchType := "none"

	// Iterate all timers to find best match
	for i := range timers {
		t := &timers[i]

		// 1. ServiceRef Check (Fast Fail)
		// OWI ServiceRefs might have minor variation (case/channums), but usually strict equality works for same backend.
		if t.ServiceRef != m.ServiceRef {
			continue
		}

		// 2. Time Window Check
		// Check if timer start is "close enough" to recording start (m.Begin)
		diff := int64(t.Begin) - int64(m.Begin)
		if diff < 0 {
			diff = -diff
		}

		if diff <= int64(timeMatchTolerance.Seconds()) {
			// Found a match!
			// In future, if we have EventID, we'd check that first.
			// For now, ServiceRef + Time is our best strong signal.
			bestMatch = t
			matchType = "serviceref_time"
			break // First valid match is usually sufficient given chronologic sort
		}
	}

	if bestMatch != nil {
		return &model.TimerTruth{
			Begin:      time.Unix(int64(bestMatch.Begin), 0),
			End:        time.Unix(int64(bestMatch.End), 0),
			ServiceRef: bestMatch.ServiceRef,
			Name:       bestMatch.Name,
			JustPlay:   bestMatch.JustPlay == 1,
			Disabled:   bestMatch.Disabled == 1,
			Running:    IsTimerRunning(bestMatch.State),
		}, matchType
	}

	return nil, "no_match"
}

// classifyFilePresence determines the domain-level presence class of a file
func (s *service) classifyFilePresence(m OWIMovie) model.FilePresenceClass {
	// Parse size
	// Note: OWI returns "filesize" which can be string ("123") or number.
	// Our generated OWIMovie struct handles this via custom type.
	// But we need to convert it to int64 bytes here.
	// For now, let's rely on the fact that List parsing populated `m.Filesize`.

	// Quick parse helper (could be moved, but keeping local for now)
	parseSize := func(val interface{}) int64 {
		s := fmt.Sprintf("%v", val)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
		return 0
	}

	sizeBytes := parseSize(m.Filesize)

	if sizeBytes <= 0 {
		// If entry exists but size is <= 0, it's effectively missing content.
		// However, "FilePresenceMissing" implies the file entry itself isn't there.
		// If OWI returns it, the file entry exists.
		// So 0 bytes is "Small".
		return model.FilePresenceSmall
	}

	if sizeBytes < model.MinBytesThreshold {
		return model.FilePresenceSmall
	}

	// Check partial extensions
	if strings.HasSuffix(m.Filename, ".part") || strings.HasSuffix(m.Filename, ".tmp") {
		return model.FilePresencePartial
	}

	return model.FilePresenceOK
}

// IsTimerRunning maps OWI timer state int to boolean.
// 0=Waiting, 2=Running, 3=Finished? (Need to verify exact OWI constants)
// OWI common states: 0=Waiting, 1=Prepared?, 2=Running, 3=Ended.
// For Truth, we trust 2.
func IsTimerRunning(state int) bool {
	return state == 2
}
