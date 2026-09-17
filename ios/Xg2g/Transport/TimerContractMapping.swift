// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

// MARK: - Contract Domain Mapping

extension Xg2gContract.Timer {
    func toDomain() -> DVRTimer? {
        let id = timerId.trimmingCharacters(in: .whitespaces)
        let name = name.trimmingCharacters(in: .whitespaces)
        let serviceRef = serviceRef.trimmingCharacters(in: .whitespaces)
        guard !id.isEmpty, !name.isEmpty, !serviceRef.isEmpty else { return nil }

        return DVRTimer(
            id: id,
            name: name,
            description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
            serviceRef: serviceRef,
            serviceName: serviceName?.trimmingCharacters(in: .whitespaces).isEmpty == false ? serviceName : nil,
            beginDate: Date(timeIntervalSince1970: TimeInterval(begin)),
            endDate: Date(timeIntervalSince1970: TimeInterval(end)),
            state: state.rawValue
        )
    }
}
