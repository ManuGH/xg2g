// Full hls.js build: the live pipeline serves multi-audio as alternate renditions
// (EXT-X-MEDIA + AUDIO group on a video-only variant). The light build ships
// `audioStreamController: undefined` / `audioTrackController: undefined`, so hls.js
// computes `altAudio: false`, never loads the audio rendition playlist, and playback
// is silent — with no AUDIO_TRACKS_UPDATED event, so the track selector stays empty too.
import Hls from 'hls.js';

export default Hls;
