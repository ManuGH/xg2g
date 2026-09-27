package io.github.manugh.xg2g.android.playback.player

import android.content.Context
import android.media.MediaFormat
import android.os.Build
import android.os.Handler
import android.util.Log
import androidx.annotation.OptIn
import androidx.media3.common.Format
import androidx.media3.common.MimeTypes
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.mediacodec.MediaCodecSelector
import androidx.media3.exoplayer.audio.AudioOutputProvider
import androidx.media3.exoplayer.audio.AudioSink
import androidx.media3.exoplayer.audio.AudioTrackAudioOutputProvider
import androidx.media3.exoplayer.audio.DefaultAudioTrackBufferSizeProvider
import androidx.media3.exoplayer.audio.DefaultAudioSink
import androidx.media3.exoplayer.video.MediaCodecVideoRenderer
import androidx.media3.exoplayer.video.VideoRendererEventListener

/**
 * MPEG-TS carries no frame rate, so media3 hands MediaCodec a Format with `frameRate = -1` and
 * consequently omits both KEY_FRAME_RATE and KEY_OPERATING_RATE. The MediaTek decoder on Fire TV
 * then provisions itself for a default rate and runs out of output buffers on a 50fps broadcast —
 * it either stalls silently or fails outright a few seconds in.
 *
 * This renderer only supplies the missing rate hint. It never touches sample data, so the decoded
 * frames stay bit-identical to the broadcast.
 */
@OptIn(markerClass = [UnstableApi::class])
internal class Xg2gVideoRenderer(builder: Builder) : MediaCodecVideoRenderer(builder) {

    override fun getMediaFormat(
        format: Format,
        codecMimeType: String,
        codecMaxValues: CodecMaxValues,
        codecOperatingRate: Float,
        deviceNeedsNoPostProcessWorkaround: Boolean,
        tunnelingAudioSessionId: Int
    ): MediaFormat {
        val mediaFormat = super.getMediaFormat(
            format,
            codecMimeType,
            codecMaxValues,
            codecOperatingRate,
            deviceNeedsNoPostProcessWorkaround,
            tunnelingAudioSessionId
        )
        if (format.frameRate == Format.NO_VALUE.toFloat() || format.frameRate <= 0f) {
            mediaFormat.setInteger(MediaFormat.KEY_FRAME_RATE, ASSUMED_FRAME_RATE.toInt())
            mediaFormat.setFloat(MediaFormat.KEY_OPERATING_RATE, ASSUMED_FRAME_RATE)
            Log.i(TAG, "[CODEC_CONFIG] format carried no frame rate -> assuming ${ASSUMED_FRAME_RATE.toInt()}fps for the decoder")
        }
        return mediaFormat
    }

    override fun getCodecOperatingRateV23(
        targetPlaybackSpeed: Float,
        format: Format,
        streamFormats: Array<out Format>
    ): Float {
        val rate = super.getCodecOperatingRateV23(targetPlaybackSpeed, format, streamFormats)
        return if (rate <= 0f) ASSUMED_FRAME_RATE * targetPlaybackSpeed else rate
    }

    private companion object {
        const val TAG = "PlayerHolder"

        /**
         * European broadcast is 25 or 50fps; assuming the higher rate only makes the decoder
         * reserve more headroom, whereas assuming too little is what starves it.
         */
        const val ASSUMED_FRAME_RATE = 50f
    }
}

/**
 * Builds [Xg2gVideoRenderer] in place of the stock video renderer, keeping every other renderer
 * (audio, text, metadata) exactly as [androidx.media3.exoplayer.DefaultRenderersFactory] provides.
 */
@OptIn(markerClass = [UnstableApi::class])
internal class Xg2gRenderersFactory(
    context: Context
) : androidx.media3.exoplayer.DefaultRenderersFactory(context) {

    override fun buildAudioSink(
        context: Context,
        enableFloatOutput: Boolean,
        enableAudioTrackPlaybackParams: Boolean
    ): AudioSink? {
        if (!Build.MODEL.startsWith("AFT", ignoreCase = true)) {
            return super.buildAudioSink(context, enableFloatOutput, enableAudioTrackPlaybackParams)
        }
        // Fire OS routes AC-3 through a separate HDMI offload clock. Keep audio on the
        // Media3 PCM clock so its timestamps can govern video presentation as well.
        val pcmBuffer = DefaultAudioTrackBufferSizeProvider.Builder()
            .setMinPcmBufferDurationUs(600_000)
            .setMaxPcmBufferDurationUs(1_200_000)
            .build()
        val deviceOutput = AudioTrackAudioOutputProvider.Builder(context)
            .setAudioTrackBufferSizeProvider(pcmBuffer)
            .build()
        val pcmOnlyOutput = object : AudioOutputProvider by deviceOutput {
            override fun getFormatSupport(
                formatConfig: AudioOutputProvider.FormatConfig
            ): AudioOutputProvider.FormatSupport =
                if (formatConfig.format.sampleMimeType == MimeTypes.AUDIO_RAW) {
                    deviceOutput.getFormatSupport(formatConfig)
                } else {
                    AudioOutputProvider.FormatSupport.UNSUPPORTED
                }
        }
        Log.i("PlayerHolder", "[AUDIO_OUTPUT] requiring decoded PCM on Fire TV")
        return DefaultAudioSink.Builder(context)
            .setAudioOutputProvider(pcmOnlyOutput)
            .setEnableFloatOutput(enableFloatOutput)
            .setEnableAudioOutputPlaybackParameters(enableAudioTrackPlaybackParams)
            .build()
    }

    override fun buildVideoRenderers(
        context: Context,
        extensionRendererMode: Int,
        mediaCodecSelector: MediaCodecSelector,
        enableDecoderFallback: Boolean,
        eventHandler: Handler,
        eventListener: VideoRendererEventListener,
        allowedVideoJoiningTimeMs: Long,
        out: ArrayList<androidx.media3.exoplayer.Renderer>
    ) {
        out.add(
            Xg2gVideoRenderer(
                MediaCodecVideoRenderer.Builder(context)
                    .setMediaCodecSelector(mediaCodecSelector)
                    .setAllowedJoiningTimeMs(allowedVideoJoiningTimeMs)
                    .setEnableDecoderFallback(enableDecoderFallback)
                    .setEventHandler(eventHandler)
                    .setEventListener(eventListener)
                    .setMaxDroppedFramesToNotify(MAX_DROPPED_VIDEO_FRAME_COUNT_TO_NOTIFY)
                    .setCodecAdapterFactory(codecAdapterFactory)
            )
        )
    }
}
