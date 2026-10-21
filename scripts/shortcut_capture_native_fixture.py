#!/usr/bin/env python3
"""Local synthetic API for an isolated Shortcuts copy; never a real account.

Run with --scenario success|ack-false|api-error|disconnect|retry. The caller
must use the fake credential and example.com URL below. Observe the Shortcuts
prompt, then inspect stdout for requests; this fixture alone is not native proof.
"""
import argparse
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit

FAKE_TOKEN = 'a' * 64
TEST_URL = 'https://example.com/rss-pal-native-smoke'


class Fixture(HTTPServer):
    def __init__(self, address, scenario='success'):
        super().__init__(address, Handler)
        self.scenario = scenario
        self.captures = []
        self.saved_note = ''
        self.last_note = ''


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def respond(self, status, result):
        payload = json.dumps(result, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_POST(self):
        try:
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 < size < 65536:
                raise ValueError('Invalid fixture request size')
            body = json.loads(self.rfile.read(size))
        except (ValueError, TypeError):
            self.respond(400, {'error': 'Invalid synthetic request'})
            return
        if self.path == '/api/shortcuts/connect':
            self.respond(200, {'token': FAKE_TOKEN}) if body == {'code': 'b' * 64} else self.respond(401, {'error': 'Use the synthetic pairing code'})
            return
        if self.path != '/api/shortcuts/capture':
            self.respond(404, {'error': 'Unknown synthetic endpoint'})
            return
        parsed = urlsplit(body.get('url', ''))
        if self.headers.get('Authorization') != 'Bearer ' + FAKE_TOKEN or (parsed.scheme, parsed.netloc, parsed.path) != ('https', 'example.com', '/rss-pal-native-smoke'):
            self.respond(400, {'error': 'Use the synthetic credential and example.com fixture URL'})
            return
        note = body.get('capture_note')
        if not isinstance(note, str):
            self.respond(400, {'error': 'capture_note must be present as a string'})
            return
        self.server.captures.append(body)
        scenario = self.server.scenario
        print(json.dumps({'request': len(self.server.captures), 'url': body['url'], 'capture_note': note, 'scenario': scenario}, ensure_ascii=False), flush=True)
        if scenario == 'disconnect':
            self.connection.close()
            return
        if scenario == 'api-error' or (scenario == 'retry' and len(self.server.captures) == 1):
            self.respond(503, {'error': 'Synthetic retryable failure'})
            return
        saved = bool(note.strip()) and scenario != 'ack-false'
        if saved and note != self.server.last_note:
            self.server.saved_note += ('\n\n' if self.server.saved_note else '') + note
            self.server.last_note = note
        self.respond(202, {'parent_article_id': 123, 'status': 'drafted', 'message': '已加入原文章待抓取列表，返回原文章后可确认并开始抓取。', 'note_saved': saved})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8877)
    parser.add_argument('--scenario', choices=['success', 'ack-false', 'api-error', 'disconnect', 'retry'], default='success')
    options = parser.parse_args()
    print('Synthetic shortcut fixture on 127.0.0.1:' + str(options.port), flush=True)
    Fixture(('127.0.0.1', options.port), options.scenario).serve_forever()
