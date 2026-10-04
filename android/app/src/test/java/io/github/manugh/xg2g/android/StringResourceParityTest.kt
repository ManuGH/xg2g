package io.github.manugh.xg2g.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

class StringResourceParityTest {

    @Test
    fun `default and german string resources have 100 percent key parity`() {
        val projectRoot = findProjectRoot()
        val defaultStringsFile = File(projectRoot, "android/app/src/main/res/values/strings.xml").let {
            if (it.exists()) it else File(projectRoot, "app/src/main/res/values/strings.xml")
        }
        val germanStringsFile = File(projectRoot, "android/app/src/main/res/values-de/strings.xml").let {
            if (it.exists()) it else File(projectRoot, "app/src/main/res/values-de/strings.xml")
        }

        assertTrue("Default strings.xml must exist: ${defaultStringsFile.absolutePath}", defaultStringsFile.exists())
        assertTrue("German strings.xml must exist: ${germanStringsFile.absolutePath}", germanStringsFile.exists())

        val defaultKeys = parseStringKeys(defaultStringsFile)
        val germanKeys = parseStringKeys(germanStringsFile)

        val missingInGerman = defaultKeys - germanKeys
        val extraInGerman = germanKeys - defaultKeys

        assertTrue("Keys missing in German strings.xml: $missingInGerman", missingInGerman.isEmpty())
        assertTrue("Extra keys in German strings.xml not in default: $extraInGerman", extraInGerman.isEmpty())
        assertEquals("Both catalogs must contain exactly the same number of keys", defaultKeys.size, germanKeys.size)
        assertTrue("Expected non-empty string catalog", defaultKeys.isNotEmpty())
    }

    @Test
    fun `critical error and diagnostic keys exist in both locales with localized translations`() {
        val projectRoot = findProjectRoot()
        val defaultStringsFile = File(projectRoot, "android/app/src/main/res/values/strings.xml").let {
            if (it.exists()) it else File(projectRoot, "app/src/main/res/values/strings.xml")
        }
        val germanStringsFile = File(projectRoot, "android/app/src/main/res/values-de/strings.xml").let {
            if (it.exists()) it else File(projectRoot, "app/src/main/res/values-de/strings.xml")
        }

        val defaultMap = parseStringKeyValueMap(defaultStringsFile)
        val germanMap = parseStringKeyValueMap(germanStringsFile)

        val criticalKeys = listOf(
            "guide_error_title",
            "guide_generic_detail",
            "guide_auth_title",
            "guide_auth_detail",
            "dashboard_recordings_error",
            "dashboard_recordings_auth_required",
            "dashboard_timers_error",
            "dashboard_timers_auth_required",
            "settings_pairing_error_start",
            "settings_pairing_error_expired",
            "settings_admin_wrong_pin",
            "settings_admin_unlock_failed",
            "settings_scan_start_failed",
            "settings_diag_status_online_ready",
            "settings_diag_status_unreachable",
            "settings_diag_status_unavailable"
        )

        for (key in criticalKeys) {
            assertTrue("Default map must contain $key", defaultMap.containsKey(key))
            assertTrue("German map must contain $key", germanMap.containsKey(key))
            val enVal = defaultMap[key]!!
            val deVal = germanMap[key]!!
            assertTrue("Key $key must not be blank in EN", enVal.isNotBlank())
            assertTrue("Key $key must not be blank in DE", deVal.isNotBlank())
            // Verify that DE and EN values are translated and distinct
            assertTrue("Key $key should have distinct localized values for EN ('$enVal') and DE ('$deVal')", enVal != deVal)
        }
    }

    private fun parseStringKeyValueMap(file: File): Map<String, String> {
        val factory = DocumentBuilderFactory.newInstance()
        val builder = factory.newDocumentBuilder()
        val doc = builder.parse(file)
        val stringNodes = doc.getElementsByTagName("string")

        val map = mutableMapOf<String, String>()
        for (i in 0 until stringNodes.length) {
            val node = stringNodes.item(i) as? Element ?: continue
            val name = node.getAttribute("name")
            val text = node.textContent
            map[name] = text
        }
        return map
    }

    private fun parseStringKeys(file: File): Set<String> {
        val factory = DocumentBuilderFactory.newInstance()
        val builder = factory.newDocumentBuilder()
        val doc = builder.parse(file)
        val stringNodes = doc.getElementsByTagName("string")

        val keys = mutableSetOf<String>()
        for (i in 0 until stringNodes.length) {
            val node = stringNodes.item(i) as? Element ?: continue
            val name = node.getAttribute("name")
            assertTrue("String key must not be blank in ${file.name}", name.isNotBlank())
            assertTrue("Duplicate key '$name' found in ${file.name}", keys.add(name))
            val textContent = node.textContent
            assertTrue("Key '$name' in ${file.name} must not be empty", textContent.isNotBlank())
        }
        return keys
    }

    private fun findProjectRoot(): File {
        var dir: File? = File(".").canonicalFile
        while (dir != null) {
            if (File(dir, "android/app/src/main/res/values/strings.xml").exists()) {
                return dir
            }
            if (File(dir, "app/src/main/res/values/strings.xml").exists()) {
                return dir
            }
            dir = dir.parentFile
        }
        return File(".").canonicalFile
    }
}
