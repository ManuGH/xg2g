package io.github.manugh.xg2g.android.auth

import android.content.Context
import android.content.res.Configuration
import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.DeviceAuthTransport
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore

internal class NativeAuthContainer private constructor(context: Context) {
    val appContext: Context = context.applicationContext
    val isTvDevice: Boolean = detectTvDevice(appContext)

    val stateStore: PersistedDeviceAuthStateStore by lazy(LazyThreadSafetyMode.NONE) {
        DeviceAuthStore(appContext)
    }

    val dpopProvider: DPoPProvider by lazy(LazyThreadSafetyMode.NONE) {
        AndroidKeystoreDPoPProvider()
    }

    val stateMachine: AuthStateMachine by lazy(LazyThreadSafetyMode.NONE) {
        AuthStateMachine(isTvDevice = isTvDevice)
    }

    val transport: DeviceAuthTransport by lazy(LazyThreadSafetyMode.NONE) {
        NativeDeviceAuthTransport(dpopProvider)
    }

    val repository: NativeDeviceAuthRepository by lazy(LazyThreadSafetyMode.NONE) {
        NativeDeviceAuthRepository(
            stateStore = stateStore,
            dpopProvider = dpopProvider,
            stateMachine = stateMachine,
            transport = transport
        )
    }

    companion object {
        @Volatile
        private var instance: NativeAuthContainer? = null

        fun getInstance(context: Context): NativeAuthContainer {
            return instance ?: synchronized(this) {
                instance ?: NativeAuthContainer(context.applicationContext).also { instance = it }
            }
        }
    }
}

private fun detectTvDevice(context: Context): Boolean {
    val uiModeManager = runCatching { context.getSystemService(Context.UI_MODE_SERVICE) as? android.app.UiModeManager }.getOrNull()
    return uiModeManager?.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION
}
