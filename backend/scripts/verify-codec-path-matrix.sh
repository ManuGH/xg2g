#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
BACKEND_ROOT="${REPO_ROOT}/backend"

run() {
  echo "==> $*"
  "$@"
}

cd "${BACKEND_ROOT}"

echo "Verifying codec/path matrix: x264, x265/HEVC, AV1"

run go test ./internal/infra/media/ffmpeg -run 'TestBuildArgs_(CPUProfileDriven|VaapiH264Deinterlace|VaapiEncodeOnlyUsesCPUDecodeAndHWUpload|HWProfileWithExplicitCPUFallbackDoesNotAutoPromoteHardware|VaapiHEVC|VaapiHEVCMPEGTSDoesNotEmitHVC1OrFMP4Init|VaapiHEVCDeinterlaceFallsBackToH264UntilVerified|VaapiHEVCDeinterlaceUsesEncodeOnlyPathWhenVerified|VaapiHEVCDeinterlaceUsesFullPathWhenVerified|AV1HWFallbackWithoutProfileMutation|AV1HWUsesMPEGTSSegmentsWhenExperimentalFlagEnabled|AV1HWInterlacedFallsBackToH264WhenPathUnverified|AV1HWInterlacedExperimentalOverrideUsesAV1EncodeOnlyPath)$' -count=1
run go test ./internal/infra/media/ffmpeg -run 'TestBuildArgsWithPlan_AV1HWProgressiveProbePreservesAV1|TestMonitorProcess_RuntimePathCorrectnessMarks(BrokenAndStopsProcess|Verified)$' -count=1
run go test ./internal/control/http/v3/intents -run 'Test(ApplyClientCompatibilityPolicy_IOSNativeHEVC(DemotesToEncodeOnly|KeepsFullVAAPIWhenConfigured|FallsBackToCPUWhenConfigured)|Service_ProcessIntent_Start(UsesEncodeOnlyForIOSNativeHEVC|AllowsFullVAAPIForIOSNativeHEVCWhenConfigured|UsesProgressiveScanTruthForAV1HW))$' -count=1
run go test ./internal/control/http/v3 -run 'TestHandleV3Intents_PlaybackModeNativeHLSUsesFMP4ForQualifiedHEVCOnIOSSafariNative|TestHandleV3Intents_PlaybackModeNativeHLSUsesAV1FMP4ForRuntimeCapableIOSSafariNative|TestHandleV3Intents_PlaybackModeTranscodeUsesEncodeOnlyHEVCForIOSSafari|TestHandleV3Intents_PlaybackModeTranscodeUsesAV1FMP4ForIOSSafari' -count=1

echo "✅ codec/path matrix verifier passed"
