// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Built-in demonstration environment for evaluating xg2g without physical DVB/Enigma2 hardware.
///
/// Uses Apple's official developer HLS test streams (`devstreaming-cdn.apple.com`) and synthetic
/// open-showcase channel metadata so reviewers and prospective users can explore Live TV, EPG,
/// DVR recordings, and timer scheduling immediately without pairing against a private receiver.
enum DemoServer {

    /// Official Apple Developer fMP4 HLS stream (BipBop Advanced).
    static let appleBipBopHLSURL = URL(
        string: "https://devstreaming-cdn.apple.com/videos/streaming/examples/img_bipbop_adv_example_fmp4/master.m3u8"
    )!

    /// Official Apple Developer Dolby Vision / Atmos HLS stream.
    static let appleAdvHLSURL = URL(
        string: "https://devstreaming-cdn.apple.com/videos/streaming/examples/adv_dv_atmos/main.m3u8"
    )!

    /// Canonical `ServerAddress` used while demo mode is active.
    ///
    /// Anchored at `https://devstreaming-cdn.apple.com/` so strict origin containment checks in
    /// `RecordingPlayback.resolve` and `MediaEndpoints` naturally accept the official Apple HLS stream URLs.
    static let demoServerAddress: ServerAddress = {
        guard let parsed = try? ServerAddressParser.parseTrusted("https://devstreaming-cdn.apple.com/") else {
            preconditionFailure("Failed to construct static demo ServerAddress")
        }
        return parsed
    }()

    /// Human-readable server label displayed in the UI during demo mode.
    static let displayAddress = "demo.xg2g.local"

    /// Checks whether user-entered setup text requests the built-in demo mode.
    static func isDemoInput(_ raw: String) -> Bool {
        let cleaned = raw
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
            .replacingOccurrences(of: "https://", with: "")
            .replacingOccurrences(of: "http://", with: "")
            .trimmingCharacters(in: CharacterSet(charactersIn: "/"))

        return cleaned == "demo"
            || cleaned == "demo.xg2g.local"
            || cleaned == "demo.xg2g.example"
            || cleaned == "devstreaming-cdn.apple.com"
    }

    /// Checks whether a configured `ServerAddress` represents the built-in demo mode.
    static func isDemoAddress(_ address: ServerAddress?) -> Bool {
        guard let host = address?.origin.host.lowercased() else { return false }
        return host == "devstreaming-cdn.apple.com"
            || host == "demo.xg2g.local"
            || host == "demo.xg2g.example"
    }

    /// Returns the official Apple HLS test stream URL for a demo channel or session.
    static func demoHLSStreamURL(forServiceRef serviceRef: String? = nil) -> URL {
        if let serviceRef, serviceRef.contains("102") || serviceRef.contains("104") {
            return appleAdvHLSURL
        }
        return appleBipBopHLSURL
    }
}

/// Thread-safe in-memory state backing `DemoAPIClient` so creating/deleting timers and recordings
/// works interactively during a demo session.
actor DemoStateStore {
    static let shared = DemoStateStore()

    private var recordings: [Xg2gContract.RecordingItem]
    private var timers: [Xg2gContract.Timer]

    init() {
        let now = Int64(Date().timeIntervalSince1970)
        self.recordings = [
            Xg2gContract.RecordingItem(
                status: .completed,
                beginUnixSeconds: now - 86_400,
                description: "Dokumentation über Raumfahrt, Satellitenkommunikation und moderne Orbitalstationen.",
                durationSeconds: 2700,
                filename: "/media/hdd/movie/demo_orbit_docu.ts",
                length: "45:00",
                localWritable: false,
                recordingId: "rec_demo_orbit",
                resume: Xg2gContract.ResumeSummary(posSeconds: 420, durationSeconds: 2700, finished: false),
                serviceRef: "1:0:19:103:1:1:DEMO000:0:0:0:",
                title: "Dokumentation: Expedition Orbit & Space"
            ),
            Xg2gContract.RecordingItem(
                status: .completed,
                beginUnixSeconds: now - 172_800,
                description: "Spielfilm (NL 2024). Open-Source Science-Fiction Kurzfilmprojekt über Robotik und visuelle Effekte. Regie: Ian Hubert.",
                durationSeconds: 4320,
                filename: "/media/hdd/movie/demo_open_cinema.ts",
                length: "72:00",
                localWritable: false,
                recordingId: "rec_demo_cinema",
                resume: nil,
                serviceRef: "1:0:19:105:1:1:DEMO000:0:0:0:",
                title: "Spielfilm: Open Source Cinema Showcase (2024)"
            ),
            Xg2gContract.RecordingItem(
                status: .completed,
                beginUnixSeconds: now - 259_200,
                description: "Technische Reportage über 50fps Hardware-Deinterlacing, Apple Metal Shaders und modernes HLS-fMP4-Streaming.",
                durationSeconds: 1800,
                filename: "/media/hdd/movie/demo_broadcast_tech.ts",
                length: "30:00",
                localWritable: false,
                recordingId: "rec_demo_tech",
                resume: nil,
                serviceRef: "1:0:19:101:1:1:DEMO000:0:0:0:",
                title: "Reportage: Broadcast Engineering & Metal Video"
            )
        ]

        self.timers = [
            Xg2gContract.Timer(
                begin: now + 3600,
                end: now + 7200,
                name: "Dokumentation: Deep Ocean Explorers",
                serviceRef: "1:0:19:104:1:1:DEMO000:0:0:0:",
                state: .scheduled,
                timerId: "timer_demo_1",
                description: "Naturdokumentation über Tiefsee-Ökosysteme in 4K HDR.",
                serviceName: "Earth & Nature 4K"
            ),
            Xg2gContract.Timer(
                begin: now + 10_800,
                end: now + 14_400,
                name: "Spielfilm: Meridian – Open Movie Project (2025)",
                serviceRef: "1:0:19:105:1:1:DEMO000:0:0:0:",
                state: .scheduled,
                timerId: "timer_demo_2",
                description: "Mystery-Spielfilm aus dem Open-Content-Programm.",
                serviceName: "Open Cinema Classics"
            )
        ]
    }

    func listRecordings() -> [Xg2gContract.RecordingItem] {
        recordings
    }

    func deleteRecording(id: String) {
        recordings.removeAll { $0.recordingId == id || $0.filename == id }
    }

    func updateResume(id: String, position: Double, total: Double?, finished: Bool?) {
        guard let idx = recordings.firstIndex(where: { $0.recordingId == id || $0.filename == id }) else { return }
        let current = recordings[idx]
        let summary = Xg2gContract.ResumeSummary(
            posSeconds: Int64(position),
            durationSeconds: total.map { Int64($0) } ?? current.durationSeconds,
            finished: finished ?? false,
            updatedAt: Date()
        )
        recordings[idx] = Xg2gContract.RecordingItem(
            status: current.status,
            beginUnixSeconds: current.beginUnixSeconds,
            description: current.description,
            durationSeconds: current.durationSeconds,
            filename: current.filename,
            length: current.length,
            localWritable: current.localWritable,
            recordingId: current.recordingId,
            resume: summary,
            serviceRef: current.serviceRef,
            title: current.title
        )
    }

    func listTimers() -> [Xg2gContract.Timer] {
        timers
    }

    func addTimer(from request: Xg2gContract.TimerCreateRequest) {
        let channelName = DemoCatalog.services.first(where: { $0.serviceRef == request.serviceRef })?.name ?? "xg2g Showcase HD"
        let newTimer = Xg2gContract.Timer(
            begin: request.begin,
            end: request.end,
            name: request.name,
            serviceRef: request.serviceRef,
            state: .scheduled,
            timerId: "timer_demo_\(UUID().uuidString.prefix(8))",
            createdAt: Date(),
            description: request.description,
            serviceName: channelName,
            updatedAt: Date()
        )
        timers.append(newTimer)
    }

    func deleteTimer(id: String) {
        timers.removeAll { $0.timerId == id }
    }
}

/// Synthetic open-showcase catalog for Demo Mode.
private enum DemoCatalog {

    static let bouquets: [Xg2gContract.Bouquet] = [
        Xg2gContract.Bouquet(name: "Open Broadcast HD", services: 4),
        Xg2gContract.Bouquet(name: "Science & Space", services: 2),
        Xg2gContract.Bouquet(name: "Cinema & Arts", services: 2)
    ]

    static let services: [Xg2gContract.Service] = [
        Xg2gContract.Service(
            codec: "h264",
            enabled: true,
            group: "Open Broadcast HD",
            id: "demo_101",
            logoUrl: nil,
            name: "xg2g Showcase HD",
            number: "101",
            resolution: "1920x1080",
            serviceRef: "1:0:19:101:1:1:DEMO000:0:0:0:"
        ),
        Xg2gContract.Service(
            codec: "hevc",
            enabled: true,
            group: "Open Broadcast HD",
            id: "demo_102",
            logoUrl: nil,
            name: "Apple Developer Stream",
            number: "102",
            resolution: "3840x2160",
            serviceRef: "1:0:19:102:1:1:DEMO000:0:0:0:"
        ),
        Xg2gContract.Service(
            codec: "h264",
            enabled: true,
            group: "Science & Space",
            id: "demo_103",
            logoUrl: nil,
            name: "Cosmos & Orbit TV",
            number: "103",
            resolution: "1920x1080",
            serviceRef: "1:0:19:103:1:1:DEMO000:0:0:0:"
        ),
        Xg2gContract.Service(
            codec: "hevc",
            enabled: true,
            group: "Science & Space",
            id: "demo_104",
            logoUrl: nil,
            name: "Earth & Nature 4K",
            number: "104",
            resolution: "3840x2160",
            serviceRef: "1:0:19:104:1:1:DEMO000:0:0:0:"
        ),
        Xg2gContract.Service(
            codec: "h264",
            enabled: true,
            group: "Cinema & Arts",
            id: "demo_105",
            logoUrl: nil,
            name: "Open Cinema Classics",
            number: "105",
            resolution: "1920x1080",
            serviceRef: "1:0:19:105:1:1:DEMO000:0:0:0:"
        ),
        Xg2gContract.Service(
            codec: "h264",
            enabled: true,
            group: "Open Broadcast HD",
            id: "demo_106",
            logoUrl: nil,
            name: "Arena Outdoors & Sport",
            number: "106",
            resolution: "1920x1080",
            serviceRef: "1:0:19:106:1:1:DEMO000:0:0:0:"
        )
    ]

    struct ProgramTemplate {
        let title: String
        let desc: String
        let genre: String
    }

    static let templatesByServiceRef: [String: [ProgramTemplate]] = [
        "1:0:19:101:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Nachrichten: Digital Broadcast Journal",
                desc: "Aktuelle Nachrichten und Analysen rund um Medientechnik, Open-Source-Architektur und Heimnetzwerk-Streaming.",
                genre: "Nachrichten"
            ),
            ProgramTemplate(
                title: "Dokumentation: Architektur moderner Receiver",
                desc: "Dokumentarfilm über DVB-S2X/C/T2 Tuner-Management, DPoP-Sicherheit und verlustfreie Signalverarbeitung.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Talkshow: Studio Live – Technik im Fokus",
                desc: "Unterhaltungsshow und Gesprächsrunde über Heimkino, Audio-Synchronisation und Bildwiederholraten.",
                genre: "Unterhaltung"
            ),
            ProgramTemplate(
                title: "Spielfilm: Signal Horizon (2024)",
                desc: "Spannender Abenteuerfilm über eine transatlantische Funk- und Satellitenmission. Regie: Alex Rivera (2024).",
                genre: "Spielfilme"
            )
        ],
        "1:0:19:102:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Dokumentation: Mastering HLS & fMP4 Streaming",
                desc: "Dokumentation über Adaptive Bitrate Streaming, Dolby Atmos Audio und Hardware-Decoding auf Apple-Plattformen.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Serie: Swift Concurrency Chronicles – Staffel 2, Folge 4",
                desc: "Dramaserie über deterministische Zustandsmaschinen, Actor-Isolation und flüssige 120Hz ProMotion-Oberflächen.",
                genre: "Serien"
            ),
            ProgramTemplate(
                title: "Nachrichten: Developer Keynote Highlights",
                desc: "Nachrichtenmagazin mit den wichtigsten Neuerungen für iOS, iPadOS und tvOS.",
                genre: "Nachrichten"
            ),
            ProgramTemplate(
                title: "Spielfilm: Cupertino Nights (2023)",
                desc: "Kinofilm über die Entstehung moderner Grafik-Pipelines. Hauptdarsteller: Sam Carter (USA 2023).",
                genre: "Spielfilme"
            )
        ],
        "1:0:19:103:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Dokumentation: Expedition Orbit – Die ISS Live",
                desc: "Wissenschaftliche Dokumentation über Forschung in der Schwerelosigkeit und Erdbeobachtung aus dem Orbit.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Dokumentation: Deep Space Telescopes",
                desc: "Reportage über Infrarot-Astronomie, Exoplaneten und die Entstehung erster Galaxien.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Serie: Mars Base Alpha – S01E03",
                desc: "Wissenschaftliche Serie über zukünftige Habitate und Lebenserhaltungssysteme auf dem Mars.",
                genre: "Serien"
            ),
            ProgramTemplate(
                title: "Dokumentation: Aurora Borealis in 4K",
                desc: "Naturdokumentation über Sonnenwinde und Polarlichter über dem Nordpolarmeer.",
                genre: "Doku & Wissen"
            )
        ],
        "1:0:19:104:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Dokumentation: Faszination Erde – Alpenkette",
                desc: "Naturdokumentation über Hochgebirgsregionen, Gletscher und alpine Wildtiere im Wandel der Jahreszeiten.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Dokumentation: Ozeane der Welt – Korallenriffe",
                desc: "Tierdokumentation über maritime Biodiversität und Meeresströmungen im Pazifik.",
                genre: "Doku & Wissen"
            ),
            ProgramTemplate(
                title: "Kinderfilm: Entdecker der Wildnis",
                desc: "Lehrreicher Animationsfilm und Kinderserie über Wälder, Flüsse und heimische Tierarten.",
                genre: "Kinder"
            ),
            ProgramTemplate(
                title: "Dokumentation: Regenwälder des Amazonas",
                desc: "Expedition in das artenreichste Ökosystem der Erde.",
                genre: "Doku & Wissen"
            )
        ],
        "1:0:19:105:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Spielfilm: Tears of Steel – Open Movie (2024)",
                desc: "Science-Fiction-Film der Blender Foundation in Amsterdam. Regie: Ian Hubert (2024).",
                genre: "Spielfilme"
            ),
            ProgramTemplate(
                title: "Spielfilm: Sintel – Die Suche (2022)",
                desc: "Preisgekrönter Fantasyfilm und Animationsfilm aus dem Open-Movie-Projekt (2022).",
                genre: "Spielfilme"
            ),
            ProgramTemplate(
                title: "Serie: Caminandes – Staffel 1, Folge 2",
                desc: "Charmante Animationsserie über Abenteuer in Patagonien.",
                genre: "Serien"
            ),
            ProgramTemplate(
                title: "Spielfilm: Cosmos Laundromat (2023)",
                desc: "Surrealer Kurzfilm und Tragikomödie unter Creative-Commons-Lizenz (2023).",
                genre: "Spielfilme"
            )
        ],
        "1:0:19:106:1:1:DEMO000:0:0:0:": [
            ProgramTemplate(
                title: "Live: Radsport – Alpine Mountain Classic",
                desc: "Sport-Übertragung der hochalpinen Etappe mit Live-Telemetrie und Höhenprofil.",
                genre: "Sport"
            ),
            ProgramTemplate(
                title: "Wintersport: Freeride World Showcase",
                desc: "Spektakuläre Abfahrten im Tiefschnee und Hintergrundberichte zur Ausrüstung.",
                genre: "Sport"
            ),
            ProgramTemplate(
                title: "Motorsport: Electric Endurance Cup",
                desc: "Langstreckenrennen und Ingenieurs-Analyse moderner Elektro-Prototypen.",
                genre: "Sport"
            ),
            ProgramTemplate(
                title: "Sportstudio: Highlights der Woche",
                desc: "Zusammenfassung der spannendsten Outdoor- und Ausdauerwettbewerbe.",
                genre: "Sport"
            )
        ]
    ]

    static func generateEpgItems(now: Date = Date()) -> [Xg2gContract.EpgItem] {
        // Align slot grid to 45-minute boundaries so Now/Next always has a currently running show
        let slotDuration: Int64 = 45 * 60
        let nowSec = Int64(now.timeIntervalSince1970)
        let currentSlotStart = (nowSec / slotDuration) * slotDuration

        var items: [Xg2gContract.EpgItem] = []
        for (channelIdx, service) in services.enumerated() {
            guard let sref = service.serviceRef else { continue }
            let templates = templatesByServiceRef[sref] ?? []
            guard !templates.isEmpty else { continue }

            // Offset start per channel slightly so progress bars look varied and realistic
            let channelOffset = Int64((channelIdx % 3) * 5 * 60)

            for slotIndex in -4..<36 {
                let startSec = currentSlotStart + Int64(slotIndex) * slotDuration - channelOffset
                let endSec = startSec + slotDuration
                let template = templates[abs(slotIndex + channelIdx) % templates.count]

                items.append(
                    Xg2gContract.EpgItem(
                        desc: template.desc,
                        duration: slotDuration,
                        end: endSec,
                        genre: template.genre,
                        id: "epg_\(service.id ?? "ch")_\(slotIndex)",
                        serviceRef: sref,
                        start: startSec,
                        title: template.title
                    )
                )
            }
        }
        return items
    }

    static func generateNowNext(for serviceRefs: [String], now: Date = Date()) -> Xg2gContract.NowNextResponse {
        let allItems = generateEpgItems(now: now)
        let nowSec = Int64(now.timeIntervalSince1970)
        var responseItems: [Xg2gContract.NowNextItem] = []

        for sref in serviceRefs {
            let channelShows = allItems
                .filter { $0.serviceRef == sref }
                .sorted { ($0.start ?? 0) < ($1.start ?? 0) }

            let currentShow = channelShows.first { ($0.start ?? 0) <= nowSec && ($0.end ?? 0) > nowSec }
                ?? channelShows.first
            let nextShow = channelShows.first { ($0.start ?? 0) >= (currentShow?.end ?? nowSec) }

            let nowEntry = currentShow.map {
                Xg2gContract.NowNextEntry(
                    end: Int($0.end ?? (nowSec + 1800)),
                    start: Int($0.start ?? nowSec),
                    title: $0.title ?? "Showcase Live",
                    desc: $0.desc,
                    genre: $0.genre
                )
            }
            let nextEntry = nextShow.map {
                Xg2gContract.NowNextEntry(
                    end: Int($0.end ?? (nowSec + 3600)),
                    start: Int($0.start ?? (nowSec + 1800)),
                    title: $0.title ?? "Up Next",
                    desc: $0.desc,
                    genre: $0.genre
                )
            }

            responseItems.append(
                Xg2gContract.NowNextItem(
                    serviceRef: sref,
                    next: nextEntry,
                    now: nowEntry
                )
            )
        }

        return Xg2gContract.NowNextResponse(items: responseItems)
    }
}

/// Local `APIClient` implementation that serves the built-in Demo Mode without external receiver hardware.
struct DemoAPIClient: APIClient {

    private let encoder: JSONEncoder = {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .iso8601
        return enc
    }()

    private let decoder: JSONDecoder = .xg2g

    func send<Response>(_ request: APIRequest<Response>) async throws -> Response where Response: Decodable, Response: Sendable {
        if Response.self == EmptyResponse.self {
            await handleMutationIfNeeded(request)
            return EmptyResponse() as! Response
        }

        let payloadData = try await route(request)
        return try decoder.decode(Response.self, from: payloadData)
    }

    private func handleMutationIfNeeded<Response>(_ request: APIRequest<Response>) async {
        let path = request.path
        if request.method == .delete && path.hasPrefix("recordings/") {
            let id = String(path.dropFirst("recordings/".count))
            await DemoStateStore.shared.deleteRecording(id: id)
        } else if request.method == .put && path.hasPrefix("recordings/") && path.hasSuffix("/resume") {
            let id = path
                .replacingOccurrences(of: "recordings/", with: "")
                .replacingOccurrences(of: "/resume", with: "")
            if let body = request.body,
               let req = try? decoder.decode(Xg2gContract.RecordingResumeRequest.self, from: body) {
                await DemoStateStore.shared.updateResume(
                    id: id,
                    position: req.position,
                    total: req.total,
                    finished: req.finished
                )
            }
        } else if request.method == .post && path == "timers" {
            if let body = request.body,
               let req = try? decoder.decode(Xg2gContract.TimerCreateRequest.self, from: body) {
                await DemoStateStore.shared.addTimer(from: req)
            }
        } else if request.method == .delete && path.hasPrefix("timers/") {
            let id = String(path.dropFirst("timers/".count))
            await DemoStateStore.shared.deleteTimer(id: id)
        }
    }

    private func route<Response>(_ request: APIRequest<Response>) async throws -> Data {
        let path = request.path

        switch (request.method, path) {
        case (.get, "services/bouquets"):
            return try encoder.encode(DemoCatalog.bouquets)

        case (.get, "services"):
            let bouquetFilter = request.query.first(where: { $0.name == "bouquet" })?.value
            let filtered: [Xg2gContract.Service]
            if let bouquetFilter, !bouquetFilter.isEmpty {
                let matching = DemoCatalog.services.filter { $0.group == bouquetFilter }
                filtered = matching.isEmpty ? DemoCatalog.services : matching
            } else {
                filtered = DemoCatalog.services
            }
            return try encoder.encode(filtered)

        case (.post, "services/now-next"):
            let requestedRefs: [String]
            if let body = request.body,
               let req = try? decoder.decode(Xg2gContract.NowNextRequest.self, from: body) {
                requestedRefs = req.services
            } else {
                requestedRefs = DemoCatalog.services.compactMap(\.serviceRef)
            }
            return try encoder.encode(DemoCatalog.generateNowNext(for: requestedRefs))

        case (.get, "epg"):
            return try encoder.encode(DemoCatalog.generateEpgItems())

        case (.get, "recordings"):
            let items = await DemoStateStore.shared.listRecordings()
            let response = Xg2gContract.RecordingResponse(
                requestId: "req_demo_recordings",
                recordings: items
            )
            return try encoder.encode(response)

        case (.post, _) where path.hasPrefix("recordings/") && path.hasSuffix("/stream-info"):
            let json = """
            {
                "url": "\(DemoServer.appleBipBopHLSURL.absoluteString)",
                "mode": "hls",
                "durationSeconds": 1800
            }
            """
            return Data(json.utf8)

        case (.get, "timers"):
            let items = await DemoStateStore.shared.listTimers()
            return try encoder.encode(Xg2gContract.TimerList(items: items))

        case (.post, "live/stream-info"):
            let response = Xg2gContract.LiveStreamInfoResponse(
                decisionReason: "demo_apple_hls",
                dvrWindowSeconds: 1800,
                isSeekable: true,
                mode: .hls,
                playbackDecisionToken: "demo_decision_token",
                sessionId: "demo_live_session",
                url: DemoServer.appleBipBopHLSURL.absoluteString
            )
            return try encoder.encode(response)

        case (.post, "intents"):
            let response = Xg2gContract.IntentAcceptedResponse(
                requestId: "req_demo_intent",
                sessionId: "demo_live_session",
                status: .accepted
            )
            return try encoder.encode(response)

        case (.post, _) where path.hasPrefix("sessions/") && path.hasSuffix("/playback-ticket"):
            let response = Xg2gContract.PlaybackTicketResponse(
                cookie: "xg2g_ticket",
                expiresIn: 3600,
                path: "/",
                sessionId: "demo_live_session",
                ticket: "demo_ticket_value"
            )
            return try encoder.encode(response)

        case (.post, "auth/session"):
            let response = Xg2gContract.AuthSessionResponse(
                sessionId: "demo_media_session",
                cookie: "xg2g_session",
                expiresIn: 3600,
                path: "/"
            )
            return try encoder.encode(response)

        default:
            return Data("{}".utf8)
        }
    }
}
