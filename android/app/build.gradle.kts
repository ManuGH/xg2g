plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "io.github.manugh.xg2g.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.manugh.xg2g.android"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        manifestPlaceholders["appLabel"] = "xg2g"
        manifestPlaceholders["usesCleartextTraffic"] = "false"
        manifestPlaceholders["deepLinkScheme"] = "https"
        manifestPlaceholders["deepLinkHost"] = "xg2g.example.invalid"
    }

    buildFeatures {
        buildConfig = true
        compose = true
        resValues = true
    }

    flavorDimensions += "environment"
    productFlavors {
        create("dev") {
            dimension = "environment"
            applicationIdSuffix = ".dev"
            versionNameSuffix = "-dev"
            manifestPlaceholders["appLabel"] = "xg2g Dev"
            manifestPlaceholders["usesCleartextTraffic"] = "true"
            manifestPlaceholders["deepLinkScheme"] = "https"
            manifestPlaceholders["deepLinkHost"] = "xg2g.home.matrixcentral.de"
        }
        create("staging") {
            dimension = "environment"
            applicationIdSuffix = ".staging"
            versionNameSuffix = "-staging"
            manifestPlaceholders["appLabel"] = "xg2g Staging"
            manifestPlaceholders["usesCleartextTraffic"] = "false"
            manifestPlaceholders["deepLinkScheme"] = "https"
            manifestPlaceholders["deepLinkHost"] = "staging.example.invalid"
        }
        create("prod") {
            dimension = "environment"
            manifestPlaceholders["appLabel"] = "xg2g"
            manifestPlaceholders["usesCleartextTraffic"] = "false"
            manifestPlaceholders["deepLinkScheme"] = "https"
            manifestPlaceholders["deepLinkHost"] = "xg2g.example.invalid"
        }
    }

    buildTypes {
        debug {
            buildConfigField("boolean", "WEBVIEW_DEBUGGING", "true")
        }
        release {
            isMinifyEnabled = false
            buildConfigField("boolean", "WEBVIEW_DEBUGGING", "false")
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

}

dependencies {
    implementation("androidx.core:core-ktx:1.18.0")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.activity:activity-ktx:1.13.0")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation(platform("androidx.compose:compose-bom:2026.03.01"))
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.10.0")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.10.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.10.0")
    implementation("com.google.android.material:material:1.13.0")
    implementation("androidx.webkit:webkit:1.15.0")
    implementation("androidx.core:core-splashscreen:1.2.0")
    implementation("androidx.media3:media3-exoplayer:1.10.0")
    implementation("androidx.media3:media3-exoplayer-hls:1.10.0")
    implementation("androidx.media3:media3-ui:1.10.0")
    implementation("androidx.media3:media3-session:1.10.0")
    implementation("androidx.media3:media3-datasource-okhttp:1.10.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
    implementation("com.squareup.okhttp3:okhttp:5.3.2")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
    testImplementation("org.json:json:20250517")

    androidTestImplementation("androidx.test:core-ktx:1.7.0")
    androidTestImplementation("androidx.test.ext:junit:1.3.0")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.7.0")
    androidTestImplementation("androidx.test:runner:1.7.0")
}
