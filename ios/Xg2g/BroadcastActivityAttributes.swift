// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
#if canImport(ActivityKit)
import ActivityKit

public struct BroadcastLiveActivityAttributes: ActivityAttributes {
    public struct ContentState: Codable, Hashable, Sendable {
        public var showTitle: String
        public var subtitle: String?
        public var startTimestamp: Double
        public var endTimestamp: Double
        public var isRecording: Bool
        public var isDirectStream: Bool
        public var channelNumber: String?

        public init(
            showTitle: String,
            subtitle: String? = nil,
            startTimestamp: Double,
            endTimestamp: Double,
            isRecording: Bool = false,
            isDirectStream: Bool = true,
            channelNumber: String? = nil
        ) {
            self.showTitle = showTitle
            self.subtitle = subtitle
            self.startTimestamp = startTimestamp
            self.endTimestamp = endTimestamp
            self.isRecording = isRecording
            self.isDirectStream = isDirectStream
            self.channelNumber = channelNumber
        }
    }

    public var channelName: String
    public var serviceRef: String

    public init(channelName: String, serviceRef: String) {
        self.channelName = channelName
        self.serviceRef = serviceRef
    }
}
#endif
