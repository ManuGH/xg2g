// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

// MARK: - Contract Domain Mapping

extension Xg2gContract.Bouquet {
    func toDomain() -> ChannelBouquet? {
        guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty else { return nil }
        return ChannelBouquet(name: name, servicesCount: services ?? 0)
    }
}

extension Xg2gContract.EpgItem {
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

extension Xg2gContract.Service {
    func toDomain(baseURL: URL? = nil) -> Channel? {
        guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty,
              let serviceRef = serviceRef?.trimmingCharacters(in: .whitespaces), !serviceRef.isEmpty
        else { return nil }

        let resolvedLogo = MediaEndpoints.logoURL(raw: logoUrl, serviceRef: serviceRef, baseURL: baseURL)

        return Channel(
            id: id?.isEmpty == false ? id! : serviceRef,
            name: name,
            number: number?.isEmpty == false ? number : nil,
            serviceRef: serviceRef,
            logoURL: resolvedLogo
        )
    }
}

extension Xg2gContract.NowNextEntry {
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

extension NowNext {
    init(item: Xg2gContract.NowNextItem) {
        self.init(
            serviceRef: item.serviceRef,
            now: item.now?.toDomain(),
            next: item.next?.toDomain()
        )
    }
}
