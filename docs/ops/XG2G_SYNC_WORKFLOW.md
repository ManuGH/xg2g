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
| Proxmox `/root/xg2g` | OpenClaw-/Reconciliation-Checkout mit historischem Dirty-State | Nein |
| Proxmox `/root/xg2g-build` | sauberer Linux-Build-Checkout | nur explizites `sync-build` |
| LXC 110 `/srv/xg2g` | Runtime-/Staging-Umgebung | nur über Deployment |

OpenClaw läuft auf Proxmox. Es arbeitet nicht im Mac-Checkout und darf keine
Mac-Pfade als Arbeitsverzeichnis annehmen.

## Standardablauf

### Zustandsmodell

Ein Git-Commit bedeutet nicht automatisch „fertig“:

1. **Lokaler Checkpoint** – Commit auf dem Mac; kann WIP sein und wird nicht
   automatisch gepusht.
2. **Review-Kandidat** – bewusst auf einen Feature-Branch gepusht; noch keine
   Freigabe und kein Deployment.
3. **Staging-Kandidat** – nach relevanten Tests ausdrücklich für LXC 110
   ausgewählt; wird auf `:8089` getestet.
4. **Produktionsfreigabe** – ausschließlich nach Manuel-Freigabe und separatem
   Promote-Schritt auf `:8088`.

Kein Agent darf einen Zustand stillschweigend in den nächsten überführen.

Auf dem Mac:

```bash
git status --short --branch
git add <gezielte-dateien>
git commit -m "<kohaerente aenderung>"
git push -u origin <branch>
scripts/reconcile_xg2g.sh status
```

Nach dem Push kann der Linux-Build-Checkout auf genau diesen Commit gebracht
werden:

```bash
scripts/reconcile_xg2g.sh sync-build --commit <sha>
```

Dieser Schritt verändert ausschließlich `/root/xg2g-build`. Er verändert weder
den geschützten `/root/xg2g`-Checkout noch den LXC.

Für Staging folgt danach:

```bash
scripts/fast_deploy.sh
```

`fast_deploy.sh` verlangt einen sauberen Mac-Checkout und dass `HEAD` exakt dem
gepushten Remote-Branch entspricht. Es deployt ausschließlich Staging auf
`:8089`; Produktion `:8088` bleibt unberührt.

## Statusprüfung

```bash
scripts/reconcile_xg2g.sh status
```

Die Prüfung zeigt mindestens:

- Mac-Branch, Mac-Commit und Dirty-Count,
- GitHub-Commit des aktuellen Branches,
- Proxmox-Quell- und Build-Checkout inklusive Dirty-Count,
- Staging-Manifest, Health und laufenden Binary-Hash.

Abweichungen sind normal, solange sie erklärbar sind. Ein uncommitted Mac-
Stand darf gegenüber GitHub und Proxmox voraus sein. Ein dirty Proxmox- oder
LXC-Checkout darf nicht automatisch überschrieben werden.

## Stop-Regeln

Der Workflow bricht ab, wenn:

- der Mac-Checkout uncommitted Änderungen enthält und `sync-build` gestartet
  wird,
- der gewünschte Commit nicht auf GitHub existiert,
- `/root/xg2g-build` dirty ist,
- ein Zielpfad kein Git-Checkout ist, obwohl einer erwartet wird,
- Staging-Health oder Binary-Hash nicht zum Deployment-Manifest passen.

In diesen Fällen wird nicht automatisch `reset`, `clean`, `stash`, Branchwechsel
oder Force-Push ausgeführt. Die Ursache wird zuerst dokumentiert und einem
konkreten Owner zugewiesen.
