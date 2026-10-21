#!/usr/bin/env python3
"""Re-export the credential-free native workflow used by the installer.

Native action formats were exported from Shortcuts.app. The direct-capture
workflow was imported and executed on macOS against an isolated API. Optional
capture-note input and durable retry actions are still unimplemented, pending
official native editor formats and execution verification after Mac unlock.
Any resulting update requires a new Apple share record. The current installer
retains the previously published compatible link, which only saves the URL:
https://www.icloud.com/shortcuts/98ef2f6cfbe740e092908ab72ea9240e

python3 scripts/build_iphone_shortcut.py /tmp/rss-pal-unsigned.shortcut
shortcuts sign --mode anyone --input /tmp/rss-pal-unsigned.shortcut \
  --output /tmp/rss-pal-signed.shortcut

A modified file needs a new Apple share link before updating the installer URL.
"""
import argparse
import copy
import plistlib
import re
from pathlib import Path
from urllib.parse import urlsplit

SOURCE = Path(__file__).resolve().parent.parent / 'shortcuts/rss-pal-capture.plist'


def workflow():
    return plistlib.loads(SOURCE.read_bytes())


def native_smoke_workflow(source, server, connection_file, draft_prefix):
    """Isolate native execution from every real connection and saved note.

    This only rewrites our known file actions and literal API origin. It never
    reads a connection file or accesses the Shortcuts application database.
    Import/run the resulting copy with the official Shortcuts app.
    """
    parsed = urlsplit(server)
    if parsed.scheme != 'http' or parsed.hostname not in {'127.0.0.1', 'localhost'} or parsed.path not in {'', '/'} or parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError('Native smoke server must be a localhost HTTP origin')
    for value in (connection_file, draft_prefix):
        if not re.fullmatch(r'rss-pal-native-smoke-[A-Za-z0-9_-]+', value):
            raise ValueError('Use a unique rss-pal-native-smoke- basename')
    result = copy.deepcopy(source)

    def rewrite(value):
        if isinstance(value, dict):
            return {key: rewrite(item) for key, item in value.items()}
        if isinstance(value, list):
            return [rewrite(item) for item in value]
        if isinstance(value, str):
            return value.replace('https://rss.morefreeze.top', server.rstrip('/')).replace('rss-pal-connection.txt', connection_file + '.txt').replace('rss-pal-capture-draft-', draft_prefix + '-')
        return value

    result = rewrite(result)
    result['WFWorkflowName'] = 'RSS Pal Native Smoke'
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('destination', type=Path)
    parser.add_argument('--native-smoke-server')
    parser.add_argument('--native-smoke-id', help='Unique test-only basename suffix')
    args = parser.parse_args()
    result = workflow()
    if args.native_smoke_server or args.native_smoke_id:
        if not (args.native_smoke_server and args.native_smoke_id):
            parser.error('Both native smoke options are required')
        prefix = 'rss-pal-native-smoke-' + args.native_smoke_id
        result = native_smoke_workflow(result, args.native_smoke_server, prefix + '-connection', prefix + '-draft')
    destination = args.destination
    destination.write_bytes(plistlib.dumps(result, fmt=plistlib.FMT_BINARY))
    print(f'Wrote credential-free shortcut: {destination}')
