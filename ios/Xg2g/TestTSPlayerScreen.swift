// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// SwiftUI screen to test and benchmark the Phase 1 1080i50 $\rightarrow$ 1080p50 VideoToolbox + Metal Vertical Slice.
public struct TestTSPlayerScreen: View {

    @StateObject private var pipeline = NativeTSVideoPipeline()
    @State private var streamURLString: String = "http://10.10.55.64:8001/1:0:19:14B8:407:1:C00000:0:0:0"
    @State private var isStreaming: Bool = false
    @State private var showHUD: Bool = true
    @State private var usePathB: Bool = false

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

                        Group {
                            hudSection(title: "INPUT") {
                                hudRow("TS Bitrate", String(format: "%.1f kbps", pipeline.telemetry.tsBitrateKbps))
                                hudRow("Video PID", pipeline.telemetry.videoPID > 0 ? String(format: "0x%04X (%d)", pipeline.telemetry.videoPID, pipeline.telemetry.videoPID) : "Searching…")
                                hudRow("Continuity Errors", "\(pipeline.telemetry.continuityErrors)", alert: pipeline.telemetry.continuityErrors > 0)
                                hudRow("PES Errors", "\(pipeline.telemetry.pesErrors)", alert: pipeline.telemetry.pesErrors > 0)
                            }

                            hudSection(title: "CODEC") {
                                hudRow("Codec", pipeline.telemetry.codec)
                                hudRow("Resolution", pipeline.telemetry.videoWidth > 0 ? "\(pipeline.telemetry.videoWidth)x\(pipeline.telemetry.videoHeight)" : "Waiting…")
                                hudRow("Interlaced", pipeline.telemetry.isInterlaced ? "YES (1080i)" : "NO (Progressive)", highlight: pipeline.telemetry.isInterlaced)
                                hudRow("Field Order", pipeline.telemetry.fieldOrder, highlight: pipeline.telemetry.fieldOrder == "TFF")
                                hudRow("Source Field Rate", String(format: "%.1f fields/s", pipeline.telemetry.sourceFieldRate))
                                hudRow("Source Frame Rate", String(format: "%.1f fps", pipeline.telemetry.sourceFrameRate))
                            }

                            hudSection(title: "DECODER (Apple Silicon HW)") {
                                hudRow("VT Session", pipeline.telemetry.vtSessionActive ? "Active 🟢" : "Inactive ⚪️")
                                hudRow("HW Required", "YES (kVT...RequireHW)")
                                hudRow("HW Active", pipeline.telemetry.hwDecodeActive ? "YES (Verified 🚀)" : "Pending…", highlight: pipeline.telemetry.hwDecodeActive)
                                hudRow("VT Deinterlace", pipeline.useNativeVTDeinterlace ? (pipeline.telemetry.vtDeinterlaceAccepted ? "Accepted 🟢" : "Unsupported ⚠️") : "Disabled ⚪️", highlight: pipeline.telemetry.vtDeinterlaceAccepted, alert: pipeline.useNativeVTDeinterlace && !pipeline.telemetry.vtDeinterlaceAccepted)
                                hudRow("Decoded Frames", String(format: "%.1f frames/s", pipeline.telemetry.decodedFramesPerSec))
                                hudRow("Decode Errors", "\(pipeline.telemetry.decodeErrors)", alert: pipeline.telemetry.decodeErrors > 0)
                                hudRow("Pipeline Mode", pipeline.telemetry.activeDecoderMode)
                            }

                            hudSection(title: "RENDER (Metal 50p Presentation)") {
                                hudRow("Generated Fields", String(format: "%.1f fields/s", pipeline.telemetry.generatedFieldsPerSec))
                                hudRow("Presented Frames", String(format: "%.1f fps", pipeline.telemetry.presentedFramesPerSec), highlight: abs(pipeline.telemetry.presentedFramesPerSec - 50.0) < 2.0)
                                hudRow("Display Callbacks", String(format: "%.1f /s", pipeline.telemetry.displayCallbacksPerSec))
                                hudRow("Dropped Frames", "\(pipeline.telemetry.droppedFrames)", alert: pipeline.telemetry.droppedFrames > 0)
                                hudRow("Presentation Jitter", String(format: "%.2f ms", pipeline.telemetry.presentationJitterMs))
                            }

                            hudSection(title: "SYSTEM") {
                                hudRow("Thermal State", pipeline.telemetry.thermalState)
                                hudRow("Memory Usage", String(format: "%.1f MB", pipeline.telemetry.memoryUsageMB))
                            }
                        }

                        // A/B Path Switch
                        HStack {
                            Toggle("Pfad B (VideoToolbox Deinterlace)", isOn: $usePathB)
                                .font(.caption.weight(.bold))
                                .foregroundStyle(.white)
                                .onChange(of: usePathB) { newValue in
                                    pipeline.useNativeVTDeinterlace = newValue
                                }
                        }
                        .padding(8)
                        .background(Color.black.opacity(0.6))
                        .cornerRadius(8)
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
            VStack {
                Spacer()
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
        .onDisappear {
            pipeline.stopStreaming()
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
