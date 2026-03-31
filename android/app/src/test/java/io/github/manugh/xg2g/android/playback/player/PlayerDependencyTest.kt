package io.github.manugh.xg2g.android.playback.player

import org.junit.Test

class PlayerDependencyTest {
    @Test
    fun `media3 hls module is available at runtime`() {
        Class.forName("androidx.media3.exoplayer.hls.HlsMediaSource\$Factory")
    }
}
