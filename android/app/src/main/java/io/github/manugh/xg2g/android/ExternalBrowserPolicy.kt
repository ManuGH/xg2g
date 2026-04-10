package io.github.manugh.xg2g.android

internal object ExternalBrowserPolicy {
    fun isUsableBrowserHandler(packageName: String?, className: String?): Boolean {
        if (packageName.isNullOrBlank()) {
            return false
        }

        if (packageName in blockedBrowserPackages) {
            return false
        }

        if (!className.isNullOrBlank() && blockedBrowserClassNames.any(className::endsWith)) {
            return false
        }

        return true
    }

    private val blockedBrowserPackages = setOf(
        "com.android.tv.frameworkpackagestubs"
    )

    private val blockedBrowserClassNames = setOf(
        "Stubs\$BrowserStub"
    )
}
