# Bouquet Audio Audit — Premium (144 services)

Measured 2026-08-22 ~22:10–22:45 CEST against the Vu+ Uno 4K (OpenATV 8.0) via
the Enigma2 stream server. One capture per service (8 s, retried at 12 s for
every service that produced nothing).

`iOS sees` is what `ios/Xg2g/TSPacketParser.swift` classifies today. `backend`
is `isAudioStreamType` in `backend/internal/stream/ingest/ring/randomaccess.go`.
A row where iOS finds nothing decodable but the backend counts audio is a row
where today's `CriterionAudio` would report READY on a channel that stays mute.

| # | Channel | ServiceRef | Transponder | Video | Audio streams (PID/type/codec/lang) | iOS pick | Sound | Backend audio | Status | Why |
|---|---------|-----------|-------------|-------|--------------------------------------|----------|-------|---------------|--------|-----|
| 1 | ORF1 HD | `1:0:19:132F:3EF:1:C00000:0:0:0:` | 3EF:1:C00000 | H.264 | 1921/0x06/AC-3/deu • 1922/0x03/MP2/mis | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 2 | ORF2N HD | `1:0:19:1334:3EF:1:C00000:0:0:0:` | 3EF:1:C00000 | H.264 | 2921/0x06/AC-3/deu • 2922/0x03/MP2/mis | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 3 | ATV HD | `1:0:19:33AC:3EB:1:C00000:0:0:0:` | 3EB:1:C00000 | H.264 | 2281/0x03/MP2/ger | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 4 | PULS 4 Austria | `1:0:1:4E27:43A:1:C00000:0:0:0:` | 43A:1:C00000 | MPEG-2 | 1792/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 5 | ServusTV HD Oesterreich | `1:0:19:1331:3EF:1:C00000:0:0:0:` | 3EF:1:C00000 | H.264 | 3584/0x04/MP2/ger • 3585/0x04/MP2/mis • 3587/0x06/AC-3/ger | AC-3/ger | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 6 | ORF III HD | `1:0:19:33FC:3ED:1:C00000:0:0:0:` | 3ED:1:C00000 | H.264 | 3081/0x06/AC-3/deu • 3082/0x03/MP2/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 7 | PULS 24 HD | `1:0:19:14B8:407:1:C00000:0:0:0:` | 407:1:C00000 | H.264 | 1283/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 8 | RTL HD | `1:0:19:EF10:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 259/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 9 | SAT.1 HD | `1:0:19:EF74:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 259/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 10 | ProSieben HD | `1:0:19:EF75:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 515/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 11 | VOX HD | `1:0:19:EF11:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 515/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 12 | kabel eins HD | `1:0:19:EF76:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 771/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 13 | RTLZWEI HD | `1:0:19:EF15:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 1539/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 14 | ATV2 HD | `1:0:19:33A7:3EB:1:C00000:0:0:0:` | 3EB:1:C00000 | H.264 | 2231/0x03/MP2/ger | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 15 | Das Erste HD | `1:0:19:283D:3FB:1:C00000:0:0:0:` | 3FB:1:C00000 | H.264 | 5102/0x03/MP2/deu • 5103/0x03/MP2/mis • 5107/0x03/MP2/qks • 5106/0x06/AC-3/deu • 5108/0x06/unknown/und | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 16 | ZDF HD | `1:0:19:2B66:3F3:1:C00000:0:0:0:` | 3F3:1:C00000 | H.264 | 6132/0x06/unknown/und • 6120/0x03/MP2/deu • 6121/0x03/MP2/mis • 6123/0x03/MP2/mul • 6122/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 17 | 3sat HD | `1:0:19:2B8E:3F2:1:C00000:0:0:0:` | 3F2:1:C00000 | H.264 | 6520/0x03/MP2/deu • 6521/0x03/MP2/mis • 6523/0x03/MP2/mul • 6522/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 18 | arte HD | `1:0:19:283E:3FB:1:C00000:0:0:0:` | 3FB:1:C00000 | H.264 | 5112/0x03/MP2/deu • 5113/0x03/MP2/fra • 5116/0x03/MP2/mul • 5117/0x03/MP2/mis | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 19 | zdf_neo HD | `1:0:19:2B7A:3F3:1:C00000:0:0:0:` | 3F3:1:C00000 | H.264 | 6320/0x03/MP2/deu • 6321/0x03/MP2/mis • 6323/0x03/MP2/mul • 6322/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 20 | BR Fernsehen Nord HD | `1:0:19:2856:401:1:C00000:0:0:0:` | 401:1:C00000 | H.264 | 5202/0x03/MP2/deu • 5203/0x03/MP2/mis • 5207/0x03/MP2/qks • 5206/0x06/AC-3/deu • 5209/0x06/unknown/und | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 21 | SWR BW HD | `1:0:19:283F:3FB:1:C00000:0:0:0:` | 3FB:1:C00000 | H.264 | 5122/0x03/MP2/deu • 5123/0x03/MP2/mis • 5127/0x03/MP2/qks • 5126/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 22 | WDR HD Köln | `1:0:19:6EAC:3FD:1:C00000:0:0:0:` | 3FD:1:C00000 | H.264 | 102/0x03/MP2/deu • 103/0x03/MP2/mis • 107/0x03/MP2/qks • 106/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 23 | NDR FS HH HD | `1:0:19:2859:401:1:C00000:0:0:0:` | 401:1:C00000 | H.264 | 5222/0x03/MP2/deu • 5223/0x03/MP2/mis • 5227/0x03/MP2/qks • 5226/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 24 | hr-fernsehen HD | `1:0:19:2873:425:1:C00000:0:0:0:` | 425:1:C00000 | H.264 | 5352/0x03/MP2/deu • 5353/0x03/MP2/mis • 5357/0x03/MP2/qks • 5356/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 25 | rbb Berlin HD | `1:0:19:286F:425:1:C00000:0:0:0:` | 425:1:C00000 | H.264 | 5312/0x03/MP2/deu • 5313/0x03/MP2/mis • 5317/0x03/MP2/qks • 5316/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 26 | SIXX HD | `1:0:19:EF77:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 1027/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 27 | ONE HD | `1:0:19:2888:40F:1:C00000:0:0:0:` | 40F:1:C00000 | H.264 | 5412/0x03/MP2/deu • 5413/0x03/MP2/mis • 5417/0x03/MP2/qks • 5416/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 28 | NITRO HD | `1:0:19:2EAF:411:1:C00000:0:0:0:` | 411:1:C00000 | H.264 | 510/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 29 | TELE 5 HD | `1:0:19:1519:455:1:C00000:0:0:0:` | 455:1:C00000 | H.264 | 515/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 30 | Pro7 MAXX HD | `1:0:19:EF78:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 1283/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 31 | DMAX HD | `1:0:19:151A:455:1:C00000:0:0:0:` | 455:1:C00000 | H.264 | 771/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 32 | RTLup HD | `1:0:19:EF16:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 1795/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 33 | VOXup HD | `1:0:19:EF17:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 2051/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 34 | Kabel Eins Doku HD | `1:0:19:EF79:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 1539/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 35 | tagesschau24 HD | `1:0:19:2887:40F:1:C00000:0:0:0:` | 40F:1:C00000 | H.264 | 5402/0x03/MP2/deu • 5403/0x03/MP2/mis • 5407/0x03/MP2/qks • 5406/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 36 | TLC HD | `1:0:19:2774:409:1:C00000:0:0:0:` | 409:1:C00000 | H.264 | 259/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 37 | ntv HD | `1:0:19:EF14:421:1:C00000:0:0:0:` | 421:1:C00000 | H.264 | 1283/0x06/AC-3/deu • 1284/0x06/AC-3/mul | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 38 | ZDFinfo HD | `1:0:19:2BA2:3F2:1:C00000:0:0:0:` | 3F2:1:C00000 | H.264 | 6720/0x03/MP2/deu • 6721/0x03/MP2/mis • 6723/0x03/MP2/mul • 6722/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 39 | SR Fernsehen HD | `1:0:19:288A:40F:1:C00000:0:0:0:` | 40F:1:C00000 | H.264 | 5432/0x03/MP2/deu • 5433/0x03/MP2/mis • 5437/0x03/MP2/qks • 5436/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 40 | MTV HD | `1:0:19:6FEE:436:1:C00000:0:0:0:` | 436:1:C00000 | H.264 | 3042/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 41 | SUPER RTL HD | `1:0:19:2E9B:411:1:C00000:0:0:0:` | 411:1:C00000 | H.264 | 310/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 42 | Comedy Central HD | `1:0:19:6FEC:436:1:C00000:0:0:0:` | 436:1:C00000 | H.264 | 3023/0x04/MP2/deu • 3024/0x04/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 43 | Disney Channel HD | `1:0:19:157C:41F:1:C00000:0:0:0:` | 41F:1:C00000 | H.264 | 259/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 44 | SPORT1 HD | `1:0:19:1581:41F:1:C00000:0:0:0:` | 41F:1:C00000 | H.264 | 1539/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 45 | ORF SPORT+ HD | `1:0:19:33FD:3ED:1:C00000:0:0:0:` | 3ED:1:C00000 | H.264 | 3091/0x06/AC-3/deu • 3092/0x03/MP2/deu • 3093/0x03/MP2/mul • 3094/0x03/MP2/qks | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 46 | Eurosport 1 HD | `1:0:19:30D6:413:1:C00000:0:0:0:` | 413:1:C00000 | H.264 | 771/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 47 | SPORTDIGITAL FUSSBALL HD | `1:0:19:10CC:418:1:C00000:0:0:0:` | 418:1:C00000 | H.264 | 256/0x03/MP2/deu • 257/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 48 | Hustler TV | `1:0:16:100B:451:35:C00000:0:0:0:` | 451:35:C00000 | H.264 | 147/0x04/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 49 | Deluxe Rap HD | `1:0:19:296F:45A:1:C00000:0:0:0:` | 45A:1:C00000 | H.264 | 2048/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 50 | Deluxe Music HD | `1:0:19:157F:41F:1:C00000:0:0:0:` | 41F:1:C00000 | H.264 | 1027/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 51 | WELT HD | `1:0:19:5274:41D:1:C00000:0:0:0:` | 41D:1:C00000 | H.264 | 771/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 52 | RTL UHD | `1:0:1F:307A:3F5:1:C00000:0:0:0:` | 3F5:1:C00000 | HEVC | 2816/0x06/E-AC-3/deu | E-AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 53 | Pro7Sat.1 UHD | `1:0:1F:183D:40B:1:C00000:0:0:0:` | 40B:1:C00000 | HEVC | 771/0x06/E-AC-3/deu | E-AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 54 | UHD1 by ASTRA / HD+ | `1:0:1F:2:40B:1:C00000:0:0:0:` | 40B:1:C00000 | HEVC | 102/0x06/E-AC-3/deu | E-AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 55 | Sky Showcase | `1:0:19:8E:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 1283/0x06/AC-3/deu • 1284/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 56 | Sky One | `1:0:19:93:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 515/0x06/AC-3/deu • 516/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 57 | Universal TV | `1:0:19:65:10:85:C00000:0:0:0:` | 10:85:C00000 | H.264 | 1539/0x06/AC-3/deu • 1540/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 58 | 13th Street | `1:0:19:7F:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 771/0x06/AC-3/deu • 772/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 59 | Sky Krimi | `1:0:19:17:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 1539/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 60 | Sky Atlantic | `1:0:19:6E:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 1283/0x06/AC-3/deu • 1284/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 61 | Sky Sci-Fi | `1:0:19:7E:C:85:C00000:0:0:0:` | C:85:C00000 | H.264 | 515/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 62 | Sky Replay | `1:0:19:7C:C:85:C00000:0:0:0:` | C:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 63 | Sky Crime | `1:0:19:D:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 1539/0x06/AC-3/deu • 1540/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 64 | Sky Documentaries | `1:0:19:70:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 515/0x06/AC-3/deu • 516/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 65 | Sky Nature | `1:0:19:76:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 515/0x06/AC-3/deu • 516/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 66 | Warner TV Serie | `1:0:19:7B:10:85:C00000:0:0:0:` | 10:85:C00000 | H.264 | 1283/0x06/AC-3/deu • 1284/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 67 | Warner TV Comedy | `1:0:19:88:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 3843/0x06/AC-3/deu • 3844/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 68 | Romance TV | `1:0:19:206:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 3072/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 69 | HISTORY Channel | `1:0:19:71:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 771/0x06/AC-3/deu • 772/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 70 | NatGeo | `1:0:19:82:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 1027/0x06/AC-3/deu • 1028/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 71 | Crime + Investigation | `1:0:16:192:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 2048/0x03/MP2/deu • 2049/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 72 | Motorvision+ | `1:0:16:A8:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 1024/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 73 | Sky Sport News | `1:0:19:6C:C:85:C00000:0:0:0:` | C:85:C00000 | H.264 | 1027/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 74 | Sky Sport Top Event | `1:0:19:81:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 770/0x06/AC-3/qae • 772/0x06/AC-3/qaf | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 75 | Sky Sport Bundesliga | `1:0:19:69:C:85:C00000:0:0:0:` | C:85:C00000 | H.264 | 258/0x06/AC-3/qab • 260/0x06/AC-3/qac | AC-3/qab | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 76 | Sky Sport F1 | `1:0:19:11:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 1794/0x06/AC-3/qae • 1796/0x06/AC-3/qaf | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 77 | Sky Sport Premier League | `1:0:19:91:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 3842/0x06/AC-3/qae • 3844/0x06/AC-3/qaf | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 78 | Sky Sport Mix | `1:0:19:8D:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 258/0x06/AC-3/qae | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 79 | Sky Sport Tennis | `1:0:19:72:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 1026/0x06/AC-3/qae • 1028/0x06/AC-3/qaf | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 80 | Sky Sport Golf | `1:0:19:90:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 2050/0x06/AC-3/qae | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 81 | Sky Sport Bundesliga 1 | `1:0:19:10B:6:85:C00000:0:0:0:` | 6:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 82 | Sky Sport Bundesliga 2 | `1:0:19:115:2:85:C00000:0:0:0:` | 2:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 83 | Sky Sport Bundesliga 3 | `1:0:19:11F:C:85:C00000:0:0:0:` | C:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 84 | Sky Sport Bundesliga 4 | `1:0:19:129:4:85:C00000:0:0:0:` | 4:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 85 | Sky Sport Bundesliga 5 | `1:0:19:133:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 1026/0x06/AC-3/qab • 1028/0x06/AC-3/qac | AC-3/qab | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 86 | Sky Sport Bundesliga 8 | `1:0:19:151:10:85:C00000:0:0:0:` | 10:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 87 | Sky Sport Bundesliga 9 | `1:0:19:101:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 88 | Sky Sport Bundesliga 10 | `1:0:19:10F:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 89 | Sky Sport 1 | `1:0:19:10C:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 2306/0x06/AC-3/qae | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 90 | Sky Sport 2 | `1:0:19:116:2:85:C00000:0:0:0:` | 2:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 91 | Sky Sport 3 | `1:0:19:120:C:85:C00000:0:0:0:` | C:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 92 | Sky Sport 4 | `1:0:19:12A:4:85:C00000:0:0:0:` | 4:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 93 | Sky Sport 5 | `1:0:19:134:4:85:C00000:0:0:0:` | 4:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 94 | Sky Sport 6 | `1:0:19:13E:6:85:C00000:0:0:0:` | 6:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 95 | Sky Sport 7 | `1:0:19:148:C:85:C00000:0:0:0:` | C:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 96 | Sky Sport 8 | `1:0:19:152:10:85:C00000:0:0:0:` | 10:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 97 | Sky Sport 9 | `1:0:19:102:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 98 | Sky Sport 10 | `1:0:19:10D:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 99 | Sky Sport Austria 1 | `1:0:19:8F:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 3330/0x06/AC-3/qae | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 100 | Sky Sport Austria 2 | `1:0:19:149:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 3074/0x06/AC-3/qae • 3076/0x06/AC-3/qaf | AC-3/qae | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 101 | Sky Sport Austria 3 | `1:0:19:153:2:85:C00000:0:0:0:` | 2:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 102 | Sky Sport Austria 4 | `1:0:19:103:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 103 | Sky Sport Austria 5 | `1:0:19:146:B:85:C00000:0:0:0:` | B:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 104 | Sky Sport Austria 6 | `1:0:19:156:10:85:C00000:0:0:0:` | 10:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 105 | Sky Sport Austria 7 | `1:0:19:105:C:85:C00000:0:0:0:` | C:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 106 | Sky Sport Austria 8 | `1:0:19:106:6:85:C00000:0:0:0:` | 6:85:C00000 | — | — | — | — | no | `NO_BROADCAST/OFF_AIR` | no data; another service on the same transponder was reached, so it is receivable |
| 107 | DAZN 1 | `1:0:19:84:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 2051/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 108 | DAZN 2 | `1:0:19:7A:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 4355/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 109 | Sky Cinema Premiere | `1:0:19:83:6:85:C00000:0:0:0:` | 6:85:C00000 | H.264 | 1283/0x06/AC-3/deu • 1284/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 110 | Sky Cinema Blockbuster | `1:0:19:6F:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 259/0x06/AC-3/deu • 260/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 111 | Sky Cinema Action | `1:0:19:74:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 771/0x06/AC-3/deu • 772/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 112 | Sky Cinema Feelgood | `1:0:19:8B:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 771/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 113 | Sky Cinema Classics | `1:0:19:6B:C:85:C00000:0:0:0:` | C:85:C00000 | H.264 | 771/0x06/AC-3/deu • 772/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 114 | Warner TV Film | `1:0:19:8C:4:85:C00000:0:0:0:` | 4:85:C00000 | H.264 | 2819/0x06/AC-3/deu • 2820/0x06/AC-3/eng | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 2 decodable track(s) |
| 115 | Beate Uhse | `1:0:19:85:2:85:C00000:0:0:0:` | 2:85:C00000 | H.264 | 2307/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 116 | Cartoonito | `1:0:16:1C:C:85:C00000:0:0:0:` | C:85:C00000 | H.264 | 1280/0x03/MP2/deu • 1281/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 117 | Nick.Jr. | `1:0:1:6FFB:436:1:C00000:0:0:0:` | 436:1:C00000 | H.264 | 3112/0x04/MP2/deu • 3113/0x04/MP2/eng • 3117/0x04/MP2/tur | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 118 | Nick/Comedy Central+1 | `1:0:1:7008:436:1:C00000:0:0:0:` | 436:1:C00000 | H.264 | 4102/0x04/MP2/ger • 4103/0x04/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 119 | Nicktoons | `1:0:16:1B:D:85:C00000:0:0:0:` | D:85:C00000 | H.264 | 1536/0x03/MP2/deu • 1537/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 120 | Cartoon Network | `1:0:16:194:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 1792/0x03/MP2/deu • 1793/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 121 | Jukebox | `1:0:16:191:B:85:C00000:0:0:0:` | B:85:C00000 | H.264 | 4608/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 122 | eSportsOne | `1:0:19:10CE:418:1:C00000:0:0:0:` | 418:1:C00000 | H.264 | 768/0x03/MP2/deu • 769/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 123 | sportdigital EDGE | `1:0:19:10CF:418:1:C00000:0:0:0:` | 418:1:C00000 | H.264 | 1024/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 124 | Deluxe Dance by Kontor HD | `1:0:19:296B:45A:1:C00000:0:0:0:` | 45A:1:C00000 | H.264 | 1024/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 125 | Deluxe Flashback HD | `1:0:19:296C:45A:1:C00000:0:0:0:` | 45A:1:C00000 | H.264 | 1280/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 126 | Schlager Deluxe | `1:0:1:23:F:85:C00000:0:0:0:` | F:85:C00000 | MPEG-2 | 512/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 127 | ORF HITRADIO OE3 VISUAL | `1:0:19:1335:3EF:1:C00000:0:0:0:` | 3EF:1:C00000 | H.264 | 2971/0x06/AC-3/ger | AC-3/ger | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 128 | EUROSPORT 2 | `1:0:1:76A5:41E:1:C00000:0:0:0:` | 41E:1:C00000 | MPEG-2 | 136/0x04/MP2/spa • 145/0x04/MP2/dos • 208/0xC0/unknown/und • 222/0xC0/unknown/und • 213/0xC1/unknown/und • 307/0xC1/unknown/und • 356/0xC1/unknown/und • 392/0xC1/unknown/und • 309/0xC0/unknown/und • 628/0xC1/unknown/und • 888/0xC1/unknown/und | — | — | yes | `SCRAMBLED_NO_CLEAR` | 2020 audio packets, none clear |
| 129 | SAT.1 GOLD HD | `1:0:19:EF7A:3F9:1:C00000:0:0:0:` | 3F9:1:C00000 | H.264 | 1795/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 130 | DORCEL TV | `1:0:19:24CF:43C:1:C00000:0:0:0:` | 43C:1:C00000 | H.264 | 2321/0x06/AC-3/fra | AC-3/fra | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 131 | DORCEL XXX | `1:0:19:24CE:43C:1:C00000:0:0:0:` | 43C:1:C00000 | H.264 | 2221/0x06/AC-3/fra | AC-3/fra | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 132 | phoenix HD | `1:0:19:285B:401:1:C00000:0:0:0:` | 401:1:C00000 | H.264 | 5262/0x03/MP2/deu • 5263/0x03/MP2/mul | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 133 | KiKA HD | `1:0:19:2B98:3F2:1:C00000:0:0:0:` | 3F2:1:C00000 | H.264 | 6620/0x03/MP2/deu • 6622/0x06/AC-3/ger • 6623/0x03/MP2/mul • 6621/0x03/MP2/mis | AC-3/ger | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 134 | MDR Sachsen HD | `1:0:19:2870:425:1:C00000:0:0:0:` | 425:1:C00000 | H.264 | 5332/0x03/MP2/deu • 5333/0x03/MP2/mis • 5337/0x03/MP2/qks • 5336/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 135 | Sportdigital1+ | `1:0:19:10CD:418:1:C00000:0:0:0:` | 418:1:C00000 | H.264 | 512/0x03/MP2/deu • 513/0x03/MP2/eng | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 136 | sonnenklar.TV HD | `1:0:19:1518:455:1:C00000:0:0:0:` | 455:1:C00000 | H.264 | 259/0x06/AC-3/deu | AC-3/deu | YES | yes | `VERIFIED_SUPPORTED` | 1 decodable track(s) |
| 137 | One Terra HD | `1:0:19:2968:45A:1:C00000:0:0:0:` | 45A:1:C00000 | H.264 | 256/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 138 | oe24.TV HD | `1:0:19:3402:3ED:1:C00000:0:0:0:` | 3ED:1:C00000 | H.264 | 3141/0x03/MP2/ger | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 139 | krone.tv | `1:0:16:3401:3ED:1:C00000:0:0:0:` | 3ED:1:C00000 | H.264 | 3131/0x03/MP2/ger | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 140 | Just Fishing HD | `1:0:19:29CC:45C:1:C00000:0:0:0:` | 45C:1:C00000 | H.264 | 256/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 141 | Just Cooking HD | `1:0:19:296D:45A:1:C00000:0:0:0:` | 45A:1:C00000 | H.264 | 1536/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 142 | Crime Time HD | `1:0:19:29D0:45C:1:C00000:0:0:0:` | 45C:1:C00000 | H.264 | 1280/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 143 | Bibel TV HD | `1:0:19:33A8:3EB:1:C00000:0:0:0:` | 3EB:1:C00000 | H.264 | 2241/0x03/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
| 144 | Aristo TV | `1:0:16:33FF:3ED:1:C00000:0:0:0:` | 3ED:1:C00000 | H.264 | 3111/0x04/MP2/deu | — | SILENT | yes | `VERIFIED_UNSUPPORTED_AUDIO` | only MP2 offered |
