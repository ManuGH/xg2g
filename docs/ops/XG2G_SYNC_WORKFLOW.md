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

## Statusprüfung

```bash
scripts/reconcile_xg2g.sh status
```

Die Prüfung zeigt mindestens:

- Mac-Branch, Mac-Commit und Dirty-Count,
- GitHub-Commit des aktuellen Branches,
- LXC-Build-Checkout inklusive Commit und Dirty-Count,
- Staging-Manifest, Health und laufenden Binary-Hash.

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
