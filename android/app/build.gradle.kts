plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "io.github.manugh.xg2g.android"
    compileSdk = 34

    defaultConfig {
        applicationId = "io.github.manugh.xg2g.android"
        minSdk = 26
        targetSdk = 34
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

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.activity:activity-ktx:1.9.2")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.webkit:webkit:1.11.0")
    implementation("androidx.core:core-splashscreen:1.0.1")

    testImplementation("junit:junit:4.13.2")

    androidTestImplementation("androidx.test:core-ktx:1.6.1")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
}
