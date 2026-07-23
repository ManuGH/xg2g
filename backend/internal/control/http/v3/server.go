package v3

import (
	"context"
	"net/http"
	"sync"
	"time"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	ctrlauth "github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	v3deviceauth "github.com/ManuGH/xg2g/internal/control/http/v3/deviceauth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	v3tokens "github.com/ManuGH/xg2g/internal/control/http/v3/tokens"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/control/read"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/control/vod"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/ManuGH/xg2g/internal/receipts"
	recinfra "github.com/ManuGH/xg2g/internal/recordings"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/singleflight"
)

type Server struct {
	mu sync.RWMutex

	// Shared State & Configuration
	cfg       config.AppConfig
	snap      config.Snapshot
	status    jobs.Status
	startTime time.Time
	// Opaque auth session store for xg2g_session cookies.
	authSessionStore ctrlauth.SessionTokenStore
	authSessionTTL   time.Duration
	// Opaque household unlock store for xg2g_household_unlock cookies.
	householdUnlockStore household.UnlockStore
	householdUnlockTTL   time.Duration

	// Security
	JWTSecret []byte // HMAC-SHA256 key for playbackDecisionToken (SSOT)

	// Core Components
	v3Bus                  bus.Bus
	v3Store                SessionStateStore
	storeRegistry          store.StoreRegistry
	resumeStore            resume.Store
	v3Scan                 ChannelScanner
	decisionAudit          decisionaudit.EventSink
	capabilityRegistry     capreg.Store
	entitlementService     *entitlements.Service
	householdService       *household.Service
	receiptService         *receipts.Service
	owiFactory             receiverControlFactory // Factory for creating OpenWebIF clients (injectable for tests)
	recordingPathMapper    *recinfra.PathMapper
	channelManager         *channels.Manager
	seriesManager          *dvr.Manager
	seriesEngine           *dvr.SeriesEngine
	vodManager             *vod.Manager
	resolver               recservice.Resolver // Strict V4 Resolver (Domain)
	artifacts              artifacts.Resolver
	epgCache               *epg.TV // EPG Cache reference
	owiClient              *openwebif.Client
	owiEpoch               uint64
	receiverAbout          *openwebif.AboutInfo
	receiverAboutAt        time.Time
	receiverAboutEpoch     uint64
	receiverLocations      []openwebif.MovieLocation
	receiverLocationsAt    time.Time
	receiverLocationsEpoch uint64
	configManager          *config.Manager
	configMu               sync.Mutex // Serializes configuration updates
	epgCacheTime           time.Time
	epgCacheMTime          time.Time
	epgSfg                 singleflight.Group
	receiverSfg            singleflight.Group
	libraryService         *library.Service // Media library per ADR-ENG-002
	admission              *admission.Controller
	admissionState         AdmissionState
	hostPressureMonitor    *admissionmonitor.ResourceMonitor
	hostPressureTracker    *hardware.PressureTracker
	tokensService          *v3tokens.Service
	playbackSLO            *playbackSessionTracker
	exposureLimiter        *exposureRateLimiter
	intentService          *v3intents.Service
	pairingV3Service       *v3pairing.Service
	deviceAuthV3Service    *v3deviceauth.Service
	recordingsV3Service    *v3recordings.Service
	sessionsV3Service      *v3sessions.Service
	playbackInfoV3Service  *v3playbackinfo.Service
	deviceAuthStateStore   deviceauthstore.StateStore
	plannerShadowWorker    *playbackshadow.Worker
	plannerShadowObserver  playbackshadow.PlannerShadowObserver
	plannerReceiptStore    *v3intents.PlanningHandoffStore
	plannerReceiptEnabled  bool
	plannerReceiptRequired bool
	profileResolver        profiles.Resolver
	clientAV1Disabled      bool
	iosNativeHEVCHWMode    string

	// Lifecycle
	requestShutdown   func(context.Context) error
	preflightProvider PreflightProvider
	healthManager     *health.Manager
	logSource         interface{ GetRecentLogs() []log.LogEntry }
	scanSource        ScanSource
	dvrSource         RecordingStatusProvider
	servicesSource    ServiceStateReader
	timersSource      TimerReader
	epgSource         read.EpgSource
	recordingsService recservice.Service
	storageMonitor    *StorageMonitor
	monitorStarted    bool
	monitorMu         sync.Mutex
	runtimeCtx        context.Context
	runtimeCancel     context.CancelFunc

	// Middlewares (injectable for tests)
	AuthMiddlewareOverride func(http.Handler) http.Handler
}

// NewServer creates a new implemented v3 server.
func NewServer(cfg config.AppConfig, cfgMgr *config.Manager, rootCancel context.CancelFunc) *Server {
	// Initialize library service if enabled (Phase 0 per ADR-ENG-002)
	var librarySvc *library.Service
	if cfg.Library.Enabled && len(cfg.Library.Roots) > 0 {
		// Convert config roots to library roots
		var libraryRoots []library.RootConfig
		for _, r := range cfg.Library.Roots {
			libraryRoots = append(libraryRoots, library.RootConfig{
				ID:         r.ID,
				Path:       r.Path,
				Type:       r.Type,
				MaxDepth:   r.MaxDepth,
				IncludeExt: r.IncludeExt,
			})
		}

		store, err := library.NewStore(cfg.Library.DBPath)
		if err != nil {
			log.L().Error().Err(err).Msg("failed to initialize library store")
		} else {
			librarySvc = library.NewService(libraryRoots, store)
			log.L().Info().Int("roots", len(libraryRoots)).Msg("library service initialized")
		}
	}
	tokensSvc := v3tokens.NewService(cfg)
	profileResolver := profiles.LoadResolver()
	clientAV1Disabled := config.ParseBool("XG2G_CLIENT_AV1_DISABLED", false)
	iosNativeHEVCHWMode := autocodec.ResolveIOSNativeHEVCHWMode()

	s := &Server{
		cfg:                  cfg,
		configManager:        cfgMgr,
		startTime:            time.Now(),
		libraryService:       librarySvc,
		storageMonitor:       NewStorageMonitor(),
		admission:            admission.NewController(cfg),
		hostPressureMonitor:  admissionmonitor.NewResourceMonitor(cfg.Limits.MaxSessions, cfg.Limits.MaxTranscodes, 1.5),
		hostPressureTracker:  hardware.NewPressureTracker(),
		tokensService:        tokensSvc,
		playbackSLO:          newPlaybackSessionTracker(defaultPlaybackSLOSessionTTL),
		exposureLimiter:      newExposureRateLimiter(),
		authSessionStore:     ctrlauth.NewInMemorySessionTokenStore(),
		authSessionTTL:       defaultAuthSessionTTL,
		householdUnlockStore: household.NewInMemoryUnlockStore(),
		householdUnlockTTL:   cfg.Household.UnlockTTL,
		profileResolver:      profileResolver,
		clientAV1Disabled:    clientAV1Disabled,
		iosNativeHEVCHWMode:  iosNativeHEVCHWMode,
	}

	// Phase 2c: Shadow Observer
	var observer playbackshadow.PlannerShadowObserver = playbackshadow.NoopObserver{}
	obsConfig := playbackshadow.ObserverConfig{
		Enabled:       cfg.PlannerShadow.Enabled,
		QueueCapacity: cfg.PlannerShadow.QueueCapacity,
	}
	if obsConfig.Enabled {
		worker, err := playbackshadow.NewWorker(obsConfig, prometheus.DefaultRegisterer, *log.L())
		if err != nil {
			log.L().Error().Err(err).Msg("failed to initialize planner shadow worker, falling back to NoopObserver")
		} else {
			s.plannerShadowWorker = worker
			observer = worker
			// Started later with runtimeCtx
		}
	}
	s.plannerShadowObserver = observer
	receiptEnabled := cfg.PlannerReceipt.Enabled || cfg.PlannerReceipt.Required
	if cfg.PlannerReceipt.Required && !cfg.PlannerReceipt.Enabled {
		log.L().Warn().Msg("planner receipt required implies enabled")
	}
	if receiptEnabled {
		receiptTTL := cfg.PlannerReceipt.TTL
		if receiptTTL > 2*time.Minute {
			log.L().Warn().Dur("configuredTTL", receiptTTL).Msg("planner receipt TTL clamped to decision token maximum")
			receiptTTL = 2 * time.Minute
		}
		s.plannerReceiptStore = v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{
			Capacity: cfg.PlannerReceipt.Capacity,
			TTL:      receiptTTL,
		})
	}
	s.plannerReceiptEnabled = receiptEnabled
	s.plannerReceiptRequired = cfg.PlannerReceipt.Required

	// JWTSecret must be set explicitly via SetJWTSecret before serving requests (fail-closed).
	// owiFactory defaults to nil (uses newOpenWebIFClient in prod)

	s.intentService = v3intents.NewService(
		&serverIntentDeps{s: s},
		v3intents.WithProfileResolver(profileResolver),
		v3intents.WithClientAV1Disabled(clientAV1Disabled),
		v3intents.WithIOSNativeHEVCHWMode(iosNativeHEVCHWMode),
	)
	s.recordingsV3Service = v3recordings.NewService(
		&serverRecordingsDeps{s: s},
		v3recordings.WithPlannerShadowObserver(observer),
		v3recordings.WithProfileResolver(profileResolver),
		v3recordings.WithClientAV1Disabled(clientAV1Disabled),
		v3recordings.WithIOSNativeHEVCHWMode(iosNativeHEVCHWMode),
	)
	s.sessionsV3Service = v3sessions.NewService(&serverSessionDeps{s: s})
	s.epgSource = &epgAdapter{s}
	return s
}

// SetResolver sets the V4 resolver used by GetRecordingPlaybackInfo.
func (s *Server) SetResolver(r recservice.Resolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = r
}

// SetRecordingsService sets the recordings service (for tests).
func (s *Server) SetRecordingsService(svc recservice.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordingsService = svc
}

// SetArtifactsResolver overrides the recordings artifact resolver (tests).
func (s *Server) SetArtifactsResolver(res artifacts.Resolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts = res
}

// SetAdmission sets the resource monitor for admission control.
// SetAdmission sets the controller for admission control.
func (s *Server) SetAdmission(ctrl *admission.Controller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admission = ctrl
}

// SetJWTSecret configures the HMAC-SHA256 signing key for playback decision tokens.
// Must be called before serving requests. Returns a defensive copy.
func (s *Server) SetJWTSecret(secret []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(secret) == 0 {
		s.JWTSecret = nil
		if s.tokensService != nil {
			s.tokensService.SetJWTSecret(nil)
		}
		return
	}
	s.JWTSecret = append([]byte(nil), secret...)
	if s.tokensService != nil {
		s.tokensService.SetJWTSecret(secret)
	}
}

// SetShutdownHandler sets the function to call for graceful shutdown.
func (s *Server) SetShutdownHandler(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestShutdown = fn
}

// UpdateConfig updates the internal configuration snapshot.
func (s *Server) UpdateConfig(cfg config.AppConfig, snap config.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.snap = snap
	s.householdUnlockTTL = cfg.Household.UnlockTTL
	s.owiClient = nil // Invalidate cached OWI client; s.owiEpoch is reset on next newOpenWebIFClient call
	if state, ok := s.admissionState.(*storeAdmissionState); ok {
		state.SetTunerCount(len(cfg.Engine.TunerSlots))
	}
	if s.tokensService != nil {
		s.tokensService.UpdateConfig(cfg)
	}
}

// UpdateStatus updates the internal status snapshot.
func (s *Server) UpdateStatus(st jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = st
}

// SetPreflightCheck sets the source availability validator.
func (s *Server) SetPreflightCheck(fn PreflightProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preflightProvider = fn
}

// Dependencies groups runtime services injected into the v3 handler.
type Dependencies struct {
	Bus                bus.Bus
	Store              SessionStateStore
	StoreRegistry      store.StoreRegistry
	DeviceAuthStore    deviceauthstore.StateStore
	ResumeStore        resume.Store
	Scan               ChannelScanner
	DecisionAudit      decisionaudit.EventSink
	CapabilityRegistry capreg.Store
	Entitlements       *entitlements.Service
	Households         *household.Service
	Receipts           *receipts.Service
	PathMapper         *recinfra.PathMapper
	ChannelManager     *channels.Manager
	SeriesManager      *dvr.Manager
	SeriesEngine       *dvr.SeriesEngine
	VODManager         *vod.Manager
	EPGCache           *epg.TV
	HealthManager      *health.Manager
	LogSource          interface{ GetRecentLogs() []log.LogEntry }
	ScanSource         ScanSource
	DVRSource          RecordingStatusProvider
	ServicesSource     ServiceStateReader
	TimersSource       TimerReader
	RecordingsService  recservice.Service
	RequestShutdown    func(context.Context) error
	PreflightProvider  PreflightProvider
}

// SetDependencies injects shared services into the handler.
//
// It runs under s.mu and delegates the per-field wiring to focused helpers
// that must NOT re-acquire the lock. The ordering below is significant:
// applyServiceDependencies assigns s.recordingPathMapper, which
// applyVODDependencies reads when constructing the artifacts resolver, and it
// assigns s.v3Store, which the concluding admission-state initialization reads.
func (s *Server) SetDependencies(deps Dependencies) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.applyServiceDependencies(deps)
	s.applyDeviceAuthDependencies(deps)
	s.applyVODDependencies(deps)

	// Initialize Admission State Source (Store-backed)
	s.admissionState = newStoreAdmissionState(s.v3Store, len(s.cfg.Engine.TunerSlots))
}

// applyServiceDependencies copies the straightforward, side-effect-free service
// fields from deps onto the Server, clearing each one when its dependency is nil.
// Must be called with s.mu held. The device-auth and VOD wiring is handled by
// dedicated helpers because those involve additional construction and side
// effects beyond a plain field assignment.
func (s *Server) applyServiceDependencies(deps Dependencies) {
	if !isNil(deps.Bus) {
		s.v3Bus = deps.Bus
	} else {
		s.v3Bus = nil
	}

	if !isNil(deps.Store) {
		s.v3Store = deps.Store
	} else {
		s.v3Store = nil
	}

	if !isNil(deps.StoreRegistry) {
		s.storeRegistry = deps.StoreRegistry
	} else {
		s.storeRegistry = nil
	}

	if !isNil(deps.ResumeStore) {
		s.resumeStore = deps.ResumeStore
	} else {
		s.resumeStore = nil
	}

	if !isNil(deps.Scan) {
		s.v3Scan = deps.Scan
	} else {
		s.v3Scan = nil
	}

	if !isNil(deps.DecisionAudit) {
		s.decisionAudit = deps.DecisionAudit
	} else {
		s.decisionAudit = nil
	}

	if !isNil(deps.CapabilityRegistry) {
		s.capabilityRegistry = deps.CapabilityRegistry
	} else {
		s.capabilityRegistry = nil
	}

	if !isNil(deps.Entitlements) {
		s.entitlementService = deps.Entitlements
	} else {
		s.entitlementService = nil
	}

	if !isNil(deps.Households) {
		s.householdService = deps.Households
	} else {
		s.householdService = nil
	}

	if !isNil(deps.Receipts) {
		s.receiptService = deps.Receipts
	} else {
		s.receiptService = nil
	}

	if !isNil(deps.ScanSource) {
		s.scanSource = deps.ScanSource
	} else {
		s.scanSource = nil
	}

	if !isNil(deps.DVRSource) {
		s.dvrSource = deps.DVRSource
	} else {
		s.dvrSource = nil
	}

	if !isNil(deps.ServicesSource) {
		s.servicesSource = deps.ServicesSource
	} else {
		s.servicesSource = nil
	}

	if !isNil(deps.TimersSource) {
		s.timersSource = deps.TimersSource
	} else {
		s.timersSource = nil
	}

	if !isNil(deps.PathMapper) {
		s.recordingPathMapper = deps.PathMapper
	} else {
		s.recordingPathMapper = nil
	}

	if !isNil(deps.ChannelManager) {
		s.channelManager = deps.ChannelManager
	} else {
		s.channelManager = nil
	}

	if !isNil(deps.SeriesManager) {
		s.seriesManager = deps.SeriesManager
	} else {
		s.seriesManager = nil
	}

	if !isNil(deps.SeriesEngine) {
		s.seriesEngine = deps.SeriesEngine
	} else {
		s.seriesEngine = nil
	}

	if !isNil(deps.EPGCache) {
		s.epgCache = deps.EPGCache
	} else {
		s.epgCache = nil
	}

	if !isNil(deps.HealthManager) {
		s.healthManager = deps.HealthManager
	} else {
		s.healthManager = nil
	}

	if !isNil(deps.LogSource) {
		s.logSource = deps.LogSource
	} else {
		s.logSource = nil
	}

	if !isNil(deps.RequestShutdown) {
		s.requestShutdown = deps.RequestShutdown
	} else {
		s.requestShutdown = nil
	}

	if !isNil(deps.PreflightProvider) {
		s.preflightProvider = deps.PreflightProvider
	} else {
		s.preflightProvider = nil
	}

	if !isNil(deps.RecordingsService) {
		s.recordingsService = deps.RecordingsService
	} else {
		s.recordingsService = nil
	}
}

// applyDeviceAuthDependencies wires the device-auth state store and, when it is
// present, (re)constructs the pairing and device-auth v3 services bound to it.
// Unlike the plain service fields, a nil DeviceAuthStore leaves the existing
// wiring untouched (there is no else branch in the original behavior).
// Must be called with s.mu held.
func (s *Server) applyDeviceAuthDependencies(deps Dependencies) {
	if !isNil(deps.DeviceAuthStore) {
		s.deviceAuthStateStore = deps.DeviceAuthStore
		s.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
			StateStore:                 deps.DeviceAuthStore,
			PublishedEndpointsProvider: serverPublishedEndpointProvider{s: s},
		})
		s.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
			StateStore:                 deps.DeviceAuthStore,
			PublishedEndpointsProvider: serverPublishedEndpointProvider{s: s},
		})
	}
}

// applyVODDependencies wires the VOD manager and its dependent artifacts
// resolver. When a manager is supplied it starts the prober pool if a runtime
// context is already available and builds the artifacts resolver from the
// current config, manager, and (previously assigned) recording path mapper.
// When absent, both the manager and the artifacts resolver are cleared.
// Must be called with s.mu held, and after applyServiceDependencies so that
// s.recordingPathMapper reflects the latest deps.PathMapper.
func (s *Server) applyVODDependencies(deps Dependencies) {
	if !isNil(deps.VODManager) {
		s.vodManager = deps.VODManager
		if s.runtimeCtx != nil {
			s.vodManager.StartProberPool(s.runtimeCtx)
		}
		// PR3: Initialize Artifacts Resolver
		s.artifacts = artifacts.New(&s.cfg, deps.VODManager, s.recordingPathMapper)
	} else {
		s.vodManager = nil
		s.artifacts = nil
	}
}
