#!/usr/bin/env python3
"""Re-export the credential-free native workflow used by the installer.

Native action formats were exported from Shortcuts.app. The direct-capture
workflow was imported and executed on macOS against an isolated API. The
installer's Apple record must match this source before release.

python3 scripts/build_iphone_shortcut.py /tmp/rss-pal-unsigned.shortcut
shortcuts sign --mode anyone --input /tmp/rss-pal-unsigned.shortcut \
  --output /tmp/rss-pal-signed.shortcut

A modified file needs a new Apple share link before updating the installer URL.
"""
import plistlib
import sys
from pathlib import Path

SOURCE = Path(__file__).resolve().parent.parent / 'shortcuts/rss-pal-capture.plist'


def workflow():
    return plistlib.loads(SOURCE.read_bytes())


if __name__ == '__main__':
    destination = Path(sys.argv[1])
    destination.write_bytes(plistlib.dumps(workflow(), fmt=plistlib.FMT_BINARY))
    print(f'Wrote credential-free shortcut: {destination}')
