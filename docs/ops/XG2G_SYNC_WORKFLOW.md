# xg2g Sync- und Build-Workflow

## Zweck

`xg2g` ist eine Linux-/Go-/Docker-Anwendung. Deshalb werden Entwicklung,
Linux-Build und Runtime getrennt behandelt. Es gibt keine bidirektionale
Dateisynchronisation zwischen dem Mac und Proxmox.

Der verbindliche Zustand ist ein Git-Commit auf GitHub. Alle anderen Kopien
werden auf diesen Commit geprüft oder reproduzierbar daraus erzeugt.

## Zuständigkeit der Systeme

| System | Rolle | Darf Produktcode schreiben? |
| --- | --- | --- |
| Mac `StudioProjects` | Entwicklung, Review, Commit und Push | Ja, durch Manuel/Codex |
| GitHub | kanonische Commit-/PR-Quelle | nur über geprüfte PRs/Pushes |
| Proxmox-Hypervisor `pve2` | LXC-/VM-Management, kein Checkout und kein Build | Nein |
| LXC 110 `/srv/xg2g-build` | sauberer, abgetrennter Linux-Build-Checkout | nur expliziter Staging-/`sync-build`-Workflow |
| LXC 110 `/srv/xg2g-staging` | Staging-Deployment auf `:8089`, kein Git-Checkout | nur explizites Staging-Deployment |
| LXC 110 `/srv/xg2g` | Produktions-Installationsfläche auf `:8088`, kein Build-Checkout | nur kanonischer Sync/Promotion nach Produktionsfreigabe |

Ein alter Git-Checkout unter `/srv/xg2g` ist Migrationsdrift und keine
Autorisierung, dort zu pullen oder zu bauen. Die aktuelle Topologie benötigt
keinen Checkout auf dem Proxmox-Hypervisor.

## Standardablauf

### Zustandsmodell

Ein Git-Commit bedeutet nicht automatisch „fertig“:

1. **Lokaler Checkpoint** – Commit auf dem Mac; kann WIP sein und wird nicht
   automatisch gepusht.
2. **Review-Kandidat** – bewusst auf einen Feature-Branch gepusht; noch keine
   Freigabe und kein Deployment.
3. **Staging-Test** – ein gepushter Feature- oder Main-Commit wird ausdrücklich
   für LXC 110 ausgewählt und auf `:8089` getestet. Dieser Schritt dient gerade
   dazu, Tests und Playback-Verhalten vor der Produktionsfreigabe zu prüfen.
4. **Produktionsfreigabe** – ausschließlich nach Manuel-Freigabe und separatem
   Promote-Schritt auf `:8088`.

Kein Agent darf einen Zustand stillschweigend in den nächsten überführen.

### Verbindliche Umgebungszustände

Die Portnummer ist niemals ein Versionssignal. Die laufenden Artefakte dürfen
nur einen dieser beiden Zustände bilden:

- **`baseline`**: Produktion `:8088` und Staging `:8089` haben dasselbe
  unveränderliche OCI-Image, denselben Git-Commit und denselben
  Binary-SHA-256-Hash. In diesem Zustand gibt es keinen Binary-Override.
- **`candidate`**: Staging ist ein nachweislicher Git-Nachfolger von Produktion
  und das schema-v2 Deployment-Manifest nennt exakt seinen Commit und
  Binary-Hash.

`stale` (Staging älter), `diverged`, ein abweichender Build desselben Commits
und ein nicht im Manifest erfasster Kandidat sind Fehlerzustände. Prüfen:

```bash
scripts/check-deployment-state.sh
```

Der direkte Runtime-Host ist standardmäßig der SSH-Alias `xg2g-dev` und kann
für Status und Baseline-Sync gezielt mit `XG2G_RUNTIME_HOST` überschrieben
werden. Diese Variable ist absichtlich von `XG2G_DEPLOY_HOST` des
Proxmox-Promotion-Schritts getrennt.

Auf LXC 110 sind beide Runtime-Ports an Loopback gebunden. Produktion wird
ausschließlich über den verwalteten Reverse Proxy erreicht; Staging nur über
einen ausdrücklichen SSH-/VPN-Operatorpfad. Ein Publish auf `0.0.0.0:8089`
oder unbeschränktes IPv6 ist ein Fehlerzustand.

Nach einer Image-basierten Produktionsaktualisierung wird Staging ohne Neubau
auf das tatsächlich laufende Produktionsartefakt zurückgeführt:

```bash
scripts/sync-staging-baseline.sh --confirm-staging-baseline
scripts/check-deployment-state.sh
```

Der Baseline-Sync pinnt das Staging-Compose auf den tatsächlich laufenden
Produktions-Image-Digest, entfernt den nur für Kandidaten erlaubten
Binary-Override, erzwingt `127.0.0.1:8089` und erstellt den Staging-Container
mit seiner bestehenden Umgebungs- und Datenkonfiguration neu. Produktion,
Produktionsdaten und die logischen Portrollen bleiben unverändert.

Auf dem Mac:

```bash
git status --short --branch
git add <gezielte-dateien>
git commit -m "<kohaerente aenderung>"
git push -u origin <branch>
scripts/reconcile_xg2g.sh status
```

Nach dem Push kann der isolierte Linux-Build-Checkout in LXC 110 auf genau
diesen Commit gebracht werden:

```bash
scripts/reconcile_xg2g.sh sync-build --commit <sha>
```

Dieser Schritt verändert ausschließlich `/srv/xg2g-build`. Er verändert weder
die Staging-Fläche `/srv/xg2g-staging` noch die Produktionsfläche
`/srv/xg2g`.

Für den Test in LXC 110 folgt danach:

```bash
scripts/fast_deploy.sh --confirm-staging
```

`fast_deploy.sh` verlangt einen sauberen Mac-Checkout und dass `HEAD` exakt dem
gepushten Remote-Branch entspricht. Es deployt ausschließlich den Teststand
auf `:8089`; Produktion `:8088` bleibt unberührt. `--confirm-staging` bestätigt
nur den Start dieses Testdeployments, nicht die Produktionsreife.

Ein schneller Branch-Binary-Test ist niemals direkt promotbar. Für eine
Produktionsfreigabe wird nach Veröffentlichung des unveränderlichen
Release-Images exakt dieses Image separat auf Staging geprüft:

```bash
scripts/stage-release-candidate.sh --ref vX.Y.Z --confirm-staging
scripts/check-deployment-state.sh
scripts/promote_production.sh --ref vX.Y.Z --confirm-production
```

Der Promote-Schritt akzeptiert nur den exakt passenden Staging-Tag und
Git-Commit. Er verwendet im LXC den kanonischen Installationspfad
`xg2g-admin update`; eine rohe Binary wird niemals in Produktion kopiert.
Nach erfolgreicher Produktionsprüfung stellt derselbe Befehl automatisch den
`baseline`-Zustand wieder her.

## Statusprüfung

```bash
scripts/reconcile_xg2g.sh status
```

Die Prüfung zeigt mindestens:

- Mac-Branch, Mac-Commit und Dirty-Count,
- GitHub-Commit des aktuellen Branches,
- LXC-Build-Checkout inklusive Commit und Dirty-Count,
- Staging-Manifest, Health und laufenden Binary-Hash.

Für die verbindliche Produktions-/Staging-Beziehung ist zusätzlich
`scripts/check-deployment-state.sh` auszuführen. Der Befehl liest die
Prozessmetadaten mit `xg2g --version`, vergleicht die tatsächlichen
Image-IDs und Binary-Hashes und schlägt bei jedem unerlaubten Zustand fehl.

Abweichungen sind normal, solange sie erklärbar sind. Ein uncommitted Mac-
Stand darf gegenüber GitHub und LXC voraus sein. Der LXC-Build-Checkout muss
sauber bleiben; uncommitted Änderungen gehören weder in diesen Checkout noch
auf Runtime-Flächen.

## Stop-Regeln

Der Workflow bricht ab, wenn:

- der Mac-Checkout uncommitted Änderungen enthält und `sync-build` gestartet
  wird,
- der gewünschte Commit nicht auf GitHub existiert,
- `/srv/xg2g-build` dirty ist,
- ein Zielpfad kein Git-Checkout ist, obwohl einer erwartet wird,
- Staging-Health oder Binary-Hash nicht zum Deployment-Manifest passen.

In diesen Fällen wird nicht automatisch `reset`, `clean`, `stash`, Branchwechsel
oder Force-Push ausgeführt. Die Ursache wird zuerst dokumentiert und einem
konkreten Owner zugewiesen.
