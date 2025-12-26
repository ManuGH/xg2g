// Legacy RecordingsService compatibility wrapper
import * as api from '../../client-ts';

export const RecordingsService = {
  getRecordings: api.getRecordings,
  deleteRecording: api.deleteRecording,
  getRecordingStream: api.getRecordingStream,
  getRecordingHlsPlaylist: api.getRecordingHlsPlaylist,
  getRecordingHlsCustomSegment: api.getRecordingHlsCustomSegment,
};
