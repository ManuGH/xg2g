# Privacy Policy for xg2g (iOS, iPadOS, tvOS)

**Effective Date:** October 10, 2026  
**Application:** xg2g (Bundle ID: `com.xg2g.app`)  
**Developer:** Manuel (`ManuGH`)

---

## 1. Overview (English)

**xg2g** is a self-hosted personal TV client designed to connect to your own local or remotely managed `xg2g` gateway and Enigma2 DVB receiver.

We believe in strict privacy by design:
- **Zero Data Collection:** The app does **not** collect, transmit, store, or sell any personal data, usage telemetry, or analytics to the developer or any third party.
- **Zero Tracking:** The app includes **no** third-party analytics SDKs, advertising frameworks, crash-reporting trackers, or cross-app tracking identifiers.

### 1.1 Network Communication
- All network traffic occurs directly between your Apple device (iPhone, iPad, or Apple TV) and the `xg2g` server address that you explicitly configure.
- When you use the built-in **Demo Mode** ("Try Demo Mode"), the app uses an embedded mock catalog and streams official Apple public test streams (`https://devstreaming-cdn.apple.com`). No personal information is transmitted.
- Local network permission (`NSLocalNetworkUsageDescription`) is requested solely so your device can connect directly to your own `xg2g` gateway and Enigma2 receiver on your home LAN.

### 1.2 On-Device Storage & Cryptographic Keys
- **UserDefaults (`CA92.1`):** The app stores user preferences (such as your configured server address, playback mode, and favorites) locally on your device using Apple's `UserDefaults` API.
- **Keychain / Secure Enclave:** When pairing with your `xg2g` server, the app generates a hardware-bound cryptographic key pair (RFC 9449 DPoP) stored exclusively in your device's Keychain / Secure Enclave.
- **Data Deletion:** You can delete all stored credentials, cryptographic keys, and cached EPG data at any time inside the app via **Settings → Disconnect & Sign Out Device** and **Clear EPG & Media Cache**, or by uninstalling the application.

### 1.3 Contact & Support
If you have questions regarding this Privacy Policy or the app, please open an issue on our repository:
- **Support & Issue Tracker:** [https://github.com/ManuGH/xg2g/issues](https://github.com/ManuGH/xg2g/issues)

---

## 2. Datenschutzerklärung (Deutsch)

**xg2g** ist ein selbst-gehosteter TV-Client zur Verbindung mit deinem eigenen lokalen oder selbst betriebenen `xg2g`-Gateway und Enigma2-Receiver.

### 2.1 Keine Datenerhebung & Kein Tracking
- **Keine Datensammlung:** Die App erhebt, überträgt, speichert oder verkauft **keinerlei** personenbezogene Daten oder Nutzungsstatistiken an den Entwickler oder Dritte.
- **Kein Tracking:** Die App enthält **keine** Analyse-SDKs von Drittanbietern, Werbenetzwerke oder Tracking-Technologien.

### 2.2 Netzwerkkommunikation
- Sämtliche Netzwerkverbindungen finden ausschließlich direkt zwischen deinem Apple-Gerät (iPhone, iPad oder Apple TV) und der von dir selbst eingetragenen `xg2g`-Serveradresse statt.
- Im integrierten **Demo-Modus** („Demo-Modus starten“) nutzt die App ausschließlich lokale Demo-Metadaten sowie offizielle öffentliche Apple-Test-Streams (`https://devstreaming-cdn.apple.com`).
- Der Zugriff auf das lokale Netzwerk (`NSLocalNetworkUsageDescription`) wird ausschließlich benötigt, um im Heimnetzwerk direkt mit deinem eigenen `xg2g`-Gateway und Enigma2-Receiver zu kommunizieren.

### 2.3 Lokale Speicherung & Sicherheit (Secure Enclave)
- **Einstellungen (`UserDefaults`):** Konfigurierte Einstellungen (Serveradresse, Favoriten, Wiedergabemodus) werden ausschließlich lokal auf dem Gerät gespeichert.
- **Geräteschlüssel (`Keychain` / `Secure Enclave`):** Für die Geräte-Kopplung wird ein kryptografischer DPoP-Schlüssel (RFC 9449) in der Apple Secure Enclave / im Schlüsselbund abgelegt.
- **Löschung:** Über **Einstellungen → Gerät trennen & abmelden** sowie **EPG- & Medien-Cache leeren** oder durch Deinstallation der App werden sämtliche lokalen Schlüssel und Sitzungsdaten vollständig gelöscht.

### 2.4 Kontakt
Bei Fragen zum Datenschutz oder für technischen Support erreichst du uns über GitHub:
- **Support:** [https://github.com/ManuGH/xg2g/issues](https://github.com/ManuGH/xg2g/issues)
