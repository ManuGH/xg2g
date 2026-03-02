import { putJsonOrThrow } from '../../lib/clientWrapper';

export interface ResumeState {
  posSeconds: number;
  durationSeconds?: number;
  finished?: boolean;
}

export interface SaveResumeRequest {
  position: number;
  total?: number;
  finished?: boolean;
}

export const saveResume = async (recordingId: string, data: SaveResumeRequest): Promise<void> => {
  const url = `/recordings/${recordingId}/resume`;
  await putJsonOrThrow(url, data);
};
