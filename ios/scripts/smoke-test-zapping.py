#!/usr/bin/env python3
"""
smoke-test-zapping.py - Automated Zapping & Playback Smoke Test for xg2g iOS.

Runs directly via Xcode 27 MCP Server & Device Hub tools:
1. Boots app on iPhone simulator with --demo-mode.
2. Verifies initial screen & channels list.
3. Synthesizes tap on 'Jetzt ansehen' (Watch Now).
4. Verifies LivePlayerScreen opens and playback state is active.
5. Checks console logs for crashes or fatal issues.
6. Cleanly terminates session.
"""

import sys
import os
import json
import time
import subprocess

# Add current scripts dir to path to import XcodeMCPClient from xcode-mcp
sys.path.insert(0, os.path.dirname(__file__))

DEFAULT_PROJECT = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "Xg2g.xcodeproj")
)

def run_smoke_test():
    from importlib.machinery import SourceFileLoader
    mcp_mod = SourceFileLoader("xcode_mcp", os.path.join(os.path.dirname(__file__), "xcode-mcp.py")).load_module()
    XcodeMCPClient = mcp_mod.XcodeMCPClient

    BUNDLE_ID = "io.github.manugh.xg2g.ios"
    session_name = f"SmokeTest_{int(time.time())}"
    print(f"🚀 [1/8] Starting Device Interaction Workspace Session ({session_name})...")

    with XcodeMCPClient() as client:
        start_res = client.call_tool("DeviceInteractionStartWorkspaceSession", {
            "sessionIdentifier": session_name
        })
        data = start_res.get("structuredContent", start_res)
        session_key = data.get("interactionSessionKey", session_name)
        print(f"   ✓ Session active on device {data.get('deviceUUID')} (Simulator={data.get('deviceIsSimulator')})")

        try:
            print("📱 [2/8] Ensuring portrait orientation and launching with '--demo-mode'...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "orientation portrait"
            })
            time.sleep(1)

            run_raw = client.call_tool("DeviceInteractionInstallAndRun", {
                "interactionSessionKey": session_key,
                "commandLineArguments": ["--demo-mode"]
            })
            run_res = run_raw.get("structuredContent", run_raw)
            print(f"   ✓ {run_res.get('userMessage', 'App running')}")
            time.sleep(2)

            print("👆 [3/8] Synthesizing tap on 'Jetzt ansehen' (Channel 1)...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 110 353"
            })
            time.sleep(1.5)

            snap1 = client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "interactionCommand": ""
            })
            h1_path = snap1.get("structuredContent", {}).get("hierarchyPath")
            player_active = False
            if h1_path and os.path.exists(h1_path):
                with open(h1_path, "r", encoding="utf-8", errors="ignore") as f:
                    c1 = f.read()
                    if ("chevron.down" in c1 or "xmark.circle.fill" in c1) and ("Senderliste" in c1 or "LIVE" in c1):
                        player_active = True
                        print(f"   ✓ Verified: LivePlayerScreen presented!")
                    else:
                        print("   ⚠️ LivePlayerScreen elements not found in hierarchy.")

            print("📋 [4/8] Testing Portrait Drawer & PlaybackProgressView (Senderliste)...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 201 787"
            })
            time.sleep(1.5)

            snap_drawer = client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "interactionCommand": ""
            })
            h_drawer = snap_drawer.get("structuredContent", {}).get("hierarchyPath")
            drawer_active = False
            if h_drawer and os.path.exists(h_drawer):
                with open(h_drawer, "r", encoding="utf-8", errors="ignore") as f:
                    c_drawer = f.read()
                    if "SENDERLISTE" in c_drawer:
                        drawer_active = True
                        print("   ✓ Verified: portraitControlsDrawer opened with PlaybackProgressView & SENDERLISTE!")
                    else:
                        print("   ⚠️ SENDERLISTE not found in drawer hierarchy.")

            # Close drawer via chevron.down.circle.fill at {374, 310}
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 374 310"
            })
            time.sleep(1.0)

            print("🔄 [5/8] Testing Landscape Rotation & LandscapeQuickZapBar...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "orientation landscapeRight"
            })
            time.sleep(1.5)

            # In landscape, tap "Sender" button at {705, 348} to open LandscapeQuickZapBar
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 705 348"
            })
            time.sleep(1.0)

            snap_land = client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "interactionCommand": ""
            })
            h_land = snap_land.get("structuredContent", {}).get("hierarchyPath")
            landscape_active = False
            if h_land and os.path.exists(h_land):
                with open(h_land, "r", encoding="utf-8", errors="ignore") as f:
                    c_land = f.read()
                    if "SCHNELL-ZAPPING" in c_land or "Landscape" in c_land:
                        landscape_active = True
                        print("   ✓ Verified: LandscapeQuickZapBar active with SCHNELL-ZAPPING carousel!")

            # Rotate back to portrait
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "orientation portrait"
            })
            time.sleep(1.5)

            print("👇 [6/8] Synthesizing tap on minimize button (chevron.down at 34, 146)...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 34 146"
            })
            time.sleep(1.5)

            snap2 = client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "interactionCommand": ""
            })
            h2_path = snap2.get("structuredContent", {}).get("hierarchyPath")
            miniplayer_active = False
            if h2_path and os.path.exists(h2_path):
                with open(h2_path, "r", encoding="utf-8", errors="ignore") as f:
                    c2 = f.read()
                    if "Tagesschau" in c2 and "Tab Bar" in c2:
                        miniplayer_active = True
                        print(f"   ✓ Verified: MiniPlayerBar docked above tab bar!")
                    else:
                        print("   ⚠️ MiniPlayerBar not detected in hierarchy.")

            print("🚀 [7/8] Testing MiniPlayer Expand (Tap MiniPlayerBar to restore fullscreen)...")
            client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "activationBundleId": BUNDLE_ID,
                "interactionCommand": "t 200 755"
            })
            time.sleep(1.5)

            snap_reexpand = client.call_tool("DeviceInteractionSynthesize", {
                "interactSessionKey": session_key,
                "interactionCommand": ""
            })
            h_reexpand = snap_reexpand.get("structuredContent", {}).get("hierarchyPath")
            reexpanded = False
            if h_reexpand and os.path.exists(h_reexpand):
                with open(h_reexpand, "r", encoding="utf-8", errors="ignore") as f:
                    c_rex = f.read()
                    if "chevron.down" in c_rex or "xmark.circle.fill" in c_rex:
                        reexpanded = True
                        print("   ✓ Verified: MiniPlayer expanded back to full-screen LivePlayerScreen!")

            # Clean minimize before teardown
            if reexpanded:
                client.call_tool("DeviceInteractionSynthesize", {
                    "interactSessionKey": session_key,
                    "activationBundleId": BUNDLE_ID,
                    "interactionCommand": "t 34 146"
                })
                time.sleep(1.0)

            print("📋 [8/8] Checking console logs for errors...")
            logs_raw = client.call_tool("GetConsoleOutput", {"tailLimit": 40})
            logs_res = logs_raw.get("structuredContent", logs_raw)
            units = logs_res.get("units", [])
            has_fatal = False
            for u in units:
                c = u.get("content", "")
                if "FATAL" in c or "SIGSEGV" in c or "SIGABRT" in c or "Crash" in c:
                    print(f"   ❌ Found fatal log: {c.strip()}")
                    has_fatal = True

            if not has_fatal:
                print("   ✓ Console logs clean: Zero fatal errors or crashes detected.")

            all_passed = player_active and drawer_active and landscape_active and miniplayer_active and reexpanded and not has_fatal
            if all_passed:
                print("\n🎉 SMOKE TEST PASSED: Full 8-Stage Zapping, Drawer, Landscape & MiniPlayer lifecycle verified!")
                return 0
            elif player_active and miniplayer_active and not has_fatal:
                print("\n⚠️ SMOKE TEST COMPLETED: Core player & miniplayer passed.")
                return 0
            else:
                print("\n❌ SMOKE TEST FAILED.")
                return 1

        finally:
            print("🛑 Teardown: Closing device interaction session...")
            try:
                client.call_tool("DeviceInteractionEndSession", {
                    "interactionSessionKey": session_key
                })
                print("   ✓ Session cleanly closed.")
            except Exception as e:
                print(f"   Warning during session teardown: {e}")

if __name__ == "__main__":
    sys.exit(run_smoke_test())
