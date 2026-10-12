#!/usr/bin/env bash
set -euo pipefail

# modelvault local end-to-end demo: backup, dedup, list, verify, restore.
MV="go run ./cmd/modelvault"
VAULT="$(mktemp -d)"
SRC="go.mod"

echo "==> Backing up $SRC (content-defined chunking)"
$MV backup --vault "$VAULT" --chunker cdc "$SRC"

echo "==> Backing up again (expect dedup: new chunks 0)"
$MV backup --vault "$VAULT" --chunker cdc "$SRC"

echo "==> Snapshots:"
$MV list --vault "$VAULT"

echo "==> Verifying integrity of all snapshots"
$MV verify --vault "$VAULT" --all

SNAP="$($MV list --vault "$VAULT" | head -n1)"
echo "==> Restoring $SNAP"
$MV restore --vault "$VAULT" --out restored_demo.out "$SNAP"

echo "==> Comparing original vs restored"
diff -q "$SRC" restored_demo.out && echo "OK: restored file is byte-identical"

rm -f restored_demo.out
rm -rf "$VAULT"
echo "==> Demo complete"