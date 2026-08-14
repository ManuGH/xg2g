package io.github.manugh.xg2g.android

import android.app.Application
import io.github.manugh.xg2g.android.auth.NativeAuthContainer

class Xg2gApplication : Application() {
    internal val authContainer: NativeAuthContainer by lazy {
        NativeAuthContainer.getInstance(this)
    }
}
