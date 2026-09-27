#!/usr/bin/env python3
"""
xcode-mcp.py - Turnkey CLI wrapper for Xcode 27 MCP Server & Tools.

Usage:
    python3 ios/scripts/xcode-mcp.py status
    python3 ios/scripts/xcode-mcp.py list-tools
    python3 ios/scripts/xcode-mcp.py call <ToolName> ['<json-arguments>']
    python3 ios/scripts/xcode-mcp.py build [--for-testing]
    python3 ios/scripts/xcode-mcp.py schemes
    python3 ios/scripts/xcode-mcp.py destinations
    python3 ios/scripts/xcode-mcp.py issues <relative-or-absolute-file-path>

Examples:
    python3 ios/scripts/xcode-mcp.py status
    python3 ios/scripts/xcode-mcp.py call BuildProject '{"buildForTesting": false}'
    python3 ios/scripts/xcode-mcp.py call XcodeListSchemes '{}'
    python3 ios/scripts/xcode-mcp.py issues ios/Xg2g/App/RootView.swift
"""

import sys
import os
import json
import subprocess
import argparse

DEFAULT_PROJECT = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "Xg2g.xcodeproj")
)

class XcodeMCPClient:
    def __init__(self, project_path=DEFAULT_PROJECT):
        self.project_path = os.path.abspath(project_path)
        self.process = None
        self.req_id = 0
        self.workspace_id = None
        self.active_destination = None
        self.active_scheme = None

    def start(self):
        # Ensure MCP server is running and project is open
        res = subprocess.run(
            ["xcrun", "mcp-server", "open", self.project_path],
            capture_output=True,
            text=True
        )
        if res.returncode != 0 and res.stderr:
            sys.stderr.write(f"Warning from mcp-server open: {res.stderr}\n")

        self.process = subprocess.Popen(
            ["xcrun", "mcpbridge"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True
        )

        # 1. Initialize
        self._send_raw({
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "xg2g-agent-helper", "version": "1.0"}
            }
        })
        init_resp = self._read_raw()
        if "error" in init_resp:
            raise RuntimeError(f"MCP Initialize failed: {init_resp['error']}")

        # 2. Initialized notification (MUST NOT have an id)
        self._send_raw({
            "jsonrpc": "2.0",
            "method": "notifications/initialized",
            "params": {}
        })

        # 3. Open Workspace to approve folder and get workspaceIdentifier
        open_res = self.call_tool("XcodeOpenWorkspace", {"path": self.project_path})
        if "structuredContent" in open_res:
            self.workspace_id = open_res["structuredContent"].get("workspaceIdentifier")
            self.active_scheme = open_res["structuredContent"].get("activeScheme")
            self.active_destination = open_res["structuredContent"].get("activeRunDestination")
        return self

    def _next_id(self):
        self.req_id += 1
        return self.req_id

    def _send_raw(self, msg):
        line = json.dumps(msg) + "\n"
        self.process.stdin.write(line)
        self.process.stdin.flush()

    def _read_raw(self):
        line = self.process.stdout.readline()
        if not line:
            stderr = self.process.stderr.read()
            raise RuntimeError(f"MCP bridge process closed unexpectedly: {stderr}")
        return json.loads(line)

    def call_tool(self, name, arguments=None):
        if arguments is None:
            arguments = {}
        if self.workspace_id and "workspaceIdentifier" not in arguments:
            arguments["workspaceIdentifier"] = self.workspace_id

        msg_id = self._next_id()
        self._send_raw({
            "jsonrpc": "2.0",
            "id": msg_id,
            "method": "tools/call",
            "params": {
                "name": name,
                "arguments": arguments
            }
        })

        resp = self._read_raw()
        if "error" in resp:
            raise RuntimeError(f"Tool {name} error: {resp['error']}")

        result = resp.get("result", {})
        if result.get("isError"):
            content = result.get("content", [{}])
            err_msg = content[0].get("text", "Unknown error") if content else "Unknown error"
            raise RuntimeError(f"Tool {name} failed: {err_msg}")

        return result

    def list_tools(self):
        msg_id = self._next_id()
        self._send_raw({
            "jsonrpc": "2.0",
            "id": msg_id,
            "method": "tools/list",
            "params": {}
        })
        resp = self._read_raw()
        return resp.get("result", {}).get("tools", [])

    def close(self):
        if self.process:
            self.process.stdin.close()
            self.process.terminate()
            self.process.wait()
            self.process = None

    def __enter__(self):
        return self.start()

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

def main():
    parser = argparse.ArgumentParser(description="Xcode 27 MCP Tool Helper for xg2g")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # status
    subparsers.add_parser("status", help="Print status of Xcode MCP server")

    # list-tools
    subparsers.add_parser("list-tools", help="List all available Xcode MCP tools")

    # schemes
    subparsers.add_parser("schemes", help="List active schemes for project")

    # destinations
    subparsers.add_parser("destinations", help="List available run destinations (simulators/devices)")

    # build
    build_parser = subparsers.add_parser("build", help="Build project via Xcode MCP")
    build_parser.add_argument("--for-testing", action="store_true", help="Build for testing")

    # issues
    issues_parser = subparsers.add_parser("issues", help="Check compiler diagnostics for a file")
    issues_parser.add_argument("file", help="Path to file to check")

    # call
    call_parser = subparsers.add_parser("call", help="Call a generic Xcode MCP tool")
    call_parser.add_argument("tool", help="Name of MCP tool")
    call_parser.add_argument("args", nargs="?", default="{}", help="JSON string of tool arguments")

    args = parser.parse_args()

    if args.command == "status":
        res = subprocess.run(["xcrun", "mcp-server", "status"], capture_output=True, text=True)
        print(res.stdout)
        if res.stderr:
            sys.stderr.write(res.stderr + "\n")
        return

    with XcodeMCPClient() as client:
        if args.command == "list-tools":
            tools = client.list_tools()
            print(f"Discovered {len(tools)} Xcode MCP tools:")
            for t in sorted(tools, key=lambda x: x["name"]):
                print(f"  - {t['name']:36} : {t.get('description', '').splitlines()[0][:70]}")

        elif args.command == "schemes":
            res = client.call_tool("XcodeListSchemes")
            print(json.dumps(res.get("structuredContent", res.get("content", [])), indent=2))

        elif args.command == "destinations":
            res = client.call_tool("XcodeListRunDestinations")
            print(json.dumps(res.get("structuredContent", res.get("content", [])), indent=2))

        elif args.command == "build":
            print(f"Building project (forTesting={args.for_testing})...")
            res = client.call_tool("BuildProject", {"buildForTesting": args.for_testing})
            print(json.dumps(res.get("structuredContent", res.get("content", [])), indent=2))

        elif args.command == "issues":
            file_path = os.path.abspath(args.file)
            res = client.call_tool("XcodeRefreshCodeIssuesInFile", {"filePath": file_path})
            print(json.dumps(res.get("structuredContent", res.get("content", [])), indent=2))

        elif args.command == "call":
            try:
                tool_args = json.loads(args.args)
            except Exception as e:
                sys.stderr.write(f"Invalid JSON in args: {e}\n")
                sys.exit(1)
            res = client.call_tool(args.tool, tool_args)
            if "structuredContent" in res:
                print(json.dumps(res["structuredContent"], indent=2))
            else:
                print(json.dumps(res, indent=2))

if __name__ == "__main__":
    main()
