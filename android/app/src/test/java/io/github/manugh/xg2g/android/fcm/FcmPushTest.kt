package io.github.manugh.xg2g.android.fcm

import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateKind
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import kotlinx.coroutines.runBlocking
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class FcmPushTest {

    @Test
    fun `FcmTokenManager registers FCM token with channel fcm`() = runBlocking {
        var recordedBodyStr = ""
        val interceptor = Interceptor { chain ->
            val request = chain.request()
            recordedBodyStr = request.body?.let { body ->
                val buffer = okio.Buffer()
                body.writeTo(buffer)
                buffer.readUtf8()
            }.orEmpty()

            Response.Builder()
                .request(request)
                .protocol(Protocol.HTTP_1_1)
                .code(201)
                .message("Created")
                .body("{}".toResponseBody("application/json".toMediaType()))
                .build()
        }

        val client = OkHttpClient.Builder().addInterceptor(interceptor).build()
        val manager = FcmTokenManager(client)

        val success = manager.registerFcmToken(
            baseUrl = "https://xg2g.local",
            fcmToken = "fcm_token_device_abc123",
            bearerToken = "at_valid_bearer"
        )

        assertTrue(success)
        val json = JSONObject(recordedBodyStr)
        assertEquals("fcm_token_device_abc123", json.getString("endpoint"))
        assertEquals("fcm", json.getString("channel"))
    }

    @Test
    fun `FcmNotificationDispatcher device_revoked payload triggers AuthStateMachine revocation`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_100", "jkt_100", 120_000L)

        val dispatcher = FcmNotificationDispatcher(machine)
        val state = dispatcher.dispatchIncomingPush(
            FcmPushPayload(
                notificationId = "notif_1",
                title = "Device Revoked",
                body = "Admin revoked your device from WebUI",
                type = "device_revoked"
            )
        )

        assertEquals(AuthStateKind.Revoked, state.kind)
        assertEquals("Admin revoked your device from WebUI", (state as AuthState.Revoked).reason)
    }

    @Test
    fun `FcmNotificationDispatcher session_expired payload triggers ReauthRequired`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_100", "jkt_100", 120_000L)

        val dispatcher = FcmNotificationDispatcher(machine)
        val state = dispatcher.dispatchIncomingPush(
            FcmPushPayload(
                notificationId = "notif_2",
                title = "Session Expired",
                body = "Your session was invalidated",
                type = "session_expired"
            )
        )

        assertEquals(AuthStateKind.ReauthRequired, state.kind)
    }

    @Test
    fun `FcmNotificationDispatcher approval_request payload triggers callback`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_100", "jkt_100", 120_000L)

        var callbackFired = false
        val dispatcher = FcmNotificationDispatcher(
            stateMachine = machine,
            onApprovalRequestReceived = { payload ->
                callbackFired = true
                assertEquals("req_approval_999", payload.resourceId)
            }
        )

        val state = dispatcher.dispatchIncomingPush(
            FcmPushPayload(
                notificationId = "notif_3",
                title = "Freigabe erforderlich",
                body = "Neuer Inhalt erfordert Eltern-Freigabe",
                type = "approval_request",
                resourceId = "req_approval_999",
                actionRequired = "approve_content"
            )
        )

        assertTrue(callbackFired)
        assertEquals(AuthStateKind.DeviceGrantActive, state.kind)
    }
}
