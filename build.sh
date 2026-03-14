#!/usr/bin/env bash
# build.sh - Complete build pipeline for ssh-easy
#
# Runs tests, compiles for all 4 platforms and builds both Windows
# installers (x64 + ARM64) using NSIS.
#
# Usage: ./build.sh
#
# @author Kurt Ingwer
# @date   2026-03-14

set -e  # Abort on any error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$SCRIPT_DIR/src"
BUILD_DIR="$SCRIPT_DIR/build"
GO=/usr/local/go/bin/go

echo "=== ssh-easy Build ==="
echo ""

# Sicherstellen dass das build-Verzeichnis existiert
mkdir -p "$BUILD_DIR"

# In das src-Verzeichnis wechseln
cd "$SRC_DIR"

# Build-Nummer aus build.txt lesen
BUILD=$(cat build.txt | tr -d '[:space:]')
# Versions-String aus der Buildnummer ableiten (z.B. Build 15 → v0.15.0)
MINOR=$(echo "$BUILD" | sed 's/^0*//')
VERSION="0.${MINOR}.0"

echo "Build : $BUILD"
echo "Version: $VERSION"
echo ""

# --- Tests ---
echo ">>> Running tests..."
$GO test ./...
echo ""

# --- Linux x64 ---
echo ">>> Linux amd64..."
$GO build -o "$BUILD_DIR/ssh-easy" .
echo "    OK: build/ssh-easy"

# --- Linux ARM64 ---
echo ">>> Linux arm64..."
GOOS=linux GOARCH=arm64 $GO build -o "$BUILD_DIR/ssh-easy-linux-arm64" .
echo "    OK: build/ssh-easy-linux-arm64"

# --- Windows x64 ---
echo ">>> Windows amd64..."
GOOS=windows GOARCH=amd64 $GO build -o "$BUILD_DIR/ssh-easy-windows-amd64.exe" .
echo "    OK: build/ssh-easy-windows-amd64.exe"

# --- Windows ARM64 ---
echo ">>> Windows arm64..."
GOOS=windows GOARCH=arm64 $GO build -o "$BUILD_DIR/ssh-easy-windows-arm64.exe" .
echo "    OK: build/ssh-easy-windows-arm64.exe"
echo ""

# --- Windows Installer (NSIS) ---
cd "$SCRIPT_DIR"
if command -v makensis &>/dev/null; then
    echo ">>> Windows Installer x64 (NSIS)..."
    makensis -DARCH=amd64 -DVERSION="$VERSION" -DBUILD="$BUILD" ssh-easy-setup.nsi \
        2>&1 | grep -E "Output:|Total size:|Error" || true
    echo "    OK: build/ssh-easy-setup-amd64.exe"

    echo ">>> Windows Installer ARM64 (NSIS)..."
    makensis -DARCH=arm64 -DVERSION="$VERSION" -DBUILD="$BUILD" ssh-easy-setup.nsi \
        2>&1 | grep -E "Output:|Total size:|Error" || true
    echo "    OK: build/ssh-easy-setup-arm64.exe"
else
    echo ">>> NSIS not found – installer skipped (apt-get install nsis)"
fi

echo ""
echo "=== Build complete ==="
ls -lh "$BUILD_DIR"/
