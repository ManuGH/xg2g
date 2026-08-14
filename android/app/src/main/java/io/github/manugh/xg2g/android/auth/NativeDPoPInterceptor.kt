package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Response

internal class NativeDPoPInterceptor(
    private val stateStore: PersistedDeviceAuthStateStore,
    private val dpopProvider: DPoPProvider,
    private val stateMachine: AuthStateMachine? = null,
    private val profileIdProvider: () -> String? = { null }
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val original = chain.request()
        val builder = original.newBuilder()

        val urlStr = original.url.toString()
        val method = original.method

        val currentStore = stateStore.load()
        val accessToken = currentStore?.accessToken?.trim()?.takeIf { it.isNotEmpty() }

        if (accessToken != null) {
            builder.header("Authorization", "DPoP $accessToken")
            val proof = dpopProvider.createProof(method, urlStr, accessToken)
            builder.header("DPoP", proof)
        }

        val profileId = profileIdProvider()?.trim()?.takeIf { it.isNotEmpty() }
        if (profileId != null) {
            builder.header("X-Household-Profile", profileId)
        }

        val response = chain.proceed(builder.build())

        if (response.code == 401 && stateMachine != null) {
            val bodyStr = runCatching { response.peekBody(1024).string() }.getOrDefault("")
            if (bodyStr.contains("invalid_grant", ignoreCase = true) || bodyStr.contains("revoked", ignoreCase = true)) {
                stateMachine.handleRefreshError("401 Unauthorized / Token Revoked", isNetworkError = false, isRevoked = true)
            }
        }

        return response
    }
}

internal fun createNativeAuthenticatedOkHttpClient(
    stateStore: PersistedDeviceAuthStateStore,
    dpopProvider: DPoPProvider,
    stateMachine: AuthStateMachine? = null,
    profileIdProvider: () -> String? = { null },
    baseClient: OkHttpClient = OkHttpClient()
): OkHttpClient {
    return baseClient.newBuilder()
        .addInterceptor(NativeDPoPInterceptor(stateStore, dpopProvider, stateMachine, profileIdProvider))
        .build()
}
