import { StrictMode, useRef } from 'react';
import { act, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, SafariVideoElement, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';
import * as networkProbe from './utils/playbackNetworkProbe';
vi.mock('./lib/hlsRuntime', () => ({ default: Object.assign(vi.fn(), { isSupported: () => false }) }));
const { recordingInfo } = vi.hoisted(() => ({ recordingInfo: vi.fn() }));
vi.mock('../../client-ts', async (original) => ({ ...await original<typeof import('../../client-ts')>(), postRecordingPlaybackInfo: recordingInfo }));
let latest: ReturnType<typeof usePlaybackOrchestrator>;
let video: SafariVideoElement;
const picker = vi.fn();
const ticket = 'a'.repeat(64);
const profile = (audio = 'aac') => ({ video: { codec: 'h264' }, audio: { codec: audio } });
let fetchMock: ReturnType<typeof vi.fn<(url: string, init?: RequestInit) => Promise<any>>>;
function Harness({ props }: { props: V3PlayerProps }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
  latest = usePlaybackOrchestrator(props, { containerRef, videoRef, hlsRef, resumePrimaryActionRef });
  return <div ref={containerRef}><video ref={videoRef} data-testid="video" /></div>;
}
function route(wireless: boolean) {
  video.webkitCurrentPlaybackTargetIsWireless = wireless;
  fireEvent(video, new Event('webkitcurrentplaybacktargetiswirelesschanged'));
}
function recordingContract(audio = 'aac') {
  return { mode: 'hls', url: `/api/v3/recordings/rec-test/playlist.m3u8?ticket=${ticket}`, durationSeconds: 1000, anchorStartSec: 120, isSeekable: true,
    decision: { mode: 'direct_stream', selectedOutputKind: 'hls', selectedOutputUrl: `/api/v3/recordings/rec-test/playlist.m3u8?ticket=${ticket}`,
      selected: { videoCodec: 'h264', audioCodec: audio, container: 'fmp4' }, trace: { targetProfile: profile(audio) }, reasons: [], constraints: [], outputs: [] } };
}
beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
  Object.defineProperty(HTMLVideoElement.prototype, 'webkitShowPlaybackTargetPicker', { configurable: true, value: picker });
  vi.spyOn(networkProbe, 'measurePlaybackNetwork').mockResolvedValue(undefined);
  const ok = (body: unknown, status = 200) => Promise.resolve({ ok: status < 300, status, headers: new Headers(), json: async () => body, text: async () => JSON.stringify(body) });
  fetchMock = vi.fn((url: string) => {
    const path = String(url);
    if (path.endsWith('/playback-ticket')) return ok({ ticket });
    if (path.includes('/live/stream-info')) return ok({ mode: 'direct_stream', playbackDecisionToken: 'decision', decision: { mode: 'direct_stream', playbackDecisionToken: 'decision', trace: { targetProfile: profile() } } });
    if (path.includes('/intents')) return ok({ sessionId: 'sess-airplay' }, 202);
    if (path.endsWith('/sessions/sess-airplay')) return ok({ state: 'READY', sessionId: 'sess-airplay', playbackUrl: '/api/v3/sessions/sess-airplay/hls/index.m3u8', trace: { targetProfile: profile() }, heartbeatIntervalSeconds: 30, leaseExpiresAt: new Date(Date.now() + 120000).toISOString() });
    return ok({});
  });
  vi.stubGlobal('fetch', fetchMock);
  recordingInfo.mockResolvedValue({ data: recordingContract(), response: { status: 200 } });
});
afterEach(() => {
  delete (HTMLVideoElement.prototype as SafariVideoElement).webkitShowPlaybackTargetPicker;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
describe.each([false, true])('AirPlay orchestration (StrictMode=%s)', (strict) => {
  function mount(props: V3PlayerProps) {
    const ui = <Harness props={{ autoStart: false, ...props }} />;
    const rendered = render(strict ? <StrictMode>{ui}</StrictMode> : ui);
    video = rendered.getByTestId('video') as SafariVideoElement;
    return rendered;
  }
  it('opening or cancelling the picker leaves playback and position untouched', async () => {
    mount({ src: 'https://media.invalid/playlist.m3u8' });
    await act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 45;
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    const loads = vi.mocked(video.load).mock.calls.length;
    act(() => latest.actions.toggleAirPlay());
    await act(async () => { await Promise.resolve(); });
    expect(picker).toHaveBeenCalledOnce();
    expect(video.load).toHaveBeenCalledTimes(loads);
    expect(video.currentTime).toBe(45);
  });
  it('a failing picker does not restart playback', async () => {
    mount({ src: 'https://media.invalid/playlist.m3u8' });
    await act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    const loads = vi.mocked(video.load).mock.calls.length;
    picker.mockImplementationOnce(() => { throw new Error('picker unavailable'); });
    act(() => latest.actions.toggleAirPlay());
    await act(async () => { await Promise.resolve(); });
    expect(video.load).toHaveBeenCalledTimes(loads);
  });
  it('negotiates native H.264/AAC and retains the live session through route changes', async () => {
    mount({ channel: { id: 'synthetic-service', name: 'Synthetic channel' } });
    await act(() => latest.actions.startStream('synthetic-service'));
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    const preflight = fetchMock.mock.calls.find(([url]) => String(url).includes('/live/stream-info'))!;
    const capabilities = JSON.parse(String(preflight[1]?.body)).capabilities;
    expect(capabilities).toMatchObject({ videoCodecs: ['h264'], audioCodecs: ['aac'], preferredHlsEngine: 'native' });
    expect(video.src).toContain(`ticket=${ticket}`);
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 44;
    const calls = fetchMock.mock.calls.length;
    act(() => { route(true); route(false); route(true); });
    await act(async () => { await Promise.resolve(); });
    expect(fetchMock).toHaveBeenCalledTimes(calls);
    expect(video.currentTime).toBe(44);
  });
  it('retains a ticketed compatible recording during handoff', async () => {
    mount({ recordingId: 'rec-test' });
    await act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 12;
    act(() => route(true));
    await act(async () => { await Promise.resolve(); });
    expect(recordingInfo).toHaveBeenCalledOnce();
    expect(video.currentTime).toBe(12);
  });
  it('retains a compatible direct MP4 recording during handoff', async () => {
    const data = recordingContract();
    data.mode = 'direct_mp4';
    data.url = `/api/v3/recordings/rec-test/stream.mp4?ticket=${ticket}`;
    data.decision.selectedOutputUrl = data.url;
    data.decision.selectedOutputKind = 'file';
    data.decision.mode = 'direct_play';
    recordingInfo.mockResolvedValueOnce({ data, response: { status: 200 } });
    mount({ recordingId: 'rec-test' });
    act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 250;
    act(() => route(true));
    await act(async () => { await Promise.resolve(); });
    expect(recordingInfo).toHaveBeenCalledOnce();
    expect(video.currentTime).toBe(250);
  });
  it('carries the absolute recording position when AAC conversion is required', async () => {
    recordingInfo.mockResolvedValueOnce({ data: recordingContract('ac3'), response: { status: 200 } });
    mount({ recordingId: 'rec-test' });
    await act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 12;
    act(() => route(true));
    await waitFor(() => expect(recordingInfo).toHaveBeenCalledTimes(2));
    expect(recordingInfo.mock.calls[1]![0].query).toEqual({ start_ms: 132000 });
  });
  it('does not add the HLS anchor to the absolute direct MP4 playhead', async () => {
    const data = recordingContract('ac3');
    data.mode = 'direct_mp4';
    data.url = `/api/v3/recordings/rec-test/stream.mp4?ticket=${ticket}`;
    data.decision.selectedOutputUrl = data.url;
    data.decision.selectedOutputKind = 'file';
    data.decision.mode = 'direct_play';
    recordingInfo.mockResolvedValueOnce({ data, response: { status: 200 } });
    mount({ recordingId: 'rec-test' });
    act(() => latest.actions.startStream());
    await waitFor(() => expect(video.getAttribute('src')).toBeTruthy());
    video.currentTime = 250;
    act(() => route(true));
    await waitFor(() => expect(recordingInfo).toHaveBeenCalledTimes(2));
    expect(recordingInfo.mock.calls[1]![0].query).toEqual({ start_ms: 250000 });
  });
  it('does not attach a ticket response arriving after unmount', async () => {
    let resolveTicket!: (value: unknown) => void;
    const pending = new Promise(resolve => { resolveTicket = resolve; });
    const baseFetch = fetchMock.getMockImplementation()!;
    fetchMock.mockImplementation((url, init) => String(url).endsWith('/playback-ticket') ? pending as Promise<any> : baseFetch(url, init));
    const mounted = mount({ channel: { id: 'synthetic-service', name: 'Synthetic channel' } });
    act(() => latest.actions.startStream('synthetic-service'));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/playback-ticket'))).toBe(true));
    const ticketCall = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/playback-ticket'))!;
    mounted.unmount();
    expect(ticketCall[1]?.signal?.aborted).toBe(true);
    await act(async () => { resolveTicket({ ok: true, status: 201, json: async () => ({ ticket }) }); await pending; });
    expect(video.getAttribute('src')).toBeNull();
  });
  it('does not attach a native recording URL without a media ticket', async () => {
    const data = recordingContract();
    data.url = '/api/v3/recordings/rec-test/playlist.m3u8';
    data.decision.selectedOutputUrl = data.url;
    recordingInfo.mockResolvedValueOnce({ data, response: { status: 200 } });
    mount({ recordingId: 'rec-test' });
    act(() => latest.actions.startStream());
    await waitFor(() => expect(latest.playbackState.status).toBe('error'));
    expect(video.getAttribute('src')).toBeNull();
  });
  it('does not attach native live media when ticket issuance fails', async () => {
    const baseFetch = fetchMock.getMockImplementation()!;
    fetchMock.mockImplementation((url, init) => String(url).endsWith('/playback-ticket') ? Promise.resolve({ ok: false, status: 503, headers: new Headers() }) : baseFetch(url, init));
    mount({ channel: { id: 'synthetic-service', name: 'Synthetic channel' } });
    await act(() => latest.actions.startStream('synthetic-service'));
    await waitFor(() => expect(latest.playbackState.status).toBe('error'));
    expect(video.getAttribute('src')).toBeNull();
    expect(latest.playbackState.status).toBe('error');
  });
});
