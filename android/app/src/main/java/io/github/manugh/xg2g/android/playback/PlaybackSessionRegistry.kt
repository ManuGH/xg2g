package io.github.manugh.xg2g.android.playback

import android.content.Context
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

internal object PlaybackSessionRegistry {
    private val stateStore = PlaybackStateStore()

    @Volatile
    private var runtime: PlaybackSession? = null

    val state: StateFlow<NativePlaybackState> = stateStore.state

    fun currentStateJson(): String = PlaybackJsonCodec.stateToJson(stateStore.current())

    fun getOrCreate(context: Context): PlaybackSession {
        return runtime ?: synchronized(this) {
            runtime ?: PlaybackRuntime(
                context = context.applicationContext,
                stateStore = stateStore
            ).also { runtime = it }
        }
    }

    fun release() {
        synchronized(this) {
            runtime?.close()
            runtime = null
        }
    }
}

internal class PlaybackStateStore(
    initialState: NativePlaybackState = NativePlaybackState()
) {
    private val _state = MutableStateFlow(initialState)
    val state: StateFlow<NativePlaybackState> = _state.asStateFlow()

    fun current(): NativePlaybackState = _state.value

    fun set(value: NativePlaybackState) {
        _state.value = value
    }
}
