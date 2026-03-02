📦 Vorgabe: Vorbereitung eines Repos für Sandbox-Offline-Test (ChatGPT)

Ziel

Die ZIP muss ermöglichen:
	•	das Repository komplett offline zu prüfen,
	•	Offline-Reproduzierbarkeit mechanisch zu verifizieren,
	•	alle Netz-Risiken eindeutig zu identifizieren,
	•	und valide Aussagen zu treffen, ob das Projekt air-gap-fähig ist.

Entscheidung (bindend)

WebUI-Modell A (empfohlen):
→ WebUI nicht offline bauen, webui/dist/ kommt fix in die ZIP

⸻

1️⃣ Repository-Inhalt (MUSS in der ZIP enthalten sein)

Go-Core (Pflicht)
	•	go.mod
	•	go.sum
	•	vendor/ vollständig und aktuell
	•	alle Go-Packages inkl. Tests
	•	alle generierten Dateien bereits eingecheckt
	•	OpenAPI Clients
	•	mocks
	•	embedded assets (//go:embed)
	•	Makefile
	•	alle lokal referenzierten Skripte (scripts/, hack/, etc.)

❌ Nicht erlaubt
	•	Generatoren, die erst beim Testen Code erzeugen und dafür Netz brauchen
	•	implizite go generate Schritte im Default-Flow

⸻

2️⃣ Toolchain-Regeln (kritisch!)

Go
	•	KEIN go install, go get, @latest
	•	GOTOOLCHAIN darf nicht hart überschrieben werden
	•	Makefile muss kompatibel mit:

GOTOOLCHAIN ?= go1.25.6
export GOTOOLCHAIN
GO := go

Ich werde testen mit:

export GOTOOLCHAIN=local

Wenn das bricht → nicht offline-fähig.

⸻

3️⃣ Offline-sichere Targets (MUSS vorhanden sein)

Pflicht-Target

quality-gates-offline

Dieses Target DARF NUR:
	•	go test ./...
	•	ggf. go vet ./...
	•	lokale Smoke-Tests
	•	lokale Verifikationen

VERBOTEN in quality-gates-offline
	•	curl, wget
	•	npx, npm, pnpm, yarn
	•	go install, go get
	•	Docker pulls
	•	Security-Scanner mit Online-Feeds
	•	irgendwas mit @latest

Online-Checks gehören explizit in:

quality-gates-online

⸻

4️⃣ WebUI: explizite Entscheidung (MUSS klar sein)

Der Techniker muss eine dieser Optionen bewusst wählen:

Option A – WebUI nicht offline bauen (empfohlen)
	•	webui/dist/ ist in der ZIP enthalten
	•	Backend nutzt diese Assets
	•	quality-gates-offline baut keine WebUI

Option B – WebUI offline bauen (nur wenn vorbereitet!)
	•	node_modules Cache oder
	•	internes npm mirror (hier nicht verfügbar → wird scheitern)

⚠️ Wenn nichts davon erfüllt ist → WebUI-Build wird hier bewusst scheitern und als Offline-Fehler markiert.

⸻

5️⃣ ZIP-Format & Struktur

Erwartete Struktur

repo-root/
├─ go.mod
├─ go.sum
├─ vendor/
├─ Makefile
├─ cmd/
├─ internal/
├─ pkg/
├─ webui/
│  └─ dist/        # falls Option A
├─ scripts/
└─ README.md

ZIP-Regeln
	•	kein .git/
	•	kein .env
	•	keine Secrets
	•	keine externen Submodule

⸻

6️⃣ README: Minimal-Hinweis (sehr empfohlen)

Im README.md bitte klar dokumentieren:

Offline test:
  export GOTOOLCHAIN=local
  export GOPROXY=off GOSUMDB=off GOVCS="*:off"
  make quality-gates-offline

Das ist mein Referenz-Runbook.

⸻

7️⃣ Was ich in der Sandbox konkret tun werde

Nach Upload der ZIP:
	1.	Static Audit
	•	Makefile & Scripts nach Netz-Touchpoints scannen
	2.	Offline-Simulation

GOTOOLCHAIN=local
GOPROXY=off GOSUMDB=off GOVCS="*:off"
go test ./...
make quality-gates-offline

	3.	Report
	•	Was ist offline-reproduzierbar ✔
	•	Was bricht garantiert offline ❌
	•	Konkrete Fix-Liste (keine Theorie)

⸻

8️⃣ Wichtiges Erwartungs-Management (für den Techniker)
	•	Wenn etwas nicht in der ZIP ist, existiert es für mich nicht
	•	Wenn etwas online zieht, markiere ich es als Design-Fehler, nicht als “Umgebungsproblem”
	•	Ziel ist Beweisbarkeit, nicht “es läuft eh irgendwie”

⸻

Kurzfassung für den Techniker

„Bereite die ZIP so vor, als würde sie in einem Hochsicherheits-Airgap getestet.
Alles, was beim Test gebraucht wird, muss drin sein.
Alles, was Netz braucht, muss draußen bleiben oder klar getrennt sein.“
