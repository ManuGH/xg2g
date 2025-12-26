// Legacy DefaultService compatibility wrapper
import * as api from '../../client-ts';

export const DefaultService = {
  getSystemHealth: api.getSystemHealth,
  getSystemConfig: api.getSystemConfig,
  putSystemConfig: api.putSystemConfig,
  postSystemRefresh: api.postSystemRefresh,
  getServices: api.getServices,
  getLogs: api.getLogs,
  getDvrStatus: api.getDvrStatus,
  getDvrCapabilities: api.getDvrCapabilities,
};
