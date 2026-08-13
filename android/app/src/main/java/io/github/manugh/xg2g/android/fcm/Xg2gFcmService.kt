package io.github.manugh.xg2g.android.fcm

import android.util.Log
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import io.github.manugh.xg2g.android.ServerSettingsStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

class Xg2gFcmService : FirebaseMessagingService() {

    override fun onNewToken(token: String) {
        super.onNewToken(token)
        Log.d(TAG, "New FCM Token received: $token")

        val settingsStore = ServerSettingsStore(applicationContext)
        val serverUrl = settingsStore.getServerUrl() ?: return
        val authToken = settingsStore.getAuthToken()
        val manager = FcmTokenManager()

        CoroutineScope(Dispatchers.IO).launch {
            try {
                manager.registerFcmToken(baseUrl = serverUrl, fcmToken = token, bearerToken = authToken)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to register FCM token with backend", e)
            }
        }
    }

    override fun onMessageReceived(remoteMessage: RemoteMessage) {
        super.onMessageReceived(remoteMessage)
        val data = remoteMessage.data
        Log.d(TAG, "FCM Push payload received: data=$data")

        val payload = FcmPushPayload(
            notificationId = data["id"] ?: data["notificationId"] ?: "",
            title = data["title"] ?: "",
            body = data["body"] ?: "",
            type = data["type"] ?: data["actionRequired"] ?: "",
            resourceId = data["resourceId"],
            actionRequired = data["actionRequired"]
        )
        Log.i(TAG, "Parsed FCM Push payload: $payload")
    }

    private companion object {
        const val TAG = "Xg2gFcmService"
    }
}
