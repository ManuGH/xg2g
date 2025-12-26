// Legacy SeriesService compatibility wrapper
import * as api from '../../client-ts';

export const SeriesService = {
  getSeriesRules: api.getSeriesRules,
  createSeriesRule: api.createSeriesRule,
  deleteSeriesRule: api.deleteSeriesRule,
  runSeriesRule: api.runSeriesRule,
  runAllSeriesRules: api.runAllSeriesRules,
  previewConflicts: api.previewConflicts,
};
