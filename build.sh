#!/usr/bin/env bash
# build.sh - Vollständiger Build-Prozess für ssh-easy
#
# Führt Tests durch, kompiliert für alle Plattformen und erstellt
# den Windows-Installer mit NSIS.
#
# Verwendung: ./build.sh
#
# @author Kurt Ingwer
# @date   2026-03-08

set -e  # Bei Fehler abbrechen

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$SCRIPT_DIR/src"
BUILD_DIR="$SCRIPT_DIR/build"
ASSETS_DIR="$SCRIPT_DIR/assets"
GO=/usr/local/go/bin/go
GOVERSIONINFO=/home/claude-code/go/bin/goversioninfo

echo "=== ssh-easy Build ==="
echo ""

# Verzeichnisse sicherstellen
mkdir -p "$BUILD_DIR"

# In src-Verzeichnis wechseln
cd "$SRC_DIR"

# Build-Nummer lesen
BUILD=$(cat build.txt | tr -d '[:space:]')
echo "Build: $BUILD"
echo ""

# --- Tests ---
echo ">>> Tests..."
$GO test ./... 2>&1
echo ""

# --- Windows-Ressourcen (Icon + Versioninfo) ---
echo ">>> Windows-Ressourcen generieren..."
$GOVERSIONINFO -platform-specific=true -o resource.syso
echo "    resource_windows_*.syso erstellt"
echo ""

# --- Linux amd64 ---
echo ">>> Linux amd64..."
$GO build -o "$BUILD_DIR/ssh-easy" .
echo "    build/ssh-easy"

# --- Linux arm64 ---
echo ">>> Linux arm64..."
GOOS=linux GOARCH=arm64 $GO build -o "$BUILD_DIR/ssh-easy-linux-arm64" .
echo "    build/ssh-easy-linux-arm64"

# --- Windows amd64 ---
echo ">>> Windows amd64..."
GOOS=windows GOARCH=amd64 $GO build -o "$BUILD_DIR/ssh-easy-windows-amd64.exe" .
echo "    build/ssh-easy-windows-amd64.exe"

# --- Windows arm64 ---
echo ">>> Windows arm64..."
GOOS=windows GOARCH=arm64 $GO build -o "$BUILD_DIR/ssh-easy-windows-arm64.exe" .
echo "    build/ssh-easy-windows-arm64.exe"

echo ""

# --- NSIS Windows Installer ---
if command -v makensis &>/dev/null; then
    echo ">>> Windows Installer (NSIS)..."
    cd "$SCRIPT_DIR"
    makensis ssh-easy-setup.nsi 2>&1 | grep -E "^Output:|^Total size:|Error"
    echo "    build/ssh-easy-setup-amd64.exe"
else
    echo ">>> NSIS nicht installiert – Installer übersprungen"
    echo "    apt-get install nsis"
fi

echo ""
echo "=== Build abgeschlossen ==="
ls -lh "$BUILD_DIR"/
