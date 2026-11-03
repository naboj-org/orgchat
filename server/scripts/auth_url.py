#!/usr/bin/env python3
"""Generate a short-lived orgchat login URL using only Python's standard library."""

import argparse
import base64
import hashlib
import hmac
import json
import os
import time
from urllib.parse import urlencode, urlsplit


def encode_json(value):
    raw = json.dumps(value, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    return base64.urlsafe_b64encode(raw).rstrip(b"=")


def positive_int(value):
    try:
        number = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("must be a positive integer") from error

    if number <= 0:
        raise argparse.ArgumentTypeError("must be a positive integer")

    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--user-id", required=True)
    parser.add_argument("--username", required=True)
    parser.add_argument("--affiliation", default="")
    parser.add_argument(
        "--channel",
        action="append",
        default=[],
        metavar="ID[=NAME]",
        help="accessible channel; repeat for multiple channels (default: none)",
    )
    parser.add_argument("--base-url", default="http://127.0.0.1:3001")
    parser.add_argument("--expires-in", type=positive_int, default=300, metavar="SECONDS")
    parser.add_argument("--issuer", default=os.environ.get("JWT_ISSUER"))
    parser.add_argument("--audience", default=os.environ.get("JWT_AUDIENCE"))
    args = parser.parse_args()

    secret = os.environ.get("JWT_SECRET", "").encode("utf-8")
    if len(secret) < 32:
        parser.error("set JWT_SECRET to the server's shared secret (at least 32 bytes)")

    base = urlsplit(args.base_url)
    if (
        base.scheme not in ("http", "https")
        or not base.netloc
        or base.username is not None
        or base.query
        or base.fragment
        or base.path not in ("", "/")
    ):
        parser.error("--base-url must be an HTTP(S) origin, without a path or query")

    if not args.user_id.strip() or len(args.user_id.encode("utf-8")) > 256:
        parser.error("--user-id must be nonempty and at most 256 UTF-8 bytes")

    if not args.username.strip() or len(args.username.encode("utf-8")) > 256:
        parser.error("--username must be nonempty and at most 256 UTF-8 bytes")

    if len(args.affiliation.encode("utf-8")) > 256:
        parser.error("--affiliation must be at most 256 UTF-8 bytes")

    channels = []
    seen = set()

    for value in args.channel:
        channel_id, separator, name = value.partition("=")
        if not separator:
            name = channel_id

        if (
            not channel_id.strip()
            or "/" in channel_id
            or len(channel_id.encode("utf-8")) > 256
            or not name.strip()
            or len(name.encode("utf-8")) > 256
            or channel_id in seen
        ):
            parser.error("channels need unique IDs without slashes and nonempty names, each at most 256 bytes")

        channels.append({"id": channel_id, "name": name})
        seen.add(channel_id)

    if len(channels) > 256:
        parser.error("at most 256 channels are supported")

    now = int(time.time())
    claims = {
        "sub": args.user_id,
        "iat": now,
        "exp": now + args.expires_in,
        "user": {
            "id": args.user_id,
            "username": args.username,
            "affiliation": args.affiliation,
        },
        "channels": channels,
    }

    if args.issuer:
        claims["iss"] = args.issuer

    if args.audience:
        claims["aud"] = args.audience

    signing_input = b".".join([encode_json({"alg": "HS256", "typ": "JWT"}), encode_json(claims)])
    signature = hmac.new(secret, signing_input, hashlib.sha256).digest()
    token = signing_input + b"." + base64.urlsafe_b64encode(signature).rstrip(b"=")

    if len(token) > 16384:
        parser.error("token exceeds the server's 16 KiB limit; use fewer or shorter channels")

    print(f"{args.base_url.rstrip('/')}/api/auth?{urlencode({'token': token.decode('ascii')})}")


if __name__ == "__main__":
    main()
