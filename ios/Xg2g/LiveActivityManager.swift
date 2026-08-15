// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
#if canImport(ActivityKit)
import ActivityKit
#endif

final class LiveActivityManager: @unchecked Sendable {

    static let shared = LiveActivityManager()

    #if canImport(ActivityKit)
    private var currentActivity: Activity<BroadcastLiveActivityAttributes>?
    #endif

    private init() {}

    func startActivity(
        channel: Channel,
        nowNext: NowNext?,
        isDirectStream: Bool = true
    ) {
        #if canImport(ActivityKit)
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }

        // End any existing activity before starting a new one
        if let existing = currentActivity {
            currentActivity = nil
            Task {
                await existing.end(nil, dismissalPolicy: .immediate)
            }
        }

        let attributes = BroadcastLiveActivityAttributes(
            channelName: channel.name,
            serviceRef: channel.id
        )

        let initialNow = nowNext?.now
        let start = initialNow?.start.timeIntervalSince1970 ?? Date().timeIntervalSince1970
        let end = initialNow?.end.timeIntervalSince1970 ?? Date().addingTimeInterval(3600).timeIntervalSince1970

        let initialContentState = BroadcastLiveActivityAttributes.ContentState(
            showTitle: initialNow?.title ?? channel.name,
            subtitle: nowNext?.next?.title,
            startTimestamp: start,
            endTimestamp: end,
            isRecording: false,
            isDirectStream: isDirectStream,
            channelNumber: channel.number
        )

        do {
            let activity = try Activity.request(
                attributes: attributes,
                content: .init(state: initialContentState, staleDate: Date(timeIntervalSince1970: end)),
                pushType: nil
            )
            self.currentActivity = activity
        } catch {
            // Live activities not available or disabled by user policy
        }
        #endif
    }

    func updateActivity(
        nowNext: NowNext?,
        isRecording: Bool = false,
        isDirectStream: Bool = true,
        channelNumber: String? = nil
    ) {
        #if canImport(ActivityKit)
        guard let activity = currentActivity else { return }

        let now = nowNext?.now
        let start = now?.start.timeIntervalSince1970 ?? Date().timeIntervalSince1970
        let end = now?.end.timeIntervalSince1970 ?? Date().addingTimeInterval(3600).timeIntervalSince1970

        let updatedState = BroadcastLiveActivityAttributes.ContentState(
            showTitle: now?.title ?? activity.attributes.channelName,
            subtitle: nowNext?.next?.title,
            startTimestamp: start,
            endTimestamp: end,
            isRecording: isRecording,
            isDirectStream: isDirectStream,
            channelNumber: channelNumber
        )

        let content = ActivityContent(state: updatedState, staleDate: Date(timeIntervalSince1970: end))
        Task {
            await activity.update(content)
        }
        #endif
    }

    func endActivity() {
        #if canImport(ActivityKit)
        guard let activity = currentActivity else { return }
        self.currentActivity = nil
        Task {
            await activity.end(nil, dismissalPolicy: .immediate)
        }
        #endif
    }
}
