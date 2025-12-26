// Legacy DvrService compatibility wrapper
import * as api from '../../client-ts';

export const DvrService = {
  getDvrStatus: api.getDvrStatus,
  getDvrCapabilities: api.getDvrCapabilities,
};
