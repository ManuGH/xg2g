import { client } from '../../client/client.gen';

export interface ResumeState {
  pos_seconds: number;
  duration_seconds?: number;
  finished?: boolean;
}

export interface SaveResumeRequest {
  position: number;
  total?: number;
  finished?: boolean;
}

export const saveResume = async (recordingId: string, data: SaveResumeRequest): Promise<void> => {
  const url = `/recordings/${recordingId}/resume`;
  await client.put({
    url,
    body: data,
    headers: { 'Content-Type': 'application/json' }
  });
};
