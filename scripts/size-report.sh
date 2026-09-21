#!/usr/bin/env bash
# Host CGO size baseline for ldcli.
# JSON on stdout. Human summary on stderr.
#
# This is a host glibc/CGO build, not the musl-static GitHub release.
# Use package ranks for ordering only. Do not quote them as release megabytes.

set -euo pipefail

DISCLAIMER='host glibc/CGO build; not the musl-static GitHub release'

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v go >/dev/null 2>&1; then
  echo "size-report: go is not on PATH" >&2
  exit 1
fi

if ! command -v gcc >/dev/null 2>&1; then
  echo "size-report: gcc is required (CGO_ENABLED=1)" >&2
  exit 1
fi

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

stripped="$tmpdir/ldcli-stripped"
nostrip="$tmpdir/ldcli-nostrip"
nm_out="$tmpdir/nm.txt"

echo "size-report: building stripped host binary (CGO_ENABLED=1 -s -w)" >&2
CGO_ENABLED=1 go build -ldflags="-s -w" -o "$stripped" .

echo "size-report: building unstripped host binary (CGO_ENABLED=1)" >&2
CGO_ENABLED=1 go build -o "$nostrip" .

echo "size-report: go tool nm -size" >&2
go tool nm -size "$nostrip" >"$nm_out"

stripped_bytes="$(wc -c <"$stripped" | tr -d ' ')"
unstripped_bytes="$(wc -c <"$nostrip" | tr -d ' ')"
ui_html_bytes="$(wc -c <internal/dev_server/ui/dist/index.html | tr -d ' ')"
resource_cmds_bytes="$(wc -c <cmd/resources/resource_cmds.go | tr -d ' ')"

release_bytes=""
if command -v gh >/dev/null 2>&1; then
  asset_name="$(
    gh release view --json assets --jq '
      [.assets[].name | select(endswith("linux_amd64.tar.gz"))] | .[0] // empty
    ' 2>/dev/null || true
  )"
  if [[ -n "${asset_name}" ]]; then
    echo "size-report: fetching latest GitHub linux/amd64 release asset (${asset_name})" >&2
    rel_dir="$tmpdir/release"
    mkdir -p "$rel_dir"
    if gh release download --pattern "$asset_name" --dir "$rel_dir" >/dev/null 2>&1; then
      tar -xzf "$rel_dir/$asset_name" -C "$rel_dir" ldcli 2>/dev/null \
        || tar -xzf "$rel_dir/$asset_name" -C "$rel_dir"
      if [[ -f "$rel_dir/ldcli" ]]; then
        release_bytes="$(wc -c <"$rel_dir/ldcli" | tr -d ' ')"
      fi
    fi
  fi
fi

export SIZE_REPORT_GOOS="$goos"
export SIZE_REPORT_GOARCH="$goarch"
export SIZE_REPORT_STRIPPED="$stripped_bytes"
export SIZE_REPORT_UNSTRIPPED="$unstripped_bytes"
export SIZE_REPORT_UI="$ui_html_bytes"
export SIZE_REPORT_RESOURCE_CMDS="$resource_cmds_bytes"
export SIZE_REPORT_RELEASE="${release_bytes}"
export SIZE_REPORT_DISCLAIMER="$DISCLAIMER"
export SIZE_REPORT_NM="$nm_out"

python3 - <<'PY'
import json, os, sys
from collections import defaultdict

nm_path = os.environ["SIZE_REPORT_NM"]
unstripped = int(os.environ["SIZE_REPORT_UNSTRIPPED"])
stripped = int(os.environ["SIZE_REPORT_STRIPPED"])
ui_html = int(os.environ["SIZE_REPORT_UI"])
resource_cmds = int(os.environ["SIZE_REPORT_RESOURCE_CMDS"])
release = os.environ.get("SIZE_REPORT_RELEASE") or ""
disclaimer = os.environ["SIZE_REPORT_DISCLAIMER"]
goos = os.environ["SIZE_REPORT_GOOS"]
goarch = os.environ["SIZE_REPORT_GOARCH"]

# go tool nm -size lines: address size type name
# Group by Go package path: text after the last '/' up to the first '.'
# is the package name. CGO / nameless symbols go in catch-all buckets.

def package_of(name: str) -> str:
    if not name:
        return "(unnamed)"
    if name.startswith("go:") or name.startswith("type:") or name.startswith("runtime.funcInfo"):
        return "(go-metadata)"
    if (
        name.startswith("_Cfunc_")
        or name.startswith("_Cgo_")
        or name.startswith("_cgo_")
        or name.startswith("x_cgo_")
    ):
        return "(cgo)"
    if "/" in name:
        last_slash = name.rfind("/")
        rest = name[last_slash + 1 :]
        dot = rest.find(".")
        if dot == -1:
            return name
        return name[: last_slash + 1 + dot]
    dot = name.find(".")
    if dot == -1:
        # bare C symbols from libsqlite3 / musl-less host CGO
        return "(cgo-unprefixed)"
    return name[:dot]


totals = defaultdict(int)
with open(nm_path, "r", errors="replace") as f:
    for line in f:
        line = line.rstrip("\n")
        if not line.strip():
            continue
        parts = line.split()
        if len(parts) < 4:
            # "address type name" without size, or junk
            continue
        # address size type name...
        try:
            size = int(parts[1], 0)
        except ValueError:
            continue
        kind = parts[2]
        if kind not in ("T", "t", "D", "d", "B", "b", "R", "r"):
            continue
        name = parts[3]
        totals[package_of(name)] += size

ranked = sorted(totals.items(), key=lambda kv: (-kv[1], kv[0]))[:25]
packages = []
for path, bytes_ in ranked:
    pct = (bytes_ / unstripped * 100.0) if unstripped else 0.0
    packages.append(
        {
            "path": path,
            "bytes": bytes_,
            "pct_of_unstripped": round(pct, 4),
        }
    )

out = {
    "kind": "host",
    "goos": goos,
    "goarch": goarch,
    "cgo": True,
    "stripped_bytes": stripped,
    "unstripped_bytes": unstripped,
    "ui_html_bytes": ui_html,
    "resource_cmds_bytes": resource_cmds,
    "packages": packages,
    "disclaimer": disclaimer,
}
if release:
    out["release_linux_amd64_bytes"] = int(release)

json.dump(out, sys.stdout, indent=2)
sys.stdout.write("\n")

def mb(n):
    return n / (1024 * 1024)

print("size-report: host glibc/CGO build; not the musl-static GitHub release", file=sys.stderr)
print(f"size-report: stripped {stripped} bytes ({mb(stripped):.2f} MiB)", file=sys.stderr)
print(f"size-report: unstripped {unstripped} bytes ({mb(unstripped):.2f} MiB)", file=sys.stderr)
print("size-report: top packages (percent of unstripped):", file=sys.stderr)
for i, p in enumerate(packages[:10], 1):
    print(
        f"size-report:  {i:2}. {p['path']}  {mb(p['bytes']):.2f} MiB  {p['pct_of_unstripped']:.2f}%",
        file=sys.stderr,
    )
if release:
    print(
        f"size-report: latest GitHub linux/amd64 uncompressed {int(release)} bytes — NOT comparable to host stripped",
        file=sys.stderr,
    )
PY
