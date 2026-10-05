package io.github.manugh.xg2g.android.ui

import io.github.manugh.xg2g.android.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

class ErrorPresentationPolicyTest {

    companion object {
        private const val DISTINCTIVE_RAW_ERROR =
            "DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761: SELECT password FROM users WHERE id = 1"

        private lateinit var defaultCatalog: Map<String, String>
        private lateinit var germanCatalog: Map<String, String>
        private lateinit var idToNameMap: Map<Int, String>

        @JvmStatic
        @BeforeClass
        fun loadCatalogs() {
            val projectRoot = findProjectRoot()
            val defaultFile = File(projectRoot, "android/app/src/main/res/values/strings.xml").let {
                if (it.exists()) it else File(projectRoot, "app/src/main/res/values/strings.xml")
            }
            val germanFile = File(projectRoot, "android/app/src/main/res/values-de/strings.xml").let {
                if (it.exists()) it else File(projectRoot, "app/src/main/res/values-de/strings.xml")
            }

            defaultCatalog = parseCatalog(defaultFile)
            germanCatalog = parseCatalog(germanFile)

            idToNameMap = R.string::class.java.fields.associate { field ->
                field.getInt(null) to field.name
            }
        }

        private fun parseCatalog(file: File): Map<String, String> {
            val factory = DocumentBuilderFactory.newInstance()
            val builder = factory.newDocumentBuilder()
            val doc = builder.parse(file)
            val nodes = doc.getElementsByTagName("string")
            val map = mutableMapOf<String, String>()
            for (i in 0 until nodes.length) {
                val node = nodes.item(i) as? Element ?: continue
                map[node.getAttribute("name")] = node.textContent
            }
            return map
        }

        private fun findProjectRoot(): File {
            var dir: File? = File(".").canonicalFile
            while (dir != null) {
                if (File(dir, "android/app/src/main/res/values/strings.xml").exists() ||
                    File(dir, "app/src/main/res/values/strings.xml").exists()) {
                    return dir
                }
                dir = dir.parentFile
            }
            return File(".").canonicalFile
        }
    }

    private fun resolveEn(resId: Int): String {
        val name = idToNameMap[resId] ?: error("Unknown resource ID $resId")
        return defaultCatalog[name] ?: error("Key '$name' not in default catalog")
    }

    private fun resolveEnWithArg(resId: Int, arg: Any): String {
        val pattern = resolveEn(resId)
        return String.format(pattern, arg)
    }

    private fun resolveDe(resId: Int): String {
        val name = idToNameMap[resId] ?: error("Unknown resource ID $resId")
        return germanCatalog[name] ?: error("Key '$name' not in German catalog")
    }

    private fun resolveDeWithArg(resId: Int, arg: Any): String {
        val pattern = resolveDe(resId)
        return String.format(pattern, arg)
    }

    @Test
    fun `settings message suppresses raw diagnostic error string under EN and DE`() {
        val enResult = ErrorPresentationPolicy.formatSettingsMessage(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveEn,
            getStringWithArg = ::resolveEnWithArg
        )
        assertFalse("Raw distinctive error must never leak into EN settings message",
            enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertFalse("Raw SQL fragment must never leak into EN settings message",
            enResult.contains("SELECT password"))
        assertEquals("Settings updated successfully.", enResult)

        val deResult = ErrorPresentationPolicy.formatSettingsMessage(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveDe,
            getStringWithArg = ::resolveDeWithArg
        )
        assertFalse("Raw distinctive error must never leak into DE settings message",
            deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertFalse("Raw SQL fragment must never leak into DE settings message",
            deResult.contains("SELECT password"))
        assertEquals("Einstellungen wurden aktualisiert.", deResult)
    }

    @Test
    fun `pairing error suppresses raw error and returns catalog message under EN and DE`() {
        val enResult = ErrorPresentationPolicy.formatPairingError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveEn
        )
        assertFalse("EN pairing error must not leak raw exception", enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Failed to start pairing.", enResult)

        val deResult = ErrorPresentationPolicy.formatPairingError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveDe
        )
        assertFalse("DE pairing error must not leak raw exception", deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Kopplung konnte nicht gestartet werden.", deResult)
    }

    @Test
    fun `unlock error suppresses raw error and returns catalog message under EN and DE`() {
        val enResult = ErrorPresentationPolicy.formatUnlockError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveEn
        )
        assertFalse("EN unlock error must not leak raw exception", enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Unlock failed.", enResult)

        val deResult = ErrorPresentationPolicy.formatUnlockError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveDe
        )
        assertFalse("DE unlock error must not leak raw exception", deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Freischaltung fehlgeschlagen.", deResult)
    }

    @Test
    fun `scan error suppresses raw error and returns catalog message under EN and DE`() {
        val enResult = ErrorPresentationPolicy.formatScanError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveEn
        )
        assertFalse("EN scan error must not leak raw exception", enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Failed to start scan.", enResult)

        val deResult = ErrorPresentationPolicy.formatScanError(
            raw = DISTINCTIVE_RAW_ERROR,
            getString = ::resolveDe
        )
        assertFalse("DE scan error must not leak raw exception", deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Sendersuchlauf konnte nicht gestartet werden.", deResult)
    }

    @Test
    fun `diagnostic status formatters suppress raw error under EN and DE`() {
        val enReceiver = ErrorPresentationPolicy.formatReceiverStatus(DISTINCTIVE_RAW_ERROR, ::resolveEn)
        assertFalse("EN receiver status must not leak raw error", enReceiver.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Unavailable", enReceiver)

        val deReceiver = ErrorPresentationPolicy.formatReceiverStatus(DISTINCTIVE_RAW_ERROR, ::resolveDe)
        assertFalse("DE receiver status must not leak raw error", deReceiver.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Nicht verfügbar", deReceiver)

        val enEpg = ErrorPresentationPolicy.formatEpgStatus(DISTINCTIVE_RAW_ERROR, ::resolveEn)
        assertFalse("EN EPG status must not leak raw error", enEpg.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Unavailable", enEpg)

        val deEpg = ErrorPresentationPolicy.formatEpgStatus(DISTINCTIVE_RAW_ERROR, ::resolveDe)
        assertFalse("DE EPG status must not leak raw error", deEpg.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Nicht verfügbar", deEpg)
    }

    @Test
    fun `web error detail in MainActivity suppresses raw throwable under EN and DE`() {
        val enResult = ErrorPresentationPolicy.resolveWebErrorDetail(::resolveEn)
        assertFalse("EN web error detail must not leak raw error", enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("The app could not load xg2g right now.", enResult)

        val deResult = ErrorPresentationPolicy.resolveWebErrorDetail(::resolveDe)
        assertFalse("DE web error detail must not leak raw error", deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Die App konnte xg2g derzeit nicht laden.", deResult)
    }

    @Test
    fun `auth revocation and session expiry details suppress raw reasons under EN and DE`() {
        val enRevoked = ErrorPresentationPolicy.resolveAuthRevokedDetail(::resolveEn)
        assertFalse(enRevoked.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Device access has been revoked by the administrator.", enRevoked)

        val deRevoked = ErrorPresentationPolicy.resolveAuthRevokedDetail(::resolveDe)
        assertFalse(deRevoked.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Der Geräte-Zugriff wurde vom Administrator widerrufen.", deRevoked)

        val enReauth = ErrorPresentationPolicy.resolveAuthReauthDetail(::resolveEn)
        assertFalse(enReauth.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("The session has expired. Please sign in again.", enReauth)

        val deReauth = ErrorPresentationPolicy.resolveAuthReauthDetail(::resolveDe)
        assertFalse(deReauth.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Die Sitzung ist abgelaufen. Bitte erneut anmelden.", deReauth)
    }

    @Test
    fun `playback error suppresses raw diagnostic error string and SQL fragment under EN and DE`() {
        val enResult = ErrorPresentationPolicy.formatPlaybackError(DISTINCTIVE_RAW_ERROR, ::resolveEn)
        assertFalse("EN playback error must not leak raw error", enResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertFalse("EN playback error must not leak raw SQL", enResult.contains("SELECT password"))
        assertEquals("Playback error", enResult)

        val deResult = ErrorPresentationPolicy.formatPlaybackError(DISTINCTIVE_RAW_ERROR, ::resolveDe)
        assertFalse("DE playback error must not leak raw error", deResult.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertFalse("DE playback error must not leak raw SQL", deResult.contains("SELECT password"))
        assertEquals("Wiedergabefehler", deResult)

        val enDecoder = ErrorPresentationPolicy.formatPlaybackError("MediaCodec video decoder initialization failed: c2.goldfish.decoder", ::resolveEn)
        assertFalse(enDecoder.contains("c2.goldfish.decoder"))
        assertEquals("Video decoder error", enDecoder)

        val deDecoder = ErrorPresentationPolicy.formatPlaybackError("MediaCodec video decoder initialization failed: c2.goldfish.decoder", ::resolveDe)
        assertFalse(deDecoder.contains("c2.goldfish.decoder"))
        assertEquals("Videodecoder-Fehler", deDecoder)
    }

    @Test
    fun `playback warning suppresses raw string and maps to catalog warning under EN and DE`() {
        val enWarning = ErrorPresentationPolicy.formatPlaybackWarning("Audio unavailable: ac3 (passthrough denied)", ::resolveEn)
        assertFalse(enWarning.contains("passthrough denied"))
        assertEquals("Audio track unavailable", enWarning)

        val deWarning = ErrorPresentationPolicy.formatPlaybackWarning("Audio unavailable: ac3 (passthrough denied)", ::resolveDe)
        assertFalse(deWarning.contains("passthrough denied"))
        assertEquals("Tonspur nicht verfügbar", deWarning)

        val enDegraded = ErrorPresentationPolicy.formatPlaybackWarning(DISTINCTIVE_RAW_ERROR, ::resolveEn)
        assertFalse(enDegraded.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Playback degraded", enDegraded)

        val deDegraded = ErrorPresentationPolicy.formatPlaybackWarning(DISTINCTIVE_RAW_ERROR, ::resolveDe)
        assertFalse(deDegraded.contains("DISTINCTIVE_RAW_SQL_INJECTION_ERR_7761"))
        assertEquals("Eingeschränkte Wiedergabe", deDegraded)
    }

    @Test
    fun `playback session state maps to catalog strings without leaking wire protocol tokens`() {
        val enActive = ErrorPresentationPolicy.formatPlaybackSessionState("ACTIVE", ::resolveEn)
        assertEquals("Playback active", enActive)

        val deActive = ErrorPresentationPolicy.formatPlaybackSessionState("ACTIVE", ::resolveDe)
        assertEquals("Wiedergabe aktiv", deActive)

        val enReady = ErrorPresentationPolicy.formatPlaybackSessionState("READY", ::resolveEn)
        assertEquals("Playback ready", enReady)

        val deReady = ErrorPresentationPolicy.formatPlaybackSessionState("READY", ::resolveDe)
        assertEquals("Wiedergabe bereit", deReady)

        val enError = ErrorPresentationPolicy.formatPlaybackSessionState("ERROR", ::resolveEn)
        assertEquals("Playback error", enError)

        val deError = ErrorPresentationPolicy.formatPlaybackSessionState("FAILED", ::resolveDe)
        assertEquals("Wiedergabefehler", deError)
    }
}
