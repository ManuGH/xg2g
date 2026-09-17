// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI
import UIKit

/// SwiftUI wrapper around Apple's `AVRoutePickerView` for seamless 1-tap AirPlay routing
/// to Apple TV, HomePod, Sonos, and AirPods.
struct AirPlayButton: UIViewRepresentable {

    var tintColor: UIColor = .white
    var activeTintColor: UIColor = UIColor(red: 1.0, green: 0.698, blue: 0.290, alpha: 1.0)

    func makeUIView(context: Context) -> AVRoutePickerView {
        let routePicker = AVRoutePickerView()
        routePicker.backgroundColor = .clear
        routePicker.tintColor = tintColor
        routePicker.activeTintColor = activeTintColor
        routePicker.prioritizesVideoDevices = true
        return routePicker
    }

    func updateUIView(_ uiView: AVRoutePickerView, context: Context) {
        uiView.tintColor = tintColor
        uiView.activeTintColor = activeTintColor
    }
}
