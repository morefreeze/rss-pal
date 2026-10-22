#!/usr/bin/env python3
"""Refresh the complete sanitized Folo/RSSHub directory embedded in the API.

Reproduce an archived input with --input FILE --fetched-at RFC3339. Production
refresh and this generator use the same Go field whitelist and normalization.
"""
import argparse
import pathlib
import subprocess
import urllib.request

SOURCE = "https://docs.rsshub.app/routes.json"
LIMIT = 20 << 20
ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=pathlib.Path, help="archived upstream JSON")
    parser.add_argument("--fetched-at", help="original fetch time (RFC3339)")
    args = parser.parse_args()
    if args.input:
        with args.input.open("rb") as source:
            data = source.read(LIMIT + 1)
    else:
        # The URL is fixed and redirects are disallowed, matching the API client.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        with urllib.request.build_opener(NoRedirect).open(SOURCE, timeout=20) as source:
            data = source.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise SystemExit("directory exceeds 20 MiB")
    command = ["go", "run", "./internal/sourcedirectory/cmd/sync"]
    if args.fetched_at:
        command += ["--fetched-at", args.fetched_at]
    result = subprocess.run(command, input=data, cwd=ROOT / "backend", capture_output=True, check=True)
    target = ROOT / "backend/internal/sourcedirectory/snapshot.json"
    target.write_bytes(result.stdout)
    print(f"Updated {target} ({len(result.stdout)} bytes)")


if __name__ == "__main__":
    main()
