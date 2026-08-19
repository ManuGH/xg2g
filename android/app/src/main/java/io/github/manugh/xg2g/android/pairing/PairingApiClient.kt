package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.contract.DeviceAuthDeviceType
import io.github.manugh.xg2g.android.contract.ECPublicKeyJWK
import io.github.manugh.xg2g.android.contract.ExchangePairingResponse
import io.github.manugh.xg2g.android.contract.PairingSecretRequest
import io.github.manugh.xg2g.android.contract.PairingStatusResponse
import io.github.manugh.xg2g.android.contract.StartPairingRequest
import io.github.manugh.xg2g.android.contract.StartPairingResponse
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * The pairing endpoints, spoken through the generated contract.
 *
 * This client used to carry its own hand-written result types and read them out
 * of org.json by hand. That is how it ended up decoding deviceGrantId and
 * deviceGrant — fields the server had already retired — while never sending the
 * device key the exchange requires. Both mistakes are unrepresentable now: the
 * request and response types come from backend/api/openapi.yaml, and the
 * generated decoders throw on a missing required field instead of substituting
 * an empty string.
 */
internal class PairingApiClient(
    private val baseUrl: String,
    private val okHttpClient: OkHttpClient = OkHttpClient()
) {
    suspend fun startPairing(
        deviceName: String = "Android TV",
        deviceType: DeviceAuthDeviceType = DeviceAuthDeviceType.ANDROID_TV
    ): StartPairingResponse = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            throw IllegalStateException("Server-URL ist nicht konfiguriert.")
        }
        val body = StartPairingRequest(
            deviceName = deviceName,
            deviceType = deviceType,
            requestedPolicyProfile = "native-app"
        )
        val json = post(apiUrl("pairing", "start"), body.toJson(), "Pairing start")
        StartPairingResponse.fromJson(json)
    }

    suspend fun getPairingStatus(
        pairingId: String,
        pairingSecret: String
    ): PairingStatusResponse = withContext(Dispatchers.IO) {
        // The status probe carries only the secret: the device key is bound at
        // exchange time, and the contract's PairingSecretRequest is not the
        // right shape for a poll.
        val body = JSONObject().put("pairingSecret", pairingSecret)
        val json = post(apiUrl("pairing", pairingId, "status"), body, "Pairing status")
        PairingStatusResponse.fromJson(json)
    }

    suspend fun exchangePairing(
        pairingId: String,
        pairingSecret: String,
        deviceJwk: ECPublicKeyJWK
    ): ExchangePairingResponse = withContext(Dispatchers.IO) {
        val body = PairingSecretRequest(pairingSecret = pairingSecret, deviceJwk = deviceJwk)
        val json = post(apiUrl("pairing", pairingId, "exchange"), body.toJson(), "Pairing exchange")
        ExchangePairingResponse.fromJson(json)
    }

    private fun post(url: HttpUrl, body: JSONObject, label: String): JSONObject {
        val request = Request.Builder()
            .url(url)
            .post(body.toString().toRequestBody(JSON_MEDIA_TYPE))
            .build()
            .withSameOriginHeaders(url)

        val response = okHttpClient.newCall(request).execute()
        val bodyStr = response.body.string()
        if (!response.isSuccessful) {
            throw IllegalStateException("$label failed (HTTP ${response.code}): $bodyStr")
        }
        return JSONObject(bodyStr)
    }

    private fun apiUrl(vararg segments: String): HttpUrl {
        val parsed = baseUrl.toHttpUrlOrNull()
            ?: throw IllegalArgumentException("Invalid server base URL: $baseUrl")
        return apiV3Url(parsed, *segments)
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
