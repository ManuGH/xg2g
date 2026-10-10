# Apple App Store Review & Submission Guide (`xg2g`)

Diese Anleitung enthält alle exakten Texte, Einstellungen und Prüfschritte, damit die **xg2g** App (iOS, iPadOS & tvOS) die **Apple App Review** beim ersten Einreichen ohne Ablehnung besteht.

---

## 1. Was im Code bereits für die Prüfung abgesichert wurde

1. **Apple Privacy Manifest (`ios/Xg2g/PrivacyInfo.xcprivacy`)**
   - Deklariert `NSPrivacyTracking = false` (kein Tracking) und leere `NSPrivacyCollectedDataTypes` (keine Datensammlung).
   - Deklariert Pflicht-API `NSPrivacyAccessedAPICategoryUserDefaults` mit Grund `CA92.1` (Zugriff nur durch die App selbst).
2. **Export-Compliance (`ITSAppUsesNonExemptEncryption = false`)**
   - In `ios/Support/Info.plist`, `ios/Support/Info-Debug.plist` und für `Xg2g-tvOS` gesetzt.
   - Verhindert, dass App Store Connect bei jedem Build nach US-Verschlüsselungs-Exportdokumenten fragt (Standard-HTTPS/TLS und RFC 9449 DPoP-Authentifizierung sind gemäß Apple-Richtlinien befreit).
3. **Integrierter Demo-Modus für Apple App Reviewer (Guideline 2.1 & 5.2.3)**
   - Da `xg2g` ein Client für selbst-gehostete Enigma2-Hardware ist und neue Geräte per DPoP-Pairing freigeschaltet werden müssen, befindet sich auf dem Startbildschirm der Button **„Try Demo Mode“ / „Demo-Modus starten“**.
   - Im Demo-Modus sind sofort 4 Demo-Sender, ein vollständiger EPG-Programmguide, DVR-Aufnahmen und Timer verfügbar.
   - Alle Video-Streams im Demo-Modus nutzen ausschließlich **offizielle, lizenzfreie Apple Developer HLS-Teststreams** (`https://devstreaming-cdn.apple.com/.../bipbop_16x9_variant.m3u8`). Dadurch gibt es keinerlei Urheberrechts-Beanstandungen (Guideline 5.2.3).
4. **Bereinigung privater Entwickler-Domains**
   - Der Schnellzugriff auf `xg2g.home.matrixcentral.de` ist mit `#if DEBUG` geschützt und im Release-/App-Store-Build unsichtbar.
5. **In-App Datenschutzerklärung & Support-Links**
   - Unter **Einstellungen → Über xg2g** sind eine vollständige In-App-Datenschutzerklärung (auch auf Apple TV lesbar) sowie Links zur Online-Datenschutzerklärung und zum Support integriert.

---

## 2. Voraussetzung vor dem Upload: Apple Developer Program

Aktuell ist in Xcode ein kostenloses **Personal Team (`YTM9QRNJ42`)** hinterlegt. Für TestFlight und den App Store wird eine aktive Mitgliedschaft im **Apple Developer Program** (99 € / Jahr) benötigt:
1. Unter [https://developer.apple.com/programs/enroll/](https://developer.apple.com/programs/enroll/) mit deiner Apple-ID anmelden.
2. Nach Freischaltung in **Xcode → Settings (⌘,) → Accounts** deinen Account auswählen, dann im Projekt `Xg2g` unter **Signing & Capabilities** für die Targets `Xg2g` und `Xg2g-tvOS` dein offizielles Developer Team auswählen.
3. Unter [https://appstoreconnect.apple.com](https://appstoreconnect.apple.com) → **Apps → (+ Neue App)** eine App mit der Bundle-ID `com.xg2g.app` anlegen.

---

## 3. Copy & Paste Vorlagen für App Store Connect

### A. App-Informationen
- **Name:** `xg2g`
- **Untertitel (DE):** `Enigma2 Gateway & Live-TV Client`
- **Untertitel (EN):** `Personal Gateway & TV Client`
- **Primäre Kategorie:** `Entertainment` (Unterhaltung)
- **Sekundäre Kategorie:** `Utilities` (Dienstprogramme)
- **Altersfreigabe:** `4+` (Keine anstößigen Inhalte)

### B. URLs in App Store Connect
- **Datenschutzrichtlinien-URL (Privacy Policy URL):**
  `https://github.com/ManuGH/xg2g/blob/main/docs/PRIVACY_POLICY_IOS.md`
- **Support-URL:**
  `https://github.com/ManuGH/xg2g/issues`
- **Marketing-URL (optional):**
  `https://github.com/ManuGH/xg2g`

### C. Beschreibungstext (WICHTIG gegen Guideline 5.2.3 Ablehnung!)
> **Hinweis:** Apple prüft IPTV-/TV-Apps sehr streng. Die Beschreibung muss unmissverständlich klarstellen, dass **keine** TV-Sender oder Medieninhalte mitgeliefert oder verkauft werden, sondern eigene Hardware vorausgesetzt wird.

**Deutsch:**
```text
xg2g ist ein moderner, nativer Client für dein selbst-gehostetes xg2g-Gateway und deinen eigenen Enigma2-basierten DVB-Receiver (Satellit, Kabel oder Terrestrisch).

WICHTIGER HINWEIS:
xg2g stellt KEINE eigenen Fernsehsender, Streams, Medieninhalte oder IPTV-Abonnements bereit. Zur regulären Nutzung benötigst du deinen eigenen Enigma2-Receiver sowie eine eigene xg2g-Server-Instanz in deinem Heimnetzwerk. Zum unverbindlichen Ausprobieren der Benutzeroberfläche ist ein integrierter Demo-Modus mit lizenzfreien Apple-Teststreams enthalten.

Funktionen:
• Live-TV & schnelles Kanal-Zapping mit Hardware-Beschleunigung
• Übersichtlicher Elektronischer Programmführer (EPG) mit Jetzt/Gleich-Ansicht und Zeitleiste
• Verwaltung und Wiedergabe deiner eigenen DVR-Aufnahmen inkl. Offline-Downloads auf iPhone & iPad
• Programmierung und Verwaltung von Aufnahme-Timern
• Moderne kryptografische Geräte-Kopplung (RFC 9449 DPoP) über die Apple Secure Enclave
• Keine Werbung, kein Tracking, keine Datensammlung
```

**English:**
```text
xg2g is a modern native client for your self-hosted xg2g gateway and your own Enigma2-based DVB receiver (satellite, cable, or terrestrial).

IMPORTANT NOTICE:
xg2g does NOT provide, host, or sell any television channels, media content, or IPTV subscriptions. To use this app with live broadcasts, you must own and configure your own Enigma2 hardware receiver and self-hosted xg2g gateway server. A built-in Demo Mode using official Apple sample streams is included to preview the interface without hardware.

Features:
• Low-latency Live TV playback and fast channel zapping
• Rich Electronic Program Guide (EPG) with Now/Next and timeline views
• Browse, stream, and download your personal DVR recordings for offline viewing on iPhone & iPad
• Create and manage recording timers remotely
• Hardware-bound cryptographic device pairing (RFC 9449 DPoP) powered by the Apple Secure Enclave
• Zero tracking, zero analytics, 100% privacy
```

### D. App Review Information → Notes for Reviewer (EXTREM WICHTIG!)
Kopiere diesen englischen Text exakt in das Feld **„Notes“ (Hinweise zur Überprüfung)** in App Store Connect. Damit weiß der Apple-Prüfer sofort, wie er die App ohne Satelliten-Receiver testen kann:

```text
Thank you for reviewing xg2g!

1. PURPOSE & HARDWARE REQUIREMENT:
xg2g is a self-hosted hardware companion client for users who own a physical Enigma2 DVB receiver (satellite/cable tuner) and run the open-source xg2g gateway on their local network. The app does NOT provide, bundle, or sell any commercial TV streams or third-party broadcast content.

2. HOW TO TEST WITHOUT HARDWARE (BUILT-IN DEMO MODE):
Because connecting to a real server requires physical DVB hardware on a local LAN and manual DPoP cryptographic device approval, we have included a complete built-in Demo Mode for App Review:
- Launch the app.
- On the initial "Connect to xg2g" screen, tap the "Try Demo Mode" ("Demo-Modus starten") button right below the Connect button.
- The app will immediately sign in to an embedded demo environment featuring 4 sample channels, a live EPG guide, DVR recordings, and timer creation/deletion.
- All video playback in Demo Mode uses official Apple Developer HLS test streams (https://devstreaming-cdn.apple.com/videos/streaming/examples/img_bipbop_adv_example_ts/master.m3u8).
- You can exit Demo Mode at any time via Settings -> Exit Demo Mode.
```
*(Checkbox „Sign-in required“ in App Store Connect kannst du deaktiviert lassen oder mit Hinweis auf `Try Demo Mode — no credentials required` ausfüllen).*

---

## 4. Release-Build erstellen & hochladen

Sobald dein bezahlter Apple Developer Account in Xcode aktiv ist, kannst du das Release-Archiv entweder direkt in Xcode (**Product → Archive**) oder über unser vorbereitetes Skript bauen:

```bash
./ios/scripts/archive-release.sh
```

Anschließend öffnet sich der **Xcode Organizer**, über den du mit **„Distribute App“ → „App Store Connect“** das Archiv direkt zu TestFlight und zur App Review hochlädst.
