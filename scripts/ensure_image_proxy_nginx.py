#!/usr/bin/env python3
"""Patch the frontend bind source without replacing it with stock nginx.conf.

Only a recognized flat API proxy can supply a new exact image location. Every
other byte is preserved. stdout is empty for no change, or the backup pathname
after an atomic replacement (which requires frontend container recreation).
"""
import dataclasses
import datetime
import os
import pathlib
import re
import stat
import sys
import tempfile


class Refused(ValueError):
    pass


@dataclasses.dataclass
class Node:
    words: list
    start: int
    end: int
    opening: int = -1
    children: list = dataclasses.field(default_factory=list)


# Preserve token offsets, quoted values, escaped characters, and comments.
# Unsupported or malformed syntax is rejected rather than partially rewritten.
TOKEN = re.compile(r'''\s+|\#[^\n]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[{};]|(?:\\.|[^\s{};\#"'])+''')


def parse(source):
    tokens = []
    position = 0
    for match in TOKEN.finditer(source):
        if match.start() != position:
            raise Refused("unrecognized nginx syntax")
        position = match.end()
        token = match.group()
        if not token.isspace() and not token.startswith("#"):
            tokens.append((token, match.start(), match.end()))
    if position != len(source):
        raise Refused("unrecognized nginx syntax")
    cursor = 0

    def block(nested=False):
        nonlocal cursor
        nodes, words, start = [], [], 0
        while cursor < len(tokens):
            token, begin, end = tokens[cursor]
            cursor += 1
            if token == "}":
                if words or not nested:
                    raise Refused("unbalanced nginx block")
                return nodes, end
            if token in ("{", ";"):
                if not words:
                    raise Refused("empty nginx directive")
                node = Node(words, start, end)
                if token == "{":
                    node.opening = begin
                    node.children, node.end = block(True)
                nodes.append(node)
                words = []
            else:
                if not words:
                    start = begin
                words.append(token)
        if nested or words:
            raise Refused("unterminated nginx directive")
        return nodes, len(source)

    return block()[0]


ALLOWED = {"set", "proxy_pass", "proxy_http_version", "proxy_set_header", "proxy_cache_bypass",
           "proxy_read_timeout", "proxy_connect_timeout", "proxy_send_timeout"}


def proxy_contract(node):
    if any(child.opening >= 0 or child.words[0] not in ALLOWED for child in node.children):
        raise Refused("unrecognized API proxy directives")
    directives = {}
    for child in node.children:
        directives.setdefault(child.words[0], []).append(child)
    passes = directives.get("proxy_pass", [])
    if len(passes) != 1 or len(passes[0].words) != 2:
        raise Refused("API proxy needs one explicit upstream")
    upstream = passes[0].words[1]
    settings = directives.get("set", [])
    if upstream.startswith("$"):
        if len(settings) != 1 or len(settings[0].words) != 3 or settings[0].words[1] != upstream:
            raise Refused("API upstream variable is not defined locally")
        upstream = settings[0].words[2]
    elif settings:
        raise Refused("unrecognized API variable settings")
    # A URI suffix, even '/', changes proxy_pass's prefix replacement when
    # copied into an exact location. Accept origin-only upstreams, never infer
    # how a private path-rewriting proxy should be transformed.
    if not re.fullmatch(r"https?://[A-Za-z0-9_.:\[\]-]+(?::[0-9]+)?", upstream):
        raise Refused("API upstream must be an origin without URI components")
    headers = {}
    for header in directives.get("proxy_set_header", []):
        if len(header.words) != 3 or header.words[1].lower() in headers:
            raise Refused("ambiguous API headers")
        headers[header.words[1].lower()] = header.words[2]
    if not {"host", "x-real-ip", "x-forwarded-for"}.issubset(headers):
        raise Refused("API identity headers are incomplete")
    for name in ALLOWED - {"set", "proxy_pass", "proxy_set_header"}:
        if len(directives.get(name, [])) > 1:
            raise Refused("duplicate API proxy directive")
    timeouts = directives.get("proxy_read_timeout", [])
    if len(timeouts) != 1 or len(timeouts[0].words) != 2 or not re.fullmatch(r"\d+[smh]?", timeouts[0].words[1]):
        raise Refused("API read timeout is not explicit")
    return upstream, headers, timeouts[0]


IMAGE_PATH = "/api/proxy/image"


def location_token(token):
    if token[0] in ("'", '"'):
        # nginx and Python do not share string/regex escape semantics. Simple
        # quoted tokens are unambiguous; escaped quoted tokens need review.
        if token[-1] != token[0] or "\\" in token[1:-1]:
            raise Refused("unsupported escaped location token")
        return token[1:-1]
    return token


def regex_matches_image(pattern, insensitive):
    # Only a small ASCII regex subset has equivalent nginx PCRE/Python
    # semantics here. Inline flags, lookarounds, verbs, character classes,
    # backreferences and PCRE escapes are unknown, even if Python accepts them.
    # Retain the established unquoted static-extension regex and simple
    # unrelated path regexes; refuse everything we cannot prove disjoint.
    if not pattern.isascii() or any(marker in pattern for marker in ("(?", "(*", "[", "]")):
        raise Refused("unsupported regex location could match image endpoint")
    cursor = 0
    while cursor < len(pattern):
        if pattern[cursor] == "\\":
            cursor += 1
            if cursor == len(pattern) or pattern[cursor] not in ".-+*?^$(){}|/":
                raise Refused("unsupported PCRE escape could match image endpoint")
        cursor += 1
    try:
        flags = re.ASCII | (re.IGNORECASE if insensitive else 0)
        return re.search(pattern, IMAGE_PATH, flags) is not None
    except re.error as error:
        raise Refused("unsupported regex location could match image endpoint") from error


def check_image_location_conflicts(server, api, image):
    api_path = api.words[-1]
    for node in server.children:
        if node.words[0] == "include":
            raise Refused("included server routes could conflict with image endpoint")
        if node.words[0] != "location" or node is api or node in image:
            continue
        words = [location_token(token) for token in node.words]
        if len(words) == 3 and words[1] in ("~", "~*"):
            if regex_matches_image(words[2], words[1] == "~*"):
                raise Refused("regex location conflicts with image endpoint")
            continue
        if len(words) == 2:
            modifier, path = "", words[1]
            if path.startswith("@"):
                continue  # Named locations are not selected by request URI.
        elif len(words) == 3 and words[1] in ("=", "^~"):
            modifier, path = words[1:]
        else:
            raise Refused("unrecognized location could match image endpoint")
        if not path.startswith("/") or "\\" in path:
            raise Refused("unrecognized location URI")
        if modifier == "=":
            conflict = path == IMAGE_PATH
        else:
            conflict = IMAGE_PATH.startswith(path) and len(path) > len(api_path)
        if conflict:
            raise Refused("more-specific location conflicts with image endpoint")


def patched(source):
    servers = []

    def visit(nodes):
        for node in nodes:
            if node.words == ["server"]:
                servers.append(node)
            else:
                visit(node.children)

    visit(parse(source))
    edits, found = [], 0
    for server in servers:
        api = [node for node in server.children if node.words in (["location", "^~", "/api"], ["location", "/api"], ["location", "^~", "/api/"], ["location", "/api/"])]
        if not api:
            continue
        if len(api) != 1:
            raise Refused("ambiguous generic API location")
        found += 1
        api = api[0]
        upstream, headers, timeout = proxy_contract(api)
        image = [node for node in server.children if node.words == ["location", "=", "/api/proxy/image"]]
        if len(image) > 1:
            raise Refused("duplicate exact image locations")
        check_image_location_conflicts(server, api, image)
        if image:
            image_upstream, image_headers, image_timeout = proxy_contract(image[0])
            # Upgrade/Connection are websocket-only. All origin identity
            # headers must agree even if an existing image location omits them.
            identity = {key: value for key, value in headers.items() if key not in ("upgrade", "connection")}
            if image_upstream != upstream or any(image_headers.get(key) != value for key, value in identity.items()):
                raise Refused("existing image proxy has a different upstream or identity")
            if image_timeout.words[1] != "120s":
                edits.append((image_timeout.start, image_timeout.end, "proxy_read_timeout 120s;"))
            continue
        body = source[api.opening:api.end]
        start, end = timeout.start - api.opening, timeout.end - api.opening
        body = body[:start] + "proxy_read_timeout 120s;" + body[end:]
        line = source.rfind("\n", 0, api.start) + 1
        indent = source[line:api.start]
        if indent.strip():
            raise Refused("generic API block must start on its own line")
        edits.append((line, line, indent + "location = /api/proxy/image " + body + "\n\n"))
    if not found:
        raise Refused("no recognized generic API proxy")
    for start, end, replacement in sorted(edits, reverse=True):
        source = source[:start] + replacement + source[end:]
    return source


def read_regular(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise Refused("config must be a regular, non-linked file")
    return path.read_bytes(), info


def atomic_write(path, contents, info):
    descriptor, temporary = tempfile.mkstemp(prefix="." + path.name + ".", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            os.fchmod(stream.fileno(), stat.S_IMODE(info.st_mode))
            current = os.fstat(stream.fileno())
            if (current.st_uid, current.st_gid) != (info.st_uid, info.st_gid):
                os.fchown(stream.fileno(), info.st_uid, info.st_gid)
            stream.write(contents)
            stream.flush()
            os.fsync(stream.fileno())
        latest = path.lstat()
        if (latest.st_ino, latest.st_dev, latest.st_mtime_ns, latest.st_size) != (info.st_ino, info.st_dev, info.st_mtime_ns, info.st_size):
            raise Refused("config changed during preparation")
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    if sys.argv[1:] == ["--check-stdin"]:
        source = sys.stdin.read()
        if patched(source) != source:
            raise Refused("running nginx lacks the exact 120s image proxy")
        return
    if len(sys.argv) == 4 and sys.argv[1] == "--restore":
        backup, path = map(pathlib.Path, sys.argv[2:])
        if backup.parent != path.parent or not backup.name.startswith(path.name + ".image-proxy-") or backup.suffix != ".bak":
            raise Refused("unrecognized config backup")
        contents, _ = read_regular(backup)
        _, info = read_regular(path)
        atomic_write(path, contents, info)
        return
    if len(sys.argv) != 2:
        raise Refused("usage: ensure_image_proxy_nginx.py config | --check-stdin | --restore backup config")
    path = pathlib.Path(sys.argv[1]).absolute()
    original, info = read_regular(path)
    updated = patched(original.decode("utf-8")).encode("utf-8")
    if updated == original:
        return
    # Verify the generated configuration is self-consistent before creating
    # either a backup or a replacement. nginx performs its own syntax check
    # after the frontend has remounted the new inode and before API startup.
    if patched(updated.decode("utf-8")).encode("utf-8") != updated:
        raise Refused("generated image proxy is not stable")
    timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
    backup = path.with_name(path.name + ".image-proxy-" + timestamp + ".bak")
    with backup.open("xb") as stream:
        os.fchmod(stream.fileno(), stat.S_IMODE(info.st_mode))
        stream.write(original)
        stream.flush()
        os.fsync(stream.fileno())
    atomic_write(path, updated, info)
    print(backup)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError) as error:
        print("image proxy nginx preparation refused: " + str(error), file=sys.stderr)
        sys.exit(1)
