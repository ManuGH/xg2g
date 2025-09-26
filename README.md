owi2xg2g ist ein Go-basierter Microservice, der OpenWebIF-Bouquets (z. B. von VU+ Receivern) in moderne IPTV-Feeds transformiert. Er generiert automatisch:
	•	M3U-Playlists mit korrekten tvg-id, tvg-chno und tvg-logo-Attributen
	•	XMLTV-Dateien (EPG) aus OpenWebIF oder externen Quellen
	•	REST-API zur Steuerung (/api/refresh, /api/status)
	•	Dateiserver für /files/playlist.m3u und /files/guide.xml

Durch Fuzzy-Matching werden Kanäle selbst bei Namensabweichungen korrekt zugeordnet.
Die Bouquet-Reihenfolge wird in tvg-chno übernommen, Picons automatisch eingebunden.

Das Ergebnis lässt sich direkt in xTeVe, Threadfin, Plex oder Jellyfin verwenden.

Features:
	•	Go 1.22+ / Docker-ready
	•	Multi-Stage Build (Alpine)
	•	Non-root Container
	•	Vollständig konfigurierbar über Umgebungsvariablen
