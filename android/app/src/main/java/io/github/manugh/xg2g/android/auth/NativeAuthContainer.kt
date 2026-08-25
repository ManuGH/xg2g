package io.github.manugh.xg2g.android.auth

import android.content.Context
import android.content.res.Configuration
import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.transport.DeviceAuthTransport
import io.github.manugh.xg2g.android.transport.auth.NativeDeviceAuthRepository
import io.github.manugh.xg2g.android.transport.auth.NativeDeviceAuthTransport

internal class NativeAuthContainer private constructor(
    context: Context,
    stateStoreProvider: ((Context) -> PersistedDeviceAuthStateStore)? = null,
    dpopProviderFactory: (() -> DPoPProvider)? = null
) {
    val appContext: Context = context.applicationContext
    val isTvDevice: Boolean = detectTvDevice(appContext)

    val stateStore: PersistedDeviceAuthStateStore by lazy {
        stateStoreProvider?.invoke(appContext) ?: DeviceAuthStore(appContext)
    }

    val dpopProvider: DPoPProvider by lazy {
        dpopProviderFactory?.invoke() ?: AndroidKeystoreDPoPProvider()
    }

    val stateMachine: AuthStateMachine by lazy {
        AuthStateMachine(isTvDevice = isTvDevice)
    }

    val transport: DeviceAuthTransport by lazy {
        NativeDeviceAuthTransport(dpopProvider)
    }

    val repository: NativeDeviceAuthRepository by lazy {
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

        fun getInstance(
            context: Context,
            stateStoreProvider: ((Context) -> PersistedDeviceAuthStateStore)? = null,
            dpopProviderFactory: (() -> DPoPProvider)? = null
        ): NativeAuthContainer {
            return instance ?: synchronized(this) {
                instance ?: NativeAuthContainer(context.applicationContext, stateStoreProvider, dpopProviderFactory).also { instance = it }
            }
        }

        fun resetForTests() {
            synchronized(this) {
                instance = null
            }
        }
    }
}

private fun detectTvDevice(context: Context): Boolean {
    val uiModeManager = runCatching { context.getSystemService(Context.UI_MODE_SERVICE) as? android.app.UiModeManager }.getOrNull()
    return uiModeManager?.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION
}
