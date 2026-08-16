// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct EpgGenreClassifierTests {

    // MARK: - Movies & False Positive Avoidance

    @Test func movieWithDarthVaderIsNotClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Star Wars: Die Rückkehr der Jedi-Ritter",
            description: "Luke Skywalker und seine Freunde kämpfen gegen Darth Vader und das böse Imperium. (USA 1983)",
            channelName: "ProSieben"
        )
        #expect(genre == .movie)
    }

    @Test func movieWithWannseekonferenzIsNotClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Die Wannseekonferenz",
            description: "Historischer Spielfilm über das geheime Treffen der NS-Führung am Wannsee im Jahr 1942. (D 2022)",
            channelName: "ZDF"
        )
        #expect(genre == .movie)
    }

    @Test func crimeThrillerWithComplexPlotIsNotClassifiedAsNewsKidsOrSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Tatort: Schatten der Vergangenheit",
            description: "In einer alten Gastwirtschaft geschieht ein Mord. Während draußen ein Unwetter tobt, muss der Ermittler mit seinen Dämonen ringen, ohne zu wissen, wo die zwei Kinder versteckt wurden. (D 2024)",
            channelName: "Das Erste"
        )
        #expect(genre == .movie)
    }

    @Test func actionMovieWithShowdownIsNotClassifiedAsShow() {
        let genre = EpgGenreClassifier.classify(
            title: "The Dark Knight",
            description: "Batman liefert sich einen dramatischen Showdown mit dem Joker in Gotham City. Regie: Christopher Nolan. (USA 2008)",
            channelName: "Sat.1"
        )
        #expect(genre == .movie)
    }

    @Test func movieOnCinemaChannelInheritsMovieGenre() {
        let genre = EpgGenreClassifier.classify(
            title: "Unbekannter Titel",
            description: "Spannende Unterhaltung am Abend.",
            channelName: "Sky Cinema Premiere HD"
        )
        #expect(genre == .movie)
    }

    // MARK: - Series

    @Test func seriesWithEpisodeCodeIsClassifiedAsSeries() {
        let genre = EpgGenreClassifier.classify(
            title: "Stranger Things",
            description: "S04E07: Das Massaker im Hawkins Lab. Elfie erinnert sich an die Vergangenheit.",
            channelName: "Netflix / TV"
        )
        #expect(genre == .series)
    }

    @Test func seriesWithStaffelFolgeIsClassifiedAsSeries() {
        let genre = EpgGenreClassifier.classify(
            title: "Die Bergretter",
            description: "Staffel 15, Folge 4: Dünnes Eis. Markus Kofler muss einen verunglückten Bergsteiger retten.",
            channelName: "ZDF"
        )
        #expect(genre == .series)
    }

    @Test func famousDailySoapIsClassifiedAsSeries() {
        let genre = EpgGenreClassifier.classify(
            title: "Gute Zeiten, schlechte Zeiten",
            description: "Folge 7890. Jo Gerner plant seinen nächsten Schachzug.",
            channelName: "RTL"
        )
        #expect(genre == .series)
    }

    @Test func seriesOnDedicatedChannelInheritsSeriesGenre() {
        let genre = EpgGenreClassifier.classify(
            title: "Beliebte Sendung",
            description: "Neue Geschichten aus Übersee.",
            channelName: "Warner TV Serie"
        )
        #expect(genre == .series)
    }

    // MARK: - Sports

    @Test func liveFootballMatchIsClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Bundesliga: FC Bayern München – Borussia Dortmund",
            description: "Live-Übertragung des 24. Spieltags aus der Allianz Arena.",
            channelName: "Sky Sport Bundesliga 1"
        )
        #expect(genre == .sport)
    }

    @Test func formulaOneQualifyingIsClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Formel 1: GP von Monaco - Qualifying",
            description: "Die Jagd nach der Pole Position im Fürstentum.",
            channelName: "Sky Sport F1"
        )
        #expect(genre == .sport)
    }

    @Test func dartsChampionshipIsClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "PDC Darts World Championship",
            description: "Das große Finale im Alexandra Palace live aus London.",
            channelName: "Sport1"
        )
        #expect(genre == .sport)
    }

    @Test func sportsMagazineIsClassifiedAsSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Sportschau",
            description: "Bundesliga-Zusammenfassung und Berichte vom aktuellen Spieltag.",
            channelName: "Das Erste"
        )
        #expect(genre == .sport)
    }

    @Test func broadcastOnSportsChannelDefaultsToSport() {
        let genre = EpgGenreClassifier.classify(
            title: "Die Analyse",
            description: "Rückblick und Höhepunkte des Wochenendes.",
            channelName: "DAZN 1"
        )
        #expect(genre == .sport)
    }

    // MARK: - News

    @Test func tagesschauIsClassifiedAsNews() {
        let genre = EpgGenreClassifier.classify(
            title: "Tagesschau",
            description: "Die wichtigsten Nachrichten des Tages.",
            channelName: "Das Erste"
        )
        #expect(genre == .news)
    }

    @Test func heuteJournalIsClassifiedAsNews() {
        let genre = EpgGenreClassifier.classify(
            title: "heute-journal",
            description: "Nachrichten, Berichte und Hintergründe aus Politik und Wirtschaft.",
            channelName: "ZDF"
        )
        #expect(genre == .news)
    }

    @Test func zdfSpezialIsClassifiedAsNews() {
        let genre = EpgGenreClassifier.classify(
            title: "ZDF spezial: Wahlen in den USA",
            description: "Sondersendung mit Analysen und Hochrechnungen.",
            channelName: "ZDF"
        )
        #expect(genre == .news)
    }

    // MARK: - Documentaries

    @Test func terraXIsClassifiedAsDocu() {
        let genre = EpgGenreClassifier.classify(
            title: "Terra X: Faszination Erde",
            description: "Dokumentation über die extremsten Orte unseres Planeten.",
            channelName: "ZDF"
        )
        #expect(genre == .docu)
    }

    @Test func historyDocuIsClassifiedAsDocu() {
        let genre = EpgGenreClassifier.classify(
            title: "Geheimnisse der Antike",
            description: "Dokumentarfilm über die Pyramiden von Gizeh und neue archäologische Funde.",
            channelName: "ZDFinfo"
        )
        #expect(genre == .docu)
    }

    // MARK: - Kids

    @Test func sendungMitDerMausIsClassifiedAsKids() {
        let genre = EpgGenreClassifier.classify(
            title: "Die Sendung mit der Maus",
            description: "Lach- und Sachgeschichten für Kinder.",
            channelName: "WDR"
        )
        #expect(genre == .kids)
    }

    @Test func pawPatrolIsClassifiedAsKids() {
        let genre = EpgGenreClassifier.classify(
            title: "PAW Patrol",
            description: "Die Rettungshunde helfen Bürgermeister Besserwisser aus der Klemme.",
            channelName: "Super RTL"
        )
        #expect(genre == .kids)
    }

    // MARK: - Shows & Entertainment

    @Test func quizshowIsClassifiedAsShow() {
        let genre = EpgGenreClassifier.classify(
            title: "Wer weiß denn sowas?",
            description: "Das beliebte Wissensquiz mit Kai Pflaume, Bernhard Hoëcker und Elton.",
            channelName: "Das Erste"
        )
        #expect(genre == .show)
    }

    @Test func letsDanceIsClassifiedAsShow() {
        let genre = EpgGenreClassifier.classify(
            title: "Let's Dance",
            description: "Die große Tanz-Liveshow mit Prominenten und Profitänzern.",
            channelName: "RTL"
        )
        #expect(genre == .show)
    }

    @Test func talkshowIsClassifiedAsShow() {
        let genre = EpgGenreClassifier.classify(
            title: "Markus Lanz",
            description: "Talkshow mit Gästen aus Politik, Kultur und Gesellschaft.",
            channelName: "ZDF"
        )
        #expect(genre == .show)
    }
}
