// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// SwiftUI screen to test and benchmark the Phase 1 1080i50 $\rightarrow$ 1080p50 VideoToolbox + Metal Vertical Slice.
public struct TestTSPlayerScreen: View {

    @StateObject private var pipeline = NativeTSVideoPipeline()
    @State private var streamURLString: String = "http://10.10.55.64:8001/1:0:19:81:6:85:C00000:0:0:0:"
    @State private var isStreaming: Bool = false
    @State private var showHUD: Bool = true

    private struct ChannelPreset: Identifiable {
        let id = UUID()
        let name: String
        let url: String
    }

    private let presets: [ChannelPreset] = [
        ChannelPreset(name: "Sky Sport Top Event", url: "http://10.10.55.64:8001/1:0:19:81:6:85:C00000:0:0:0:"),
        ChannelPreset(name: "PULS 24 HD", url: "http://10.10.55.64:8001/1:0:19:14B8:407:1:C00000:0:0:0:"),
        ChannelPreset(name: "Sky Sport F1", url: "http://10.10.55.64:8001/1:0:19:11:6:85:C00000:0:0:0:"),
        ChannelPreset(name: "Sky Sport Bundesliga", url: "http://10.10.55.64:8001/1:0:19:69:C:85:C00000:0:0:0:"),
        ChannelPreset(name: "Sky Sport Premier League", url: "http://10.10.55.64:8001/1:0:19:91:4:85:C00000:0:0:0:")
    ]

    public init() {}

    public var body: some View {
        ZStack(alignment: .topLeading) {
            Color.black.ignoresSafeArea()

            // 1. Metal Video Stage
            MetalVideoStageView(pipeline: pipeline)
                .ignoresSafeArea()

            // 2. Telemetry HUD Overlay
            if showHUD {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        hudHeader

                        if let warning = pipeline.telemetry.display.validationWarning {
                            HStack(spacing: 8) {
                                Image(systemName: "exclamationmark.triangle.fill")
                                    .foregroundStyle(.yellow)
                                Text(warning)
                                    .font(.caption.weight(.bold))
                                    .foregroundStyle(.white)
                            }
                            .padding(8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(Color.red.opacity(0.85))
                            .cornerRadius(8)
                        } else if pipeline.telemetry.display.isDirect1080iVerified {
                            HStack(spacing: 8) {
                                Image(systemName: "checkmark.seal.fill")
                                    .foregroundStyle(.green)
                                Text("✅ Echter 1080i50 Direct-Stream (Kein Server-Transcode!)")
                                    .font(.caption.weight(.bold))
                                    .foregroundStyle(.white)
                            }
                            .padding(8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(Color.green.opacity(0.25))
                            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color.green.opacity(0.6), lineWidth: 1))
                            .cornerRadius(8)
                        }

                        Group {
                            hudSection(title: "STARTUP GATES (Time-To-First-Picture)") {
                                hudRow("TTFP (GPU Completed)", pipeline.telemetry.display.ttfpGpuCompletedMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpGpuCompletedMs) : (pipeline.telemetry.display.ttfpTotalMs > 0 ? String(format: "%.1f ms (Submitted)", pipeline.telemetry.display.ttfpTotalMs) : "Measuring…"), highlight: (pipeline.telemetry.display.ttfpGpuCompletedMs > 0 ? pipeline.telemetry.display.ttfpGpuCompletedMs : pipeline.telemetry.display.ttfpTotalMs) <= 800.0 && pipeline.telemetry.display.ttfpTotalMs > 0, alert: pipeline.telemetry.display.ttfpTotalMs > 1500.0)
                                hudRow("Performance Rating", pipeline.telemetry.display.ttfpRating, highlight: pipeline.telemetry.display.ttfpTotalMs <= 800.0 && pipeline.telemetry.display.ttfpTotalMs > 0, alert: pipeline.telemetry.display.ttfpTotalMs > 1500.0)
                                hudRow("t0→t1: Request→TS Data", pipeline.telemetry.display.ttfpNetworkMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpNetworkMs) : "Waiting…")
                                hudRow("t1→t2: TS→PAT/PMT", pipeline.telemetry.display.ttfpPsiMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpPsiMs) : "Waiting…")
                                hudRow("t2→t3: PSI→SPS/PPS", pipeline.telemetry.display.ttfpParamSetsMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpParamSetsMs) : "Waiting…")
                                hudRow("t3→t4: Params→First IDR", pipeline.telemetry.display.ttfpIdrMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpIdrMs) : "Waiting…")
                                hudRow("t4→t5: IDR→Decoded", pipeline.telemetry.display.ttfpDecodeMs > 0 ? String(format: "%.1f ms (≤100ms)", pipeline.telemetry.display.ttfpDecodeMs) : "Waiting…", highlight: pipeline.telemetry.display.ttfpDecodeMs <= 100.0 && pipeline.telemetry.display.ttfpDecodeMs > 0)
                                hudRow("t5→t6: Decoded→Submit", pipeline.telemetry.display.ttfpRenderMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpRenderMs) : "Waiting…")
                                hudRow("Early Glitches (2s)", "\(pipeline.telemetry.display.earlyStabilityIssues)", alert: pipeline.telemetry.display.earlyStabilityIssues > 0)
                            }

                            hudSection(title: "INPUT") {
                                hudRow("TS Bitrate", String(format: "%.1f kbps", pipeline.telemetry.display.tsBitrateKbps))
                                hudRow("Video PID", pipeline.telemetry.display.videoPID > 0 ? String(format: "0x%04X (%d)", pipeline.telemetry.display.videoPID, pipeline.telemetry.display.videoPID) : "Searching…")
                                hudRow("Continuity Errors", "\(pipeline.telemetry.display.continuityErrors)", alert: pipeline.telemetry.display.continuityErrors > 0)
                                hudRow("PES Errors", "\(pipeline.telemetry.display.pesErrors)", alert: pipeline.telemetry.display.pesErrors > 0)
                                hudRow("AUs ohne PTS", "\(pipeline.telemetry.display.accessUnitsWithoutPTS)", alert: pipeline.telemetry.display.accessUnitsWithoutPTS > 0)
                            }

                            hudSection(title: "CODEC") {
                                hudRow("Codec", pipeline.telemetry.display.codec)
                                hudRow("Resolution", pipeline.telemetry.display.videoWidth > 0 ? "\(pipeline.telemetry.display.videoWidth)x\(pipeline.telemetry.display.videoHeight)" : "Waiting…")
                                hudRow("Interlaced", pipeline.telemetry.display.isInterlaced ? "YES (1080i) ✅" : "NO (Progressive) ❌", highlight: pipeline.telemetry.display.isInterlaced, alert: !pipeline.telemetry.display.isInterlaced && pipeline.telemetry.display.videoWidth > 0)
                                hudRow("Field Order", pipeline.telemetry.display.fieldOrder, highlight: pipeline.telemetry.display.fieldOrder == "TFF")
                                hudRow("Source Field Rate", String(format: "%.1f fields/s", pipeline.telemetry.display.sourceFieldRate))
                                hudRow("Source Frame Rate", String(format: "%.1f fps", pipeline.telemetry.display.sourceFrameRate))
                            }

                            hudSection(title: "DECODER (Apple Silicon HW)") {
                                hudRow("VT Session", pipeline.telemetry.display.vtSessionActive ? "Active 🟢" : "Inactive ⚪️")
                                hudRow("HW Required", "YES (kVT...RequireHW)")
                                hudRow("HW Active", pipeline.telemetry.display.hwDecodeActive ? "YES (Verified 🚀)" : "Pending…", highlight: pipeline.telemetry.display.hwDecodeActive)
                                hudRow("Deinterlace", "Metal Bob (Pfad A)")
                                hudRow("Decoded AU", String(format: "%.1f AU/s", pipeline.telemetry.display.decodedFramesPerSec), alert: pipeline.telemetry.display.sourceFrameRate > 0 && abs(pipeline.telemetry.display.decodedFramesPerSec - pipeline.telemetry.display.sourceFrameRate) > 3.0)
                                hudRow("PTS Delta (Frame)", pipeline.telemetry.display.ptsProgressionMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ptsProgressionMs) : "Pending…", highlight: abs(pipeline.telemetry.display.ptsProgressionMs - 40.0) < 3.0)
                                hudRow("Decode Errors", "\(pipeline.telemetry.display.decodeErrors)", alert: pipeline.telemetry.display.decodeErrors > 0)
                                hudRow("Pipeline Mode", pipeline.telemetry.display.activeDecoderMode)
                            }

                            hudSection(title: "RENDER (Bob-Deinterlace, PTS-Scheduling)") {
                                hudRow("Top Fields", String(format: "%.1f /s (~25)", pipeline.telemetry.display.topFieldsPerSec), highlight: abs(pipeline.telemetry.display.topFieldsPerSec - 25.0) < 2.0)
                                hudRow("Bottom Fields", String(format: "%.1f /s (~25)", pipeline.telemetry.display.bottomFieldsPerSec), highlight: abs(pipeline.telemetry.display.bottomFieldsPerSec - 25.0) < 2.0)
                                hudRow("Fields Submitted", String(format: "%.1f /s (~50)", pipeline.telemetry.display.fieldsSubmittedPerSec), highlight: abs(pipeline.telemetry.display.fieldsSubmittedPerSec - 50.0) < 2.0)
                                hudRow("Field Cadence", pipeline.telemetry.display.fieldCadenceMs > 0 ? String(format: "%.2f ms (~20)", pipeline.telemetry.display.fieldCadenceMs) : "Pending…", highlight: abs(pipeline.telemetry.display.fieldCadenceMs - 20.0) < 1.0)
                                hudRow("Presentation Jitter", String(format: "%.2f ms", pipeline.telemetry.display.presentationJitterMs), alert: pipeline.telemetry.display.presentationJitterMs > 8.0)
                                // Expected: display rate minus field rate. Re-showing a
                                // field is how 50 fields/s fits a 60 or 120 Hz panel.
                                hudRow("Repeated Draws", String(format: "%.1f /s (Total: %d)", pipeline.telemetry.display.repeatedFieldsPerSec, pipeline.telemetry.display.repeatedFieldCount))
                                hudRow("Draws / Callbacks", String(format: "%.1f / %.1f /s", pipeline.telemetry.display.presentedFramesPerSec, pipeline.telemetry.display.displayCallbacksPerSec))
                                hudRow("Queue Depth", "\(pipeline.telemetry.display.queuedFieldCount) fields")
                                hudRow("Dropped Frames", "\(pipeline.telemetry.display.droppedFrames)", alert: pipeline.telemetry.display.droppedFrames > 0)
                                hudRow("Late Fields", "\(pipeline.telemetry.display.lateFrames)", alert: pipeline.telemetry.display.lateFrames > 0)
                            }

                            hudSection(title: "SYSTEM") {
                                hudRow("Thermal State", pipeline.telemetry.display.thermalState)
                                hudRow("Memory Usage", String(format: "%.1f MB", pipeline.telemetry.display.memoryUsageMB))
                            }
                        }

                    }
                    .padding(12)
                }
                .frame(maxWidth: 340)
                .background(.ultraThinMaterial)
                .cornerRadius(12)
                .padding(.top, 50)
                .padding(.leading, 16)
            }

            // 3. Bottom Control Bar
            VStack(spacing: 8) {
                Spacer()

                // Channel Presets Chips
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(presets) { preset in
                            Button {
                                streamURLString = preset.url
                                if let url = URL(string: preset.url) {
                                    pipeline.startStreaming(url: url)
                                    isStreaming = true
                                }
                            } label: {
                                Text(preset.name)
                                    .font(.caption.weight(streamURLString == preset.url ? .bold : .regular))
                                    .foregroundStyle(streamURLString == preset.url ? .black : .white)
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 5)
                                    .background(streamURLString == preset.url ? Color.orange : Color.white.opacity(0.2))
                                    .cornerRadius(12)
                            }
                        }
                    }
                    .padding(.horizontal)
                }

                HStack(spacing: 12) {
                    TextField("Stream URL", text: $streamURLString)
                        .textFieldStyle(.roundedBorder)
                        .font(.caption)
                        .autocapitalization(.none)
                        .disableAutocorrection(true)

                    Button(isStreaming ? "Stop" : "Start 1080i50") {
                        if isStreaming {
                            pipeline.stopStreaming()
                            isStreaming = false
                        } else {
                            if let url = URL(string: streamURLString) {
                                pipeline.startStreaming(url: url)
                                isStreaming = true
                            }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(isStreaming ? .red : .green)

                    Button {
                        showHUD.toggle()
                    } label: {
                        Image(systemName: showHUD ? "chart.bar.fill" : "chart.bar")
                    }
                    .buttonStyle(.bordered)
                }
                .padding()
                .background(.ultraThinMaterial)
            }
        }
        .onAppear {
            if !isStreaming, let url = URL(string: streamURLString) {
                pipeline.startStreaming(url: url)
                isStreaming = true
            }
        }
        .onDisappear {
            pipeline.stopStreaming()
            isStreaming = false
        }
    }

    private var hudHeader: some View {
        HStack {
            Text("⚡️ Native 1080p50 Video Telemetry")
                .font(.subheadline.weight(.bold))
                .foregroundStyle(.white)
            Spacer()
            Circle()
                .fill(isStreaming ? Color.green : Color.gray)
                .frame(width: 10, height: 10)
        }
    }

    private func hudSection<Content: View>(title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.system(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(Color.yellow)
            content()
        }
        .padding(8)
        .background(Color.black.opacity(0.4))
        .cornerRadius(6)
    }

    private func hudRow(_ label: String, _ value: String, highlight: Bool = false, alert: Bool = false) -> some View {
        HStack {
            Text(label)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(Color.gray)
            Spacer()
            Text(value)
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(alert ? Color.red : (highlight ? Color.green : Color.white))
        }
    }
}

private struct MetalVideoStageView: UIViewRepresentable {
    let pipeline: NativeTSVideoPipeline

    func makeUIView(context: Context) -> MetalVideoView {
        let view = MetalVideoView(frame: .zero)
        view.telemetry = pipeline.telemetry
        pipeline.renderView = view
        return view
    }

    func updateUIView(_ uiView: MetalVideoView, context: Context) {
        uiView.telemetry = pipeline.telemetry
    }
}
