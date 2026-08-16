// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// One channel, as the app understands it.
///
/// The wire type has more fields than this — `enabled`, `resolution`, `codec`,
/// `group`. They are not carried here because nothing in the app decides
/// anything with them yet, and a model that mirrors a schema rather than a use
/// keeps every future change honest only by accident.
///
/// `serviceRef` is the identifier that matters: it is what a stream intent and
/// a now/next lookup are keyed on. `id` is the catalogue key and is not
/// interchangeable with it.
struct Channel: Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let name: String
    let number: String?
    let serviceRef: String
    let logoURL: URL?

    /// Channels arrive with a `number` like "101" that is really an ordering
    /// key. Sorting on the string would put 100 before 2.
    var sortKey: Int { number.flatMap(Int.init) ?? Int.max }
}

/// What is on now, and what follows.
struct NowNext: Equatable, Sendable {
    struct Entry: Identifiable, Equatable, Sendable {
        var id: String { "\(start.timeIntervalSince1970)_\(title)" }
        let title: String
        let description: String?
        let start: Date
        let end: Date

        private static let timeFormatter: DateFormatter = {
            let f = DateFormatter()
            f.dateFormat = "HH:mm"
            f.timeZone = .current
            return f
        }()

        /// How far through the programme we are, 0…1. `nil` before it starts or
        /// after it ends, so a caller cannot mistake "not on" for "just began".
        func progress(at now: Date) -> Double? {
            let total = end.timeIntervalSince(start)
            guard total > 0, now >= start, now <= end else { return nil }
            return now.timeIntervalSince(start) / total
        }

        /// Minutes left in the currently running programme.
        func remainingMinutes(at now: Date) -> Int? {
            guard now >= start, now <= end else { return nil }
            let secondsLeft = end.timeIntervalSince(now)
            return max(1, Int(secondsLeft / 60))
        }

        var formattedStartTime: String {
            Self.timeFormatter.string(from: start)
        }

        var formattedEndTime: String {
            Self.timeFormatter.string(from: end)
        }

        var formattedTimeRange: String {
            "\(Self.timeFormatter.string(from: start)) – \(Self.timeFormatter.string(from: end))"
        }

        var formattedDayHeader: String {
            let calendar = Calendar.current
            if calendar.isDateInToday(start) {
                return "HEUTE"
            } else if calendar.isDateInTomorrow(start) {
                return "MORGEN"
            } else {
                let f = DateFormatter()
                f.locale = Locale(identifier: "de_DE")
                f.dateFormat = "EEEE, d. MMMM"
                return f.string(from: start).uppercased()
            }
        }

        var dayIdentifier: String {
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: start)
        }

        var durationMinutes: Int {
            max(1, Int(end.timeIntervalSince(start) / 60))
        }

        var genre: EpgGenre {
            genre(channelName: nil)
        }

        func genre(channelName: String? = nil) -> EpgGenre {
            EpgGenreClassifier.classify(title: title, description: description, channelName: channelName)
        }

        func matches(genre: EpgGenre, channelName: String? = nil) -> Bool {
            if genre == .all { return true }
            return self.genre(channelName: channelName) == genre
        }
    }

    let serviceRef: String
    let now: Entry?
    let next: Entry?
}

// MARK: - Advanced EPG Genre Classifier (Multi-Heuristic & Channel-Aware)

enum EpgGenreClassifier {

    nonisolated(unsafe) private static let classificationCache = NSCache<NSString, NSString>()
    nonisolated(unsafe) private static let regexCache = NSCache<NSString, NSRegularExpression>()

    private static func matchesRegex(_ text: String, pattern: String) -> Bool {
        guard !text.isEmpty else { return false }
        let key = pattern as NSString
        let regex: NSRegularExpression
        if let cached = regexCache.object(forKey: key) {
            regex = cached
        } else if let compiled = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive]) {
            regexCache.setObject(compiled, forKey: key)
            regex = compiled
        } else {
            return false
        }
        let range = NSRange(text.startIndex..<text.endIndex, in: text)
        return regex.firstMatch(in: text, options: [], range: range) != nil
    }

    /// Classifies an EPG entry into a genre using high-precision scoring,
    /// title/description weighting, word-boundary regex patterns, and channel specialization.
    static func classify(title: String, description: String?, channelName: String?) -> EpgGenre {
        let cacheKey = "\(title)|\(description ?? "")|\(channelName ?? "")" as NSString
        if let cached = classificationCache.object(forKey: cacheKey) {
            return EpgGenre(rawValue: cached as String) ?? .all
        }

        let computed = computeClassification(title: title, description: description, channelName: channelName)
        classificationCache.setObject(computed.rawValue as NSString, forKey: cacheKey)
        return computed
    }

    private static func computeClassification(title: String, description: String?, channelName: String?) -> EpgGenre {
        let titleLower = title.lowercased()
        let descLower = (description ?? "").lowercased()

        // 1. Direct High-Confidence Format Indicators
        if isDefiniteSeries(title: titleLower, description: descLower) {
            return .series
        }
        if isDefiniteMovie(title: titleLower, description: descLower) {
            return .movie
        }
        if isDefiniteNews(title: titleLower, description: descLower) {
            return .news
        }
        if isDefiniteSport(title: titleLower, description: descLower) {
            return .sport
        }
        if isDefiniteDocu(title: titleLower, description: descLower) {
            return .docu
        }
        if isDefiniteKids(title: titleLower, description: descLower) {
            return .kids
        }
        if isDefiniteShow(title: titleLower, description: descLower) {
            return .show
        }

        // 2. Multi-Score Evaluator (Weighting Title 3x over Description + Channel Priors)
        var scores: [EpgGenre: Int] = [
            .movie: 0,
            .series: 0,
            .sport: 0,
            .docu: 0,
            .show: 0,
            .news: 0,
            .kids: 0
        ]

        // Channel-type prior (Weight = 25)
        if let channelName {
            if channelMatches(genre: .sport, channelName: channelName) { scores[.sport, default: 0] += 25 }
            if channelMatches(genre: .movie, channelName: channelName) { scores[.movie, default: 0] += 25 }
            if channelMatches(genre: .series, channelName: channelName) { scores[.series, default: 0] += 25 }
            if channelMatches(genre: .news, channelName: channelName) { scores[.news, default: 0] += 25 }
            if channelMatches(genre: .docu, channelName: channelName) { scores[.docu, default: 0] += 25 }
            if channelMatches(genre: .kids, channelName: channelName) { scores[.kids, default: 0] += 25 }
            if channelMatches(genre: .show, channelName: channelName) { scores[.show, default: 0] += 25 }
        }

        scores[.movie, default: 0] += movieScore(title: titleLower, description: descLower)
        scores[.series, default: 0] += seriesScore(title: titleLower, description: descLower)
        scores[.sport, default: 0] += sportScore(title: titleLower, description: descLower)
        scores[.docu, default: 0] += docuScore(title: titleLower, description: descLower)
        scores[.show, default: 0] += showScore(title: titleLower, description: descLower)
        scores[.news, default: 0] += newsScore(title: titleLower, description: descLower)
        scores[.kids, default: 0] += kidsScore(title: titleLower, description: descLower)

        if let (bestGenre, bestScore) = scores.max(by: { $0.value < $1.value }), bestScore >= 10 {
            return bestGenre
        }

        return .all
    }

    static func channelMatches(genre: EpgGenre, channelName: String) -> Bool {
        let name = channelName.lowercased()
        switch genre {
        case .all:
            return true
        case .sport:
            return name.contains("sport") || name.contains("eurosport") || name.contains("dazn") ||
                   name.contains("motorvision") || name.contains("sportdigital") || name.contains("ran") ||
                   name.contains("espn") || name.contains("wwe")
        case .movie:
            return name.contains("cinema") || name.contains("film") || name.contains("kabel eins classics") ||
                   name.contains("warner tv film") || name.contains("kinowelt") || name.contains("tnt film") ||
                   name.contains("sky cinema") || name.contains("tele 5")
        case .series:
            return name.contains("serie") || name.contains("atlantic") || name.contains("13th street") ||
                   name.contains("syfy") || name.contains("warner tv serie") || name.contains("tnt serie") ||
                   name.contains("fox")
        case .news:
            return name.contains("tagesschau24") || name.contains("n-tv") || name.contains("welt") ||
                   name.contains("phoenix") || name.contains("euronews") || name.contains("cnn") ||
                   name.contains("bbc news") || name.contains("bloomberg") || name.contains("cnbc") ||
                   name.contains("al jazeera")
        case .docu:
            return name.contains("national geographic") || name.contains("discovery") || name.contains("history") ||
                   name.contains("planet") || name.contains("zdfinfo") || name.contains("ard alpha") ||
                   name.contains("geo") || name.contains("spiegel tv") || name.contains("crime + investigation")
        case .kids:
            return name.contains("kika") || name.contains("disney") || name.contains("nick") ||
                   name.contains("toggo") || name.contains("super rtl") || name.contains("cartoon") ||
                   name.contains("junior") || name.contains("boomerang") || name.contains("ric")
        case .show:
            return name.contains("comedy central") || name.contains("tlc")
        }
    }

    // MARK: - 1. Definite High-Confidence Matchers

    static func isDefiniteSeries(title: String, description: String) -> Bool {
        let t = title.lowercased()
        let d = description.lowercased()

        // S01E02 / Staffel 1 / Folge 3 / Episode 4 / Teil 1/3
        if matchesRegex(t, pattern: #"\b(s\d+[\s/]*e\d+|staffel\s*\d+|folge\s*\d+|episode\s*\d+|teil\s*\d+\s*/\s*\d+)\b"#) ||
           matchesRegex(d, pattern: #"\b(s\d+[\s/]*e\d+|staffel\s*\d+|folge\s*\d+|episode\s*\d+|teil\s*\d+\s*/\s*\d+)\b"#) {
            return true
        }

        // Distinct Series formats in title or description
        if matchesRegex(t, pattern: #"\b(krimiserie|comedyserie|dramaserie|us-serie|sitcom|telenovela|daily soap|miniserie|mini-serie|animationsserie|anime-serie|arztserie|vorabendserie|jugendserie)\b"#) ||
           matchesRegex(d, pattern: #"\b(krimiserie|comedyserie|dramaserie|us-serie|sitcom|telenovela|daily soap|miniserie|mini-serie|animationsserie|anime-serie|arztserie|vorabendserie)\b"#) {
            return true
        }

        // Specific series titles / title patterns
        if matchesRegex(t, pattern: #"\b(gute zeiten, schlechte zeiten|gzsz|unter uns|alles was zählt|rote rosen|sturm der liebe|in aller freundschaft|die bergretter|der bergdoktor|notruf hafenkante|soko \w+|hubert ohne staller|hubert und staller|die rosenheim-cops|großstadtrevier|watzmann ermittelt|morden im norden|wapo \w+|navy cis|ncis|csi:\s*\w+|criminal minds|the big bang theory|young sheldon|modern family|how i met your mother|two and a half men|friends|grey's anatomy|die simpsons|family guy|futurama|south park|rick and morty|the walking dead|game of thrones|house of the dragon|stranger things|breaking bad|better call saul|fargo|yellowstone|the rookie|chicago fire|chicago pd|chicago med|blue bloods|hawaii five-0|magnum p\.i\.|doctor who|star trek:)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteMovie(title: String, description: String) -> Bool {
        let t = title.lowercased()
        let d = description.lowercased()

        // Format tags in title or description
        if matchesRegex(t, pattern: #"\b(spielfilm|fernsehfilm|actionfilm|kriminalfilm|liebesfilm|heimatfilm|kurzfilm|blockbuster|filmdrama|gangsterfilm|psychothriller|katastrophenfilm|liebeskomödie|tragikomödie|historienfilm|monumentalfilm|fantasyfilm|sci-fi-film|science-fiction-film|zeichentrickfilm|animationsfilm|abenteuerfilm|westernfilm|kino-film|kinofilm|krimikomödie)\b"#) ||
           matchesRegex(d, pattern: #"\b(spielfilm|fernsehfilm|actionfilm|kriminalfilm|liebesfilm|heimatfilm|kurzfilm|blockbuster|filmdrama|gangsterfilm|psychothriller|katastrophenfilm|liebeskomödie|tragikomödie|historienfilm|monumentalfilm|fantasyfilm|sci-fi-film|science-fiction-film|zeichentrickfilm|animationsfilm|abenteuerfilm|westernfilm|kino-film|kinofilm|krimikomödie)\b"#) {
            return true
        }

        // TV Movie Brands (Tatort, Polizeiruf 110, Wilsberg, Inga Lindström, Rosamunde Pilcher...)
        if matchesRegex(t, pattern: #"\b(tatort|polizeiruf 110|der alte|der staatsanwalt|ein fall für zwei|wilsberg|nord nord mord|der zürich-krimi|der usedom-krimi|der passau-krimi|der barcelona-krimi|der irland-krimi|der bozen-krimi|der kroatien-krimi|der salzburg-krimi|der amsterdam-krimi|der stralsund-krimi|donna leon|friesland|marie brand|stralsund|münchen mord|kommissarin lucas|helen dorn|ein starkes team|das quartett|erzgebirgskrimi|herzkino|das traumschiff|kreuzfahrt ins glück|rosamunde pilcher|inga lindström|katie fforde|emilie richards)\b"#) {
            return true
        }

        // Production Year anywhere in title or description: e.g. (USA 2023), (D 2021), (2022)
        if matchesRegex(t, pattern: #"\(([a-z]{1,4}(/[a-z]{1,4})*\s+)?(19\d\d|20\d\d)\)"#) ||
           matchesRegex(d, pattern: #"\(([a-z]{1,4}(/[a-z]{1,4})*\s+)?(19\d\d|20\d\d)\)"#) {
            return true
        }

        // Production credits in description (Regie: ... / Hauptdarsteller: ...)
        if matchesRegex(d, pattern: #"\b(regie:|hauptdarsteller:|originaltitel:|drehbuch:|fsk:\s*\d+)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteNews(title: String, description: String) -> Bool {
        let t = title.lowercased()

        // News brands & titles
        if matchesRegex(t, pattern: #"\b(tagesschau|tagesthemen|heute journal|heute-journal|heute xpress|heute in deutschland|heute in europa|heute 19:00|rtl aktuell|sat\.1 nachrichten|prosieben nachrichten|kabel eins news|newstime|zeit im bild|zib 1|zib 2|zib flash|zib magazin|zib nacht|rundschau|ard brennpunkt|brennpunkt:|zdf spezial|zdfspezial|presseclub|bericht aus berlin|world news|bbc news|cnn news|nachrichtenmagazin|spätausgabe|mittagsmagazin|morgenmagazin|zdf-morgenmagazin|ard-morgenmagazin|frühstart|telebörse|börse vor acht|wetter vor acht|das wetter|wetterbericht)\b"#) {
            return true
        }

        if matchesRegex(t, pattern: #"^(nachrichten|aktuell|wetter|zib|journal)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteSport(title: String, description: String) -> Bool {
        let t = title.lowercased()

        // Live sports prefix
        if matchesRegex(t, pattern: #"\b(live:\s*(fußball|fussball|formel|tennis|sport|eishockey|basketball|handball|darts?|golf|wintersport|radsport|boxen))\b"#) {
            return true
        }

        // Sports brands & major shows
        if matchesRegex(t, pattern: #"\b(sportschau|das aktuelle sportstudio|sportstudio|doppelpass|ran football|ran bundesliga|ran racing|sky sport news|eurosport news|sport extra)\b"#) {
            return true
        }

        // Prominent sports leagues & events in Title
        if matchesRegex(t, pattern: #"\b(bundesliga|2\. bundesliga|3\. liga|premier league|champions league|europa league|conference league|dfb[- ]pokal|la liga|serie a|ligue 1|länderspiel|uefa\b|fifa\b|champions-league|bundesliga-konferenz|formel [1234e]|formula [1234e]|motogp|moto2|moto3|superbike|motorsport|nascar|rallye-wm|wrc\b|indycar|nürburgring 24h|24h le mans|wintersport|ski alpin|skispringen|biathlon|nordische kombination|bobsport|skeleton|eiskunstlauf|eishockey|vierschanzentournee|wimbledon|us open|french open|roland garros|australian open|davis cup|euroleague|super bowl|american football|pdc darts|darts-wm|dart-wm|pga tour|ryder cup|profiboxen|boxkampf|tour de france|giro d'italia|vuelta|olympische spiele|paralympics)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteDocu(title: String, description: String) -> Bool {
        let t = title.lowercased()
        let d = description.lowercased()

        if matchesRegex(t, pattern: #"\b(dokumentation|dokumentarfilm|doku-reihe|dokureihe|reportage|doku-serie|dokuserie|terra x|planet erde|planet wissen|welt der wunder|quarks|leschs kosmos|abenteuer leben|galileo|faszination erde|universum|kulturzeit|arte journal|arte entdeckung|spiegel tv|spiegel geschichte|national geographic|discovery channel|history channel|crime \+ investigation|geo television|37 grad|naturdokumentation|tierdokumentation|geschichtsdokumentation|zeitgeschichte)\b"#) ||
           matchesRegex(d, pattern: #"\b(dokumentation|dokumentarfilm|doku-reihe|dokureihe|doku-serie|dokuserie|terra x|naturdokumentation|tierdokumentation|geschichtsdokumentation)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteKids(title: String, description: String) -> Bool {
        let t = title.lowercased()

        if matchesRegex(t, pattern: #"\b(kika|sendung mit der maus|löwenzahn|paw patrol|peppa wutz|peppa pig|sandmännchen|checker tobi|checker julian|checker marina|willi wills wissen|pur\+|purplus|1, 2 oder 3|sesamstraße|sesamstrasse|logo!|anna und die haustiere|anna und die wilden tiere|neun\+einhalb|die pfefferkörner|schloss einstein|marvi hämmer|tom und jerry|spongebob|sponge bob|phineas und ferb|micky maus|ben 10|bob der baumeister|feuerwehrmann sam|yakari|tabaluga|pettersson und findus|lauras stern|wickie|heidi|pippi langstrumpf|biene maja|kinderfilm|kinderserie|puppentrick|kindernachrichten)\b"#) {
            return true
        }

        return false
    }

    static func isDefiniteShow(title: String, description: String) -> Bool {
        let t = title.lowercased()

        if matchesRegex(t, pattern: #"\b(talkshow|quizshow|gameshow|late-night-show|late night|unterhaltungsshow|comedy-show|comedyshow|satire-show|tv total|wer weiß denn sowas|wer weiss denn sowas|gefragt – gejagt|gefragt - gejagt|wer wird millionär|the masked singer|maskierte sänger|dsds|deutschland sucht den superstar|let's dance|the voice|the voice of germany|heute-show|extra 3|die anstalt|neo magazin|zdf magazin royale|maischberger|markus lanz|hart aber fair|caren miosga|maybrit illner|illner|kölner treff|ndr talk show|riverboat|3nach9|3 nach 9|kabarett|stand-up-comedy|schlagerboom|florian silbereisen|verstehen sie spaß|klein gegen groß|duell um die welt|joko & klaas|joko und klaas|ninja warrior|bachelor|bachelorette|ich bin ein star|dschungelcamp|promi big brother|big brother|shopping queen|first dates|das perfekte dinner|grill den henssler|kitchen impossible)\b"#) {
            return true
        }

        return false
    }

    // MARK: - 2. Score Evaluators for Tie-Breaking and Ambiguous Programs

    private static func movieScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(thriller|drama|komödie|action|horror|krimi|western|science fiction|sci-fi|fantasy)\b"#) {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(thriller|komödie|action-thriller|psychothriller|kriminalfilm|filmdrama|kinofilm|abenteuerfilm|katastrophenfilm)\b"#) {
            score += 15
        }
        if matchesRegex(description, pattern: #"\b(regie:|darsteller:|fsk:)\b"#) {
            score += 10
        }
        return score
    }

    private static func seriesScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(serie|fernsehserie|soap|dramedy|anime|sitcom)\b"#) {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(serie|fernsehserie|dramaserie|krimiserie|comedyserie)\b"#) {
            score += 15
        }
        return score
    }

    private static func sportScore(title: String, description: String) -> Int {
        var score = 0
        let isStarWars = title.contains("star wars") || description.contains("darth") || description.contains("jedi") || description.contains("skywalker")
        if isStarWars {
            return 0
        }

        if matchesRegex(title, pattern: #"\b(sport|fussball|fußball|tennis|basketball|handball|eishockey|golf|darts|boxen|motorsport|wintersport|radsport|marathon|triathlon|olympia|nfl|nba|nhl|atp|wta)\b"#) {
            score += 25
        }
        if matchesRegex(description, pattern: #"\b(bundesliga|champions league|dfb[- ]pokal|premier league|formel 1|motogp|ski alpin|biathlon|wimbledon|super bowl|tour de france|leichtathletik|boxkampf|darts-wm)\b"#) {
            score += 15
        }
        return score
    }

    private static func newsScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(nachrichten|aktuell|wetter|journal|rundschau|brennpunkt|telebörse|presseclub)\b"#) {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(nachrichtenmagazin|spätausgabe|sondersendung|eilmeldung)\b"#) {
            score += 15
        }
        return score
    }

    private static func docuScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(dokumentation|reportage|doku|expedition|faszination|universum|biografie)\b"#) {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(dokumentarfilm|reportage|doku-reihe|naturdokumentation|tierdokumentation|zeitgeschichte)\b"#) {
            score += 15
        }
        return score
    }

    private static func kidsScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(kika|zeichentrick|märchen|kinderfilm|kinderserie|trickfilm|animation)\b"#) {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(kinderfilm|kinderserie|zeichentrickserie|puppentrick)\b"#) {
            score += 15
        }
        return score
    }

    private static func showScore(title: String, description: String) -> Int {
        var score = 0
        if matchesRegex(title, pattern: #"\b(show|talkshow|quiz|gameshow|unterhaltung|comedy|late night|kabarett|satire|gala)\b"#) && !title.contains("showdown") {
            score += 20
        }
        if matchesRegex(description, pattern: #"\b(talkshow|quizshow|gameshow|unterhaltungsshow|comedy-show|satire-show|varieté)\b"#) && !description.contains("showdown") {
            score += 15
        }
        return score
    }

    // MARK: - Compatibility Convenience Methods

    static func isMovie(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .movie
    }
    static func isMovie(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .movie
    }

    static func isSeries(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .series
    }
    static func isSeries(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .series
    }

    static func isSport(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .sport
    }
    static func isSport(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .sport
    }

    static func isNews(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .news
    }
    static func isNews(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .news
    }

    static func isDocu(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .docu
    }
    static func isDocu(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .docu
    }

    static func isKids(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .kids
    }
    static func isKids(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .kids
    }

    static func isShow(title: String, description: String? = nil, channelName: String? = nil) -> Bool {
        classify(title: title, description: description, channelName: channelName) == .show
    }
    static func isShow(text: String, channelName: String?) -> Bool {
        classify(title: text, description: nil, channelName: channelName) == .show
    }
}

/// EPG Genre classification for filtering and highlighting (TV Pro style)
enum EpgGenre: String, CaseIterable, Identifiable, Sendable {
    case all = "Alle"
    case movie = "Spielfilme"
    case series = "Serien"
    case sport = "Sport"
    case docu = "Doku & Wissen"
    case show = "Unterhaltung"
    case news = "Nachrichten"
    case kids = "Kinder"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .all: return "square.grid.2x2"
        case .movie: return "film"
        case .series: return "tv"
        case .sport: return "sportscourt"
        case .docu: return "globe.europe.africa"
        case .show: return "sparkles.tv"
        case .news: return "newspaper"
        case .kids: return "teddybear"
        }
    }
}

/// View presentation mode: Compact List vs Magazine Grid (TV Pro style)
enum EpgViewMode: String, CaseIterable, Identifiable, Sendable {
    case list = "Senderliste"
    case magazine = "Magazin"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .list: return "list.bullet"
        case .magazine: return "square.grid.2x2"
        }
    }
}

/// A bouquet / channel group (e.g. "Favorites", "HD", "Sports").
struct Bouquet: Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let name: String
    let servicesCount: Int

    init(id: String? = nil, name: String, servicesCount: Int = 0) {
        self.id = id ?? name
        self.name = name
        self.servicesCount = servicesCount
    }
}

// MARK: - Wire

enum ChannelWire {

    struct BouquetItem: Decodable, Sendable {
        let name: String?
        let services: Int?

        func toDomain() -> Bouquet? {
            guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty else { return nil }
            return Bouquet(name: name, servicesCount: services ?? 0)
        }
    }

    struct EpgItem: Decodable, Sendable {
        let serviceRef: String?
        let title: String?
        let desc: String?
        let start: Int?
        let end: Int?

        func toDomain() -> (String, NowNext.Entry)? {
            guard let serviceRef, let title, let start, let end else { return nil }
            let sanitizedDesc: String? = {
                guard let raw = desc?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
                    return nil
                }
                var text = raw
                    .replacingOccurrences(of: "\\n", with: "\n")
                    .replacingOccurrences(of: "\\r", with: "")
                    .replacingOccurrences(of: "\\t", with: "\t")
                while text.contains("\n\n\n") {
                    text = text.replacingOccurrences(of: "\n\n\n", with: "\n\n")
                }
                return text.trimmingCharacters(in: .whitespacesAndNewlines)
            }()

            let entry = NowNext.Entry(
                title: title.replacingOccurrences(of: "\\n", with: " ").trimmingCharacters(in: .whitespacesAndNewlines),
                description: sanitizedDesc,
                start: Date(timeIntervalSince1970: TimeInterval(start)),
                end: Date(timeIntervalSince1970: TimeInterval(end))
            )
            return (serviceRef, entry)
        }
    }

    /// The server sends every field as optional. A channel without a name or a
    /// service reference cannot be displayed or played, so it is dropped at the
    /// boundary rather than carried inward as a half-value.
    struct Service: Decodable, Sendable {
        let id: String?
        let name: String?
        let number: String?
        let serviceRef: String?
        let logoUrl: String?

        func toDomain(baseURL: URL? = nil) -> Channel? {
            guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty,
                  let serviceRef = serviceRef?.trimmingCharacters(in: .whitespaces), !serviceRef.isEmpty
            else { return nil }

            let resolvedLogo: URL?
            if let rawLogo = logoUrl?.trimmingCharacters(in: .whitespacesAndNewlines), !rawLogo.isEmpty {
                if rawLogo.hasPrefix("http://") || rawLogo.hasPrefix("https://") {
                    resolvedLogo = URL(string: rawLogo)
                } else if let baseURL {
                    let path = rawLogo.hasPrefix("/") ? String(rawLogo.dropFirst()) : rawLogo
                    resolvedLogo = baseURL.appendingPathComponent(path)
                } else {
                    resolvedLogo = URL(string: rawLogo)
                }
            } else if let baseURL {
                // Fallback: Default OpenWebif/xg2g logo path by normalized service reference
                let sanitizedRef = serviceRef.replacingOccurrences(of: ":", with: "_").trimmingCharacters(in: CharacterSet(charactersIn: "_"))
                resolvedLogo = baseURL.appendingPathComponent("logos/\(sanitizedRef).png")
            } else {
                resolvedLogo = nil
            }

            return Channel(
                id: id?.isEmpty == false ? id! : serviceRef,
                name: name,
                number: number?.isEmpty == false ? number : nil,
                serviceRef: serviceRef,
                logoURL: resolvedLogo
            )
        }
    }

    struct NowNextRequest: Encodable, Sendable {
        let services: [String]
    }

    struct NowNextResponse: Decodable, Sendable {
        let items: [Item]

        struct Item: Decodable, Sendable {
            let serviceRef: String
            let now: Entry?
            let next: Entry?
        }

        struct Entry: Decodable, Sendable {
            let title: String
            let desc: String?
            let start: Int
            let end: Int

            func toDomain() -> NowNext.Entry {
                let sanitizedDesc: String? = {
                    guard let raw = desc?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
                        return nil
                    }
                    var text = raw
                        .replacingOccurrences(of: "\\n", with: "\n")
                        .replacingOccurrences(of: "\\r", with: "")
                        .replacingOccurrences(of: "\\t", with: "\t")
                    while text.contains("\n\n\n") {
                        text = text.replacingOccurrences(of: "\n\n\n", with: "\n\n")
                    }
                    return text.trimmingCharacters(in: .whitespacesAndNewlines)
                }()

                let sanitizedTitle = title
                    .replacingOccurrences(of: "\\n", with: " ")
                    .replacingOccurrences(of: "\\r", with: "")
                    .trimmingCharacters(in: .whitespacesAndNewlines)

                return NowNext.Entry(
                    title: sanitizedTitle,
                    description: sanitizedDesc,
                    start: Date(timeIntervalSince1970: TimeInterval(start)),
                    end: Date(timeIntervalSince1970: TimeInterval(end))
                )
            }
        }
    }
}

extension NowNext {
    init(item: ChannelWire.NowNextResponse.Item) {
        self.init(
            serviceRef: item.serviceRef,
            now: item.now?.toDomain(),
            next: item.next?.toDomain()
        )
    }
}

extension Sequence where Element: Hashable {
    func uniqued() -> [Element] {
        var seen = Set<Element>()
        return filter { seen.insert($0).inserted }
    }
}
