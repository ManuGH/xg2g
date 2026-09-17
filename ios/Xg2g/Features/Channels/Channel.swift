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
