package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.transport.pairing.PairingApiClient
import kotlinx.coroutines.delay

internal class TvPairingCoordinator(
    private val pairingApiClient: PairingApiClient,
    private val stateStore: PersistedDeviceAuthStateStore,
    private val dpopProvider: DPoPProvider,
    private val stateMachine: AuthStateMachine
) {
    suspend fun executePairingFlow(
        baseUrl: String,
        deviceName: String = "Android TV",
        pollIntervalMs: Long = 2000L,
        maxPollAttempts: Int = 150
    ): AuthState {
        val startResult = pairingApiClient.startPairing(deviceName = deviceName, deviceType = "tv")

        stateMachine.startEnrollment(
            pairingCode = startResult.userCode,
            qrUrl = startResult.qrPayload
        )

        var attempts = 0
        while (attempts < maxPollAttempts) {
            delay(pollIntervalMs)
            attempts++

            val statusResult = try {
                pairingApiClient.getPairingStatus(startResult.pairingId, startResult.pairingSecret)
            } catch (e: Exception) {
                // Temporary network error during poll -> keep polling
                continue
            }

            when (statusResult.status.lowercase()) {
                "approved" -> {
                    val exchange = pairingApiClient.exchangePairing(startResult.pairingId, startResult.pairingSecret)
                    val jkt = dpopProvider.getJWKThumbprint()

                    val persistedState = PersistedDeviceAuthState(
                        serverUrl = baseUrl.trim().trimEnd('/'),
                        deviceGrantId = exchange.deviceGrantId,
                        deviceGrant = exchange.deviceGrant,
                        accessSessionId = exchange.accessSessionId,
                        accessToken = exchange.accessToken,
                        accessTokenExpiresAtEpochMs = exchange.accessTokenExpiresAtEpochMs,
                        policyVersion = exchange.policyVersion,
                        publishedEndpoints = exchange.endpoints
                    )
                    stateStore.save(persistedState)

                    return stateMachine.activateDeviceGrant(
                        deviceGrantId = exchange.deviceGrantId,
                        accessToken = exchange.accessToken,
                        jktThumbprint = jkt,
                        expiresAtEpochMs = exchange.accessTokenExpiresAtEpochMs
                    )
                }
                "expired", "cancelled", "rejected" -> {
                    return stateMachine.handleRefreshError(
                        errorMsg = "Pairing session ${statusResult.status}",
                        isNetworkError = false,
                        isRevoked = false
                    )
                }
                else -> {
                    // "pending" -> keep polling
                }
            }
        }

        return stateMachine.handleRefreshError(
            errorMsg = "Pairing timed out",
            isNetworkError = false,
            isRevoked = false
        )
    }
}
