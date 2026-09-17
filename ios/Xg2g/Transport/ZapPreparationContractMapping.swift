// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension Xg2gContract.ZapPreparationResponse {
    func toDomain() -> ZapPreparation {
        ZapPreparation(
            preparationId: preparationId,
            zapId: zapId,
            serviceRef: serviceRef,
            state: state.rawValue,
            outcome: outcome,
            generation: generation.map { UInt64($0) },
            readyAfterMs: readyAfterMs.map { Int($0) },
            pending: pending,
            detail: detail
        )
    }
}
