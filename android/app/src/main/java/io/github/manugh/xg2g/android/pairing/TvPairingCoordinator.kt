package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PublishedEndpoint as PersistedPublishedEndpoint
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.contract.DeviceAuthDeviceType
import io.github.manugh.xg2g.android.contract.ExchangePairingResponse
import io.github.manugh.xg2g.android.contract.PairingStatus
import io.github.manugh.xg2g.android.contract.Xg2gContractException
import kotlinx.coroutines.delay

internal class TvPairingCoordinator(
    private val pairingApiClient: PairingApiClient,
    private val stateStore: PersistedDeviceAuthStateStore,
    private val dpopProvider: DPoPProvider,
    private val stateMachine: AuthStateMachine,
    private val nowEpochMs: () -> Long = System::currentTimeMillis
) {
    suspend fun executePairingFlow(
        baseUrl: String,
        deviceName: String = "Android TV",
        pollIntervalMs: Long = 2000L,
        maxPollAttempts: Int = 150
    ): AuthState {
        val startResult = pairingApiClient.startPairing(
            deviceName = deviceName,
            deviceType = DeviceAuthDeviceType.ANDROID_TV
        )

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
            } catch (e: Xg2gContractException) {
                // A response that does not match the contract is not a
                // transient failure, and retrying it just hides it behind a
                // pairing timeout. Let it surface.
                throw e
            } catch (e: Exception) {
                // Temporary network error during poll -> keep polling
                continue
            }

            when (statusResult.status) {
                PairingStatus.APPROVED -> {
                    val exchange = pairingApiClient.exchangePairing(
                        pairingId = startResult.pairingId,
                        pairingSecret = startResult.pairingSecret,
                        deviceJwk = dpopProvider.publicJwk()
                    )
                    stateStore.save(persistedState(baseUrl, exchange))

                    return stateMachine.activateDeviceGrant(
                        deviceGrantId = exchange.deviceId,
                        accessToken = exchange.accessToken,
                        jktThumbprint = dpopProvider.getJWKThumbprint(),
                        expiresAtEpochMs = accessTokenExpiryEpochMs(exchange)
                    )
                }

                // The contract knows exactly these terminal states. The previous
                // hand-written check tested for "cancelled" and "rejected",
                // which the server never sends, and silently kept polling a
                // consumed or revoked pairing until the attempt budget ran out.
                PairingStatus.EXPIRED, PairingStatus.CONSUMED, PairingStatus.REVOKED -> {
                    return stateMachine.handleRefreshError(
                        errorMsg = "Pairing session ${statusResult.status.wireValue}",
                        isNetworkError = false,
                        isRevoked = statusResult.status == PairingStatus.REVOKED
                    )
                }

                PairingStatus.PENDING -> Unit
            }
        }

        return stateMachine.handleRefreshError(
            errorMsg = "Pairing timed out",
            isNetworkError = false,
            isRevoked = false
        )
    }

    /**
     * Maps the identity-shaped exchange result onto the persisted state.
     *
     * This is the one place where the old deviceauth vocabulary still shows
     * through, and it is a transition, not a translation. The server no longer
     * issues a device grant, a grant id or an access session: it issues a
     * DPoP-bound access token plus a rotating refresh token. The nearest true
     * counterparts are used and nothing is invented —
     *
     *   deviceGrantId    <- deviceId      the stable identity of this device
     *   deviceGrant      <- refreshToken  the long-lived rotating credential
     *   accessSessionId  <- null          no counterpart exists any more
     *
     * Exit condition: PersistedDeviceAuthState and DeviceAuthRepository still
     * speak deviceGrant/accessSession and still call the retired refresh
     * endpoint, so the stored refresh token cannot yet be redeemed. Renaming
     * the persisted model and moving the refresh path onto the identity
     * endpoints removes this adapter; until then enrolment succeeds and refresh
     * does not. Owner: the Android identity-wiring lane.
     */
    private fun persistedState(baseUrl: String, exchange: ExchangePairingResponse) = PersistedDeviceAuthState(
        serverUrl = baseUrl.trim().trimEnd('/'),
        deviceGrantId = exchange.deviceId,
        deviceGrant = exchange.refreshToken,
        accessSessionId = null,
        accessToken = exchange.accessToken,
        accessTokenExpiresAtEpochMs = accessTokenExpiryEpochMs(exchange),
        policyVersion = exchange.policyVersion,
        publishedEndpoints = exchange.endpoints.map { endpoint ->
            PersistedPublishedEndpoint(
                url = endpoint.url,
                kind = endpoint.kind.wireValue,
                priority = endpoint.priority,
                tlsMode = endpoint.tlsMode.wireValue,
                allowPairing = endpoint.allowPairing,
                allowStreaming = endpoint.allowStreaming,
                allowWeb = endpoint.allowWeb,
                allowNative = endpoint.allowNative,
                advertiseReason = endpoint.advertiseReason,
                source = endpoint.source.wireValue
            )
        }
    )

    // expiresIn is a lifetime in seconds counted from the response, not an
    // absolute instant, so the clock is read once here rather than assumed.
    private fun accessTokenExpiryEpochMs(exchange: ExchangePairingResponse): Long =
        nowEpochMs() + exchange.expiresIn * 1000L
}
