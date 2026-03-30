package io.github.manugh.xg2g.android

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MainActivitySmokeTest {

    private val context: Context = ApplicationProvider.getApplicationContext()
    private val prefs by lazy { context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE) }

    @Before
    @After
    fun clearServerSettings() {
        prefs.edit().clear().commit()
    }

    @Test
    fun launchWithoutConfiguredServer_showsSetupUi() {
        val scenario = ActivityScenario.launch(MainActivity::class.java)
        try {
            onView(withId(R.id.setup_container)).check(matches(isDisplayed()))
        } finally {
            scenario.close()
        }
    }

    @Test
    fun launchWithBaseUrlExtra_persistsNormalizedServerUrl() {
        val intent = Intent(context, MainActivity::class.java).apply {
            putExtra(ServerTargetResolver.EXTRA_BASE_URL, "demo.example")
        }

        val scenario = ActivityScenario.launch<MainActivity>(intent)
        try {
            assertEquals("https://demo.example/ui/", prefs.getString(PREF_SERVER_URL, null))
        } finally {
            scenario.close()
        }
    }

    @Test
    fun launchWithCustomSchemeLink_persistsLinkedServerUrl() {
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setClass(context, MainActivity::class.java)
            data = Uri.parse("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fui%2F")
        }

        val scenario = ActivityScenario.launch<MainActivity>(intent)
        try {
            assertEquals("https://tv.example/ui/", prefs.getString(PREF_SERVER_URL, null))
        } finally {
            scenario.close()
        }
    }

    @Test
    fun launchWithHttpsDeepLink_persistsDerivedUiBaseUrl() {
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setClass(context, MainActivity::class.java)
            data = Uri.parse("https://demo.example/ui/live/channel-1")
        }

        val scenario = ActivityScenario.launch<MainActivity>(intent)
        try {
            assertEquals("https://demo.example/ui/", prefs.getString(PREF_SERVER_URL, null))
        } finally {
            scenario.close()
        }
    }

    private companion object {
        private const val PREFS_NAME = "app_settings"
        private const val PREF_SERVER_URL = "server_url"
    }
}
