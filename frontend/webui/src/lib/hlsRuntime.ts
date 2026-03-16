// xg2g WebUI only needs the core HLS playback path.
// The light build trims unused features like alternate audio, subtitles, CMCD, and DRM.
import Hls from 'hls.js/light';

export default Hls;
