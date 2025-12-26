// Compatibility layer: Re-export new TypeScript client with old API structure
// This allows existing .jsx files to work unchanged while we migrate incrementally

// Re-export all types and functions from new client
export * from '../client-ts';

// Legacy OpenAPI object
export { OpenAPI } from './core/OpenAPI';

// Legacy Services
export { DefaultService } from './services/DefaultService';
export { ServicesService } from './services/ServicesService';
export { AuthService } from './services/AuthService';
export { RecordingsService } from './services/RecordingsService';
export { DvrService } from './services/DvrService';
export { SeriesService } from './services/SeriesService';
export { TimersService } from './services/TimersService';
export { EpgService } from './services/EpgService';

// Export types with legacy names
export type {
  SystemHealth,
  AppConfig,
  ConfigUpdate,
  Service,
  Bouquet,
  Timer,
  TimerCreateRequest,
  TimerPatchRequest,
  SeriesRule,
  SeriesRuleWritable,
  RecordingResponse,
  StreamSession,
  LogEntry,
  DvrCapabilities,
  EpgStatus
} from '../client-ts';
