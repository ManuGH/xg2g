package io.github.manugh.xg2g.android

internal enum class TvNavigationDestination(val routePath: String) {
    Home("/dashboard"),
    Guide("/epg"),
    Recordings("/recordings"),
    Timers("/timers"),
    Settings("/settings")
}
