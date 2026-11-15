#!/usr/bin/env python3
"""Check or regenerate the read-only Homepage suite policy snapshot.

--check (default): validate local provenance and compare current Homepage main.
--offline: validate only committed snapshot (upstream freshness unverified).
--sync: regenerate from current Homepage main for a reviewed consumer PR.
"""
import argparse
import hashlib
import pathlib
import re
import sys
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
COPY = ROOT / "docs" / "suite-development-policy.md"
SOURCE = "https://raw.githubusercontent.com/richard-mauri/homepage"
API = "https://api.github.com/repos/richard-mauri/homepage/commits/main"
STANDARD = ("AGENTS.md", "DEVELOPMENT.md", "ARCHITECTURE.md", "CURRENT_WORK.md", "README.md")


def download(url):
    req = urllib.request.Request(url, headers={"User-Agent": "mauri-suite-policy-check/1"})
    with urllib.request.urlopen(req, timeout=20) as response:
        return response.read()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def parse_copy():
    data = COPY.read_bytes()
    match = re.match(rb"<!-- GENERATED FILE .+?\n-->\n", data, re.S)
    if not match:
        raise ValueError("missing generated-policy provenance header")
    header = match.group().decode("utf-8")
    fields = dict(re.findall(r"^(source_repository|source_path|source_commit|policy_version|source_sha256): (.+)$", header, re.M))
    if fields.get("source_repository") != "richard-mauri/homepage" or fields.get("source_path") != "SUITE_DEVELOPMENT_POLICY.md":
        raise ValueError("unexpected authoritative source")
    if not re.fullmatch(r"[0-9a-f]{40}", fields.get("source_commit", "")):
        raise ValueError("invalid pinned upstream commit")
    if not re.fullmatch(r"[0-9a-f]{64}", fields.get("source_sha256", "")):
        raise ValueError("invalid source digest")
    body = data[match.end():]
    if digest(body) != fields["source_sha256"]:
        raise ValueError("generated policy body has been edited")
    version = re.search(rb"\*\*Policy version:\*\* ([0-9]+\.[0-9]+\.[0-9]+)", body)
    if not version or version.group(1).decode() != fields.get("policy_version"):
        raise ValueError("policy version metadata mismatch")
    return fields, body


def check_local():
    for name in STANDARD:
        if not (ROOT / name).is_file():
            raise ValueError("missing required app document: " + name)
    agent = (ROOT / "AGENTS.md").read_text(encoding="utf-8")
    if "docs/suite-development-policy.md" not in agent:
        raise ValueError("AGENTS.md does not require the local policy copy")
    return parse_copy()


def sync():
    import json
    commit = json.loads(download(API))["sha"]
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("invalid upstream commit response")
    body = download(f"{SOURCE}/{commit}/SUITE_DEVELOPMENT_POLICY.md")
    version = re.search(rb"\*\*Policy version:\*\* ([0-9]+\.[0-9]+\.[0-9]+)", body)
    if not version:
        raise ValueError("upstream policy has no semantic policy version")
    header = ("<!-- GENERATED FILE — DO NOT EDIT\n"
              "source_repository: richard-mauri/homepage\n"
              "source_path: SUITE_DEVELOPMENT_POLICY.md\n"
              f"source_commit: {commit}\n"
              f"policy_version: {version.group(1).decode()}\n"
              f"source_sha256: {digest(body)}\n"
              "generation: python3 scripts/suite_policy.py --sync\n"
              "-->\n").encode()
    COPY.parent.mkdir(parents=True, exist_ok=True)
    COPY.write_bytes(header + body)
    print(f"Regenerated {COPY.relative_to(ROOT)} from Homepage {commit}")


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--offline", action="store_true")
    group.add_argument("--sync", action="store_true")
    args = parser.parse_args()
    try:
        if args.sync:
            sync()
            return
        fields, body = check_local()
        if not args.offline:
            import json
            current_sha = json.loads(download(API))["sha"]
            current = download(f"{SOURCE}/{current_sha}/SUITE_DEVELOPMENT_POLICY.md")
            if current != body:
                raise ValueError("policy snapshot is stale relative to Homepage main; regenerate and review")
            print(f"PASS: policy copy current against Homepage main {current_sha}")
        else:
            print(f"PASS: local policy copy integrity (upstream freshness UNVERIFIED), pinned {fields['source_commit']}")
    except (ValueError, OSError, KeyError, urllib.error.URLError) as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
