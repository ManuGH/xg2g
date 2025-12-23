from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from urllib.parse import parse_qs, urlparse

class MockHandler(BaseHTTPRequestHandler):
    current_service = "1:0:0:0:0:0:0:0:0:0:" # Default empty/idle

    def do_GET(self):
        print(f"MockRequest: {self.path}")
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.end_headers()

        response = {}

        if "/api/zap" in self.path:
            # Parse query param sRef
            query = parse_qs(urlparse(self.path).query)
            if 'sRef' in query:
                MockHandler.current_service = query['sRef'][0]
                print(f"Zapped to: {MockHandler.current_service}")
            response = {"result": True, "message": "Zapped"}

        elif "/api/getcurrent" in self.path:
            response = {
                "result": True,
                "info": {
                    "ref": MockHandler.current_service, # Key changed from serviceReference
                    "name": "Mock Service",
                    "provider": "Mock Provider"
                }
            }
        
        elif "/api/signal" in self.path:
            response = {
                "result": True,
                "lock": True, # Key changed from locked
                "snr": 85,
                "agc": 90,
                "ber": 0
            }

        elif "/api/bouquets" in self.path:
            response = {"bouquets": []}

        else:
            response = {"result": True}

        self.wfile.write(json.dumps(response).encode())

print("Starting Stateful Mock Enigma2 on :8001")
HTTPServer(('0.0.0.0', 8001), MockHandler).serve_forever()
