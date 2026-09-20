#!/usr/bin/env python3
"""
tv-remote.py - Drive the Apple TV simulator's Siri Remote through Xcode 27's MCP.

Usage:
    python3 ios/scripts/tv-remote.py '<command>' ['<command>' ...]

Each argument is one DeviceInteractionSynthesize command; an empty string only
captures. Commands chain with spaces, e.g. 'r down r down r select w 5'.
tvOS commands: r up|down|left|right|select|menu|home|playpause, w <seconds>,
k <text>. After each command the focused element from the accessibility
hierarchy is printed; the last screenshot and hierarchy paths follow.

Environment:
    TV_UDID     simulator UDID (default: the first booted tvOS device)
    TV_PROJECT  .xcodeproj to open (default: ios/Xg2g.xcodeproj)
    TV_SESSION  session identifier; default is unique per run, because Xcode
                refuses an identifier that is in use or was used recently
"""

import importlib.util
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))


def load_client():
    spec = importlib.util.spec_from_file_location("xcode_mcp", os.path.join(HERE, "xcode-mcp.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.XcodeMCPClient


def booted_tvos_udid():
    out = subprocess.run(
        ["xcrun", "simctl", "list", "devices", "booted", "--json"],
        capture_output=True, text=True, check=True,
    ).stdout
    devices = json.loads(out)["devices"]
    for runtime, entries in devices.items():
        if "tvOS" in runtime:
            for entry in entries:
                if entry.get("state") == "Booted":
                    return entry["udid"]
    sys.exit("No booted tvOS simulator; boot one with `xcrun simctl boot <udid>`.")


def text_of(result):
    return "".join(item.get("text", "") for item in result.get("content", []) if item.get("type") == "text")


def main():
    commands = sys.argv[1:] or [""]
    udid = os.environ.get("TV_UDID") or booted_tvos_udid()
    project = os.environ.get("TV_PROJECT") or os.path.join(HERE, "..", "Xg2g.xcodeproj")
    key = os.environ.get("TV_SESSION") or f"TV Remote {int(time.time()) % 100000}"

    client = load_client()(project_path=project)
    client.start()
    try:
        client.call_tool("DeviceInteractionStartSession", {"deviceIdentifier": udid, "sessionIdentifier": key})
        last = None
        for command in commands:
            result = client.call_tool(
                "DeviceInteractionSynthesize",
                {"interactSessionKey": key, "interactionCommand": command},
            )
            last = json.loads(text_of(result))
            with open(last["hierarchyPath"]) as handle:
                lines = handle.read().splitlines()
            focused = [line.strip() for line in lines if "Focused" in line]
            label = focused[0][:140] if focused else "NONE"
            print(f"[{command or 'capture'}] lines={len(lines)} focused={label}")
        if last:
            print("screenshot:", last["screenshotPath"])
            print("hierarchy:", last["hierarchyPath"])
    finally:
        try:
            client.call_tool("DeviceInteractionEndSession", {"interactionSessionKey": key})
        except Exception:
            pass
        client.close()


if __name__ == "__main__":
    main()
