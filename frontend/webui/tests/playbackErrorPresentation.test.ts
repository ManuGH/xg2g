import { describe, expect, it } from 'vitest';
import { classifyHlsFatalError, classifyMediaElementError } from '../src/features/player/playbackErrorPresentation';

const t = ((key: string, options?: { defaultValue?: string }) => options?.defaultValue ?? key) as any;

describe('playbackErrorPresentation', () => {
  it('classifies manifest range rejections as manifest errors', () => {
    const presentation = classifyHlsFatalError({
      type: 'networkError',
      details: 'manifestLoadError',
      response: { code: 416 },
    } as any, t, 'https://example.test/api/v3/recordings/rec-1/playlist.m3u8');

    expect(presentation.title).toBe('Playback manifest was rejected');
    expect(presentation.details).toContain('HTTP 416');
  });

  it('classifies segment fetch failures separately from manifest failures', () => {
    const presentation = classifyHlsFatalError({
      type: 'networkError',
      details: 'fragLoadError',
      response: { code: 503 },
    } as any, t, 'https://example.test/api/v3/sessions/sess-1/hls/index.m3u8');

    expect(presentation.title).toBe('Media segment request failed');
    expect(presentation.details).toContain('HTTP 503');
  });

  it('classifies media element code 4 as rejected source', () => {
    const presentation = classifyMediaElementError({
      code: 4,
      message: '',
      currentSrc: 'https://example.test/api/v3/recordings/rec-1/playlist.m3u8',
      readyState: 0,
      networkState: 3,
      hlsJsActive: false,
    }, t);

    expect(presentation.title).toBe('This device rejected the stream source');
    expect(presentation.details).toContain('playlist.m3u8');
  });
});
