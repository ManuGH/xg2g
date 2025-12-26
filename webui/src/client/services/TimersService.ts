// Legacy TimersService compatibility wrapper
import * as api from '../../client-ts';

export const TimersService = {
  getTimers: api.getTimers,
  getTimer: api.getTimer,
  addTimer: api.addTimer,
  updateTimer: api.updateTimer,
  deleteTimer: api.deleteTimer,
  previewConflicts: api.previewConflicts,
};
