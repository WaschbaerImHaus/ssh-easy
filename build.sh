#!/usr/bin/env bash
# build.sh - Complete build pipeline for ssh-easy
#
# Runs tests, compiles for all 4 platforms and builds both Windows
# installers (x64 + ARM64) using NSIS.
#
# Usage: ./build.sh
#
# Optional: Authenticode-Signatur der Windows-EXEs und Setups (osslsigncode).
# Nur aktiv, wenn SSH_EASY_CODESIGN_PKCS12 auf eine PKCS#12-Datei (.p12/.pfx)
# zeigt. Weitere Variablen (nie das Passwort im Skript oder im Repo ablegen):
#   SSH_EASY_CODESIGN_PASSFILE  Datei mit dem Passwort (bevorzugt, Rechte 0600)
#   SSH_EASY_CODESIGN_PASSWORD  Passwort direkt (Alternative zur Datei)
#   SSH_EASY_CODESIGN_TSA       RFC-3161-Zeitstempelserver
#                               (Standard: http://timestamp.digicert.com,
#                                "none" = ohne Zeitstempel, nur für Tests)
#   SSH_EASY_CODESIGN_CAFILE    CA-Datei für "osslsigncode verify"
#                               (Standard: System-CA-Bündel)
# Ohne Zertifikat wird der Schritt mit Hinweis übersprungen (kein Fehler).
#
# Sicherheit (Audit Build 90): SSH_EASY_CODESIGN_PASSWORD wird ganz am Anfang
# in eine NICHT exportierte Variable übernommen und sofort aus der Umgebung
# entfernt - go test, go build, makensis und osslsigncode sehen es nie. Die
# Passwortdatei entsteht bevorzugt unter $XDG_RUNTIME_DIR (tmpfs, nur der
# Nutzer), sonst per mktemp, immer 0600. Passwortdatei und .p12 müssen für
# Gruppe und andere unzugänglich sein, sonst bricht der Build ab.
#
# Frühe Prüfung (Build 90): codesign_check_config läuft ganz am Anfang von
# main, VOR den Tests und den Builds. Eine falsch eingerichtete Signatur
# (Dateien fehlen, Rechte offen, osslsigncode oder NSIS >= 3.08 fehlt, keine
# Passwortquelle) bricht so sofort ab statt erst nach Minuten. Das eigentliche
# Signieren bleibt an seiner Stelle. Sind beide Passwortquellen gesetzt, gilt
# SSH_EASY_CODESIGN_PASSFILE (mit Hinweis).
#
# Uninstall.exe (Build 90): Ist die Signatur eingerichtet, signiert NSIS den
# Deinstaller selbst (!uninstfinalize, NSIS >= 3.08). makensis ruft dafür
# dieses Skript mit "--codesign-uninstaller <Datei>" auf. Dabei werden nur
# Pfade übergeben (nie das Passwort): Skript, .p12, Passwortdatei, TSA, CA.
#
# Zum Testen lässt sich das Skript mit "source" laden, ohne den Build zu
# starten (SSH_EASY_BUILD_SOURCE_ONLY=1, siehe Ende der Datei).
#
# @author Kurt Ingwer
# @date   2026-11-15 19:36

set -e  # Abort on any error

# --- Passwort sofort aus der Umgebung nehmen (vor jedem Kindprozess) ---
# unset vor der Zuweisung: war CODESIGN_PASSWORD schon exportiert, bliebe
# das Export-Attribut sonst erhalten
unset CODESIGN_PASSWORD CODESIGN_PASSWORD_SET
CODESIGN_PASSWORD=""
CODESIGN_PASSWORD_SET=0
if [ -n "${SSH_EASY_CODESIGN_PASSWORD+x}" ]; then
    CODESIGN_PASSWORD="$SSH_EASY_CODESIGN_PASSWORD"
    CODESIGN_PASSWORD_SET=1
fi
unset SSH_EASY_CODESIGN_PASSWORD

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_FILE="$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")"
SRC_DIR="$SCRIPT_DIR/src"
BUILD_DIR="$SCRIPT_DIR/build"
GO=/usr/local/go/bin/go

# Vollständig statische Binaries ohne libc-Abhängigkeit: ohne CGO_ENABLED=0
# startet die Linux-Binary auf älteren Distributionen nicht (CXXABI-Fehler)
export CGO_ENABLED=0

echo "=== ssh-easy Build ==="
echo ""

# --- Authenticode (optional) ---
# Die Windows-EXEs werden VOR dem Setup-Bau signiert (das Setup enthält die
# signierte EXE), die Setups danach. Die SHA256SUMS entstehen erst ganz am
# Ende und enthalten damit die Prüfsummen der signierten Dateien.
CODESIGN_PKCS12="${SSH_EASY_CODESIGN_PKCS12:-}"
CODESIGN_TSA="${SSH_EASY_CODESIGN_TSA:-http://timestamp.digicert.com}"
CODESIGN_CAFILE="${SSH_EASY_CODESIGN_CAFILE:-}"
CODESIGN_PASSFILE="${SSH_EASY_CODESIGN_PASSFILE:-}"
CODESIGN_TMP_PASSFILE=""
CODESIGN_ACTIVE=0
CODESIGN_CHECKED=0
# Bezeichnung für Meldungen statt des Dateinamens (Uninstall.exe liegt
# während des Setup-Baus unter einem Zufallsnamen in /tmp)
CODESIGN_LABEL=""
# 1 = keine Banner (Aufruf durch makensis)
CODESIGN_QUIET=0

# Temporäre Passwortdatei beim Beenden immer löschen
cleanup_codesign() {
    if [ -n "$CODESIGN_TMP_PASSFILE" ]; then
        rm -f "$CODESIGN_TMP_PASSFILE"
    fi
}
trap cleanup_codesign EXIT
# Signale in ein normales Ende umsetzen: dann läuft der EXIT-Trap sicher und
# die Passwortdatei bleibt auch bei Strg+C oder kill nicht liegen
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# Gibt die Zugriffsrechte einer Datei oktal aus (folgt Symlinks; GNU stat,
# Rückfall BSD/macOS stat).
file_mode_octal() {
    stat -L -c '%a' "$1" 2>/dev/null || stat -L -f '%Lp' "$1" 2>/dev/null
}

# Bricht ab, wenn Gruppe oder andere auf die Datei zugreifen dürfen
# (Passwortdatei, PKCS#12 mit privatem Schlüssel).
# $1 = Datei, $2 = Bezeichnung für die Meldung
require_private_file() {
    local mode
    mode="$(file_mode_octal "$1")" || mode=""
    if [ -z "$mode" ]; then
        echo "ERROR: cannot read permissions of $2" >&2
        exit 1
    fi
    if (( (8#$mode & 8#077) != 0 )); then
        echo "ERROR: $2 is accessible by group/others (mode $mode) - run: chmod 600 <file>" >&2
        exit 1
    fi
}

# Legt die temporäre Passwortdatei an: bevorzugt in $XDG_RUNTIME_DIR, wenn
# das ein eigenes Verzeichnis nur für den Nutzer ist, sonst per mktemp.
# Immer unter umask 077, danach wird 0600 geprüft.
make_codesign_passfile() {
    local dir="" file
    if [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "$XDG_RUNTIME_DIR" ] && [ ! -L "$XDG_RUNTIME_DIR" ] \
        && [ -O "$XDG_RUNTIME_DIR" ]; then
        local dmode
        dmode="$(file_mode_octal "$XDG_RUNTIME_DIR")" || dmode=""
        if [ -n "$dmode" ] && (( (8#$dmode & 8#077) == 0 )); then
            dir="$XDG_RUNTIME_DIR"
        fi
    fi
    if [ -n "$dir" ]; then
        file="$(umask 077 && mktemp "$dir/ssh-easy-codesign.XXXXXXXX")"
    else
        file="$(umask 077 && mktemp)"
    fi
    chmod 600 "$file"
    CODESIGN_TMP_PASSFILE="$file"
    require_private_file "$file" "temporary password file"
}

# Gibt 0 zurück, wenn makensis mindestens NSIS 3.08 ist (erst ab dieser
# Version gibt es !uninstfinalize zum Signieren des Deinstallers).
nsis_supports_uninstfinalize() {
    local ver major rest minor
    ver="$(makensis -VERSION 2>/dev/null | head -n 1)" || return 1
    ver="${ver#v}"
    major="${ver%%.*}"
    rest="${ver#*.}"
    minor="${rest%%[!0-9]*}"
    [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ ]] || return 1
    (( 10#$major > 3 || (10#$major == 3 && 10#$minor >= 8) ))
}

# Reine Prüfung der Signatur-Konfiguration, OHNE etwas anzulegen oder zu
# signieren (Build 90: läuft ganz am Anfang von main, vor Tests und Builds).
# Nicht konfiguriert = still zurück. Falsch konfiguriert = Abbruch, damit nie
# versehentlich ein unsigniertes Release entsteht.
codesign_check_config() {
    [ "$CODESIGN_CHECKED" = "1" ] && return 0
    [ -n "$CODESIGN_PKCS12" ] || return 0
    if [ ! -f "$CODESIGN_PKCS12" ]; then
        echo "ERROR: SSH_EASY_CODESIGN_PKCS12 points to a missing file" >&2
        exit 1
    fi
    # Die .p12 enthält den privaten Schlüssel: nur für den Nutzer lesbar
    require_private_file "$CODESIGN_PKCS12" "SSH_EASY_CODESIGN_PKCS12"
    if ! command -v osslsigncode &>/dev/null; then
        echo "ERROR: osslsigncode not found (apt-get install osslsigncode)" >&2
        exit 1
    fi
    # Passwortquelle: Datei (hat Vorrang) oder Variable, mindestens eine
    if [ -n "$CODESIGN_PASSFILE" ]; then
        if [ ! -f "$CODESIGN_PASSFILE" ]; then
            echo "ERROR: SSH_EASY_CODESIGN_PASSFILE points to a missing file" >&2
            exit 1
        fi
        require_private_file "$CODESIGN_PASSFILE" "SSH_EASY_CODESIGN_PASSFILE"
        if [ "$CODESIGN_PASSWORD_SET" = "1" ]; then
            echo "NOTE: SSH_EASY_CODESIGN_PASSFILE and SSH_EASY_CODESIGN_PASSWORD are both set;" >&2
            echo "      the password file is used, the variable is ignored" >&2
        fi
    elif [ "$CODESIGN_PASSWORD_SET" != "1" ]; then
        echo "ERROR: certificate password missing (set SSH_EASY_CODESIGN_PASSFILE" >&2
        echo "       or SSH_EASY_CODESIGN_PASSWORD)" >&2
        exit 1
    fi
    if [ -n "$CODESIGN_CAFILE" ] && [ ! -f "$CODESIGN_CAFILE" ]; then
        echo "ERROR: SSH_EASY_CODESIGN_CAFILE points to a missing file" >&2
        exit 1
    fi
    # Der Deinstaller wird von NSIS selbst signiert (!uninstfinalize, >= 3.08).
    # Ohne makensis entfällt der Setup-Bau ohnehin (Hinweis weiter unten).
    if command -v makensis &>/dev/null && ! nsis_supports_uninstfinalize; then
        echo "ERROR: makensis too old or version unknown: signing Uninstall.exe needs NSIS >= 3.08" >&2
        exit 1
    fi
    CODESIGN_CHECKED=1
}

# Schaltet das Signieren ein oder aus (prüft vorher die Konfiguration, falls
# main das noch nicht getan hat) und legt bei Bedarf die Passwortdatei an.
codesign_init() {
    codesign_check_config
    if [ -z "$CODESIGN_PKCS12" ]; then
        echo ">>> Authenticode: skipped (SSH_EASY_CODESIGN_PKCS12 not set;"
        echo "    Windows files stay unsigned, SmartScreen will warn)"
        return 0
    fi
    if [ -z "$CODESIGN_PASSFILE" ]; then
        # Passwort aus der Variable in eine 0600-Datei legen: so taucht es
        # nicht in der Prozessliste (Kommandozeile) auf; printf ist ein
        # eingebauter Befehl (kein eigener Prozess)
        make_codesign_passfile
        printf '%s' "$CODESIGN_PASSWORD" > "$CODESIGN_TMP_PASSFILE"
        CODESIGN_PASSFILE="$CODESIGN_TMP_PASSFILE"
    fi
    # Passwort nicht länger als nötig im Speicher der Shell halten
    CODESIGN_PASSWORD=""
    unset CODESIGN_PASSWORD
    CODESIGN_ACTIVE=1
    [ "$CODESIGN_QUIET" = "1" ] && return 0
    echo ">>> Authenticode: enabled (SHA-256, certificate from SSH_EASY_CODESIGN_PKCS12)"
    if [ "$CODESIGN_TSA" = "none" ]; then
        echo "    WARNING: no timestamp (SSH_EASY_CODESIGN_TSA=none) - the signature"
        echo "    becomes invalid when the certificate expires"
    fi
}

# Signiert die übergebenen Dateien (SHA-256, RFC-3161-Zeitstempel) und prüft
# sie anschließend mit "osslsigncode verify". Ohne Zertifikat: nichts tun.
codesign_files() {
    [ "$CODESIGN_ACTIVE" = "1" ] || return 0
    local file tmp tmpdir
    local sign_args=(-pkcs12 "$CODESIGN_PKCS12" -readpass "$CODESIGN_PASSFILE"
        -h sha256 -n "ssh-easy" -i "https://github.com/WaschbaerImHaus/ssh-easy")
    local verify_args=()
    if [ "$CODESIGN_TSA" != "none" ]; then
        sign_args+=(-ts "$CODESIGN_TSA")
    fi
    if [ -n "$CODESIGN_CAFILE" ]; then
        verify_args+=(-CAfile "$CODESIGN_CAFILE")
        if [ "$CODESIGN_TSA" != "none" ]; then
            verify_args+=(-TSA-CAfile "$CODESIGN_CAFILE")
        fi
    fi
    for file in "$@"; do
        # Ausgabe in einem frischen, privaten Ordner (mktemp -d, 0700) neben der
        # Datei: kein Symlink-Rennen, auch wenn sie in einem für alle
        # beschreibbaren Ordner liegt (makensis legt Uninstall.exe in /tmp ab).
        # osslsigncode überschreibt keine vorhandene Datei, darum der Ordner.
        tmpdir="$(mktemp -d "$file.signing.XXXXXX")"
        tmp="$tmpdir/signed"
        if ! osslsigncode sign "${sign_args[@]}" -in "$file" -out "$tmp"; then
            rm -rf "$tmpdir"
            echo "ERROR: signing ${CODESIGN_LABEL:-$(basename "$file")} failed" >&2
            exit 1
        fi
        mv -f "$tmp" "$file"
        rm -rf "$tmpdir"
        if ! osslsigncode verify "${verify_args[@]}" -in "$file" >/dev/null; then
            echo "ERROR: signature of ${CODESIGN_LABEL:-$(basename "$file")} does not verify" >&2
            echo "       (self-signed/unknown CA? set SSH_EASY_CODESIGN_CAFILE)" >&2
            exit 1
        fi
        echo "    signed + verified: ${CODESIGN_LABEL:-$(basename "$file")}"
    done
}

# Aufruf durch makensis (!uninstfinalize in ssh-easy-setup.nsi): signiert den
# frisch erzeugten Deinstaller an Ort und Stelle. Das Passwort kommt nie hier
# an, nur die Pfade (SSH_EASY_CODESIGN_PASSFILE usw. setzt run_makensis).
# Ohne eingerichtete Signatur ist das ein Fehler, nie ein stilles Überspringen.
codesign_uninstaller_mode() {
    if [ $# -ne 1 ] || [ ! -f "$1" ]; then
        echo "ERROR: --codesign-uninstaller needs the path of an existing file" >&2
        exit 1
    fi
    CODESIGN_QUIET=1
    CODESIGN_LABEL="Uninstall.exe"
    codesign_init
    if [ "$CODESIGN_ACTIVE" != "1" ]; then
        echo "ERROR: Authenticode is not configured, Uninstall.exe stays unsigned" >&2
        exit 1
    fi
    codesign_files "$1"
}

# Baut ein Setup mit makensis. Ist die Signatur eingerichtet, signiert NSIS
# den Deinstaller dabei über build.sh selbst (-DSIGN_UNINSTALLER=1) und ein
# Fehlschlag bricht den Build ab. Ohne Signatur bleibt alles wie bisher (die
# Ausgabe wird gefiltert, der Rückgabewert ignoriert).
# $1 = Architektur (amd64 | arm64)
run_makensis() {
    local arch="$1" log rc=0
    local -a sign_defs=() sign_env=()
    if [ "$CODESIGN_ACTIVE" = "1" ]; then
        sign_defs=(-DSIGN_UNINSTALLER=1)
        # Nur Pfade und Einstellungen, nie das Passwort selbst
        sign_env=("SSH_EASY_SIGN_SCRIPT=$SCRIPT_FILE"
            "SSH_EASY_CODESIGN_PKCS12=$CODESIGN_PKCS12"
            "SSH_EASY_CODESIGN_PASSFILE=$CODESIGN_PASSFILE"
            "SSH_EASY_CODESIGN_TSA=$CODESIGN_TSA"
            "SSH_EASY_CODESIGN_CAFILE=$CODESIGN_CAFILE")
    fi
    log="$(umask 077 && mktemp)"
    env "${sign_env[@]}" makensis -DARCH="$arch" -DVERSION="$VERSION" -DBUILD="$BUILD" \
        "${sign_defs[@]}" ssh-easy-setup.nsi >"$log" 2>&1 || rc=$?
    grep -E "Output:|Total size:|Error|signed \+ verified|UninstFinalize" "$log" || true
    if [ "$CODESIGN_ACTIVE" = "1" ] && [ "$rc" != "0" ]; then
        echo "ERROR: makensis ($arch) failed (exit $rc):" >&2
        tail -n 20 "$log" >&2
        rm -f "$log"
        exit 1
    fi
    rm -f "$log"
}

# Hauptablauf (am Ende aufgerufen, außer beim Laden für Tests)
main() {
# Signatur-Konfiguration zuerst prüfen: vor Tests und Builds, damit eine
# Fehlkonfiguration sofort abbricht (nichts wird angelegt oder signiert)
codesign_check_config

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
# -H windowsgui ist zwingend: ohne diesen Flag läuft die Binary als Console-
# Subsystem, conhost gehört dann zu cmd.exe und das Taskleisten-Icon ist falsch.
echo ">>> Windows amd64..."
GOOS=windows GOARCH=amd64 $GO build -ldflags "-H windowsgui" -o "$BUILD_DIR/ssh-easy-windows-amd64.exe" .
echo "    OK: build/ssh-easy-windows-amd64.exe"

# --- Windows ARM64 ---
echo ">>> Windows arm64..."
GOOS=windows GOARCH=arm64 $GO build -ldflags "-H windowsgui" -o "$BUILD_DIR/ssh-easy-windows-arm64.exe" .
echo "    OK: build/ssh-easy-windows-arm64.exe"
echo ""

# --- Authenticode: EXEs signieren (vor dem Setup-Bau) ---
codesign_init
if [ "$CODESIGN_ACTIVE" = "1" ]; then
    echo ">>> Signing Windows executables..."
    codesign_files "$BUILD_DIR/ssh-easy-windows-amd64.exe" \
        "$BUILD_DIR/ssh-easy-windows-arm64.exe"
    echo ""
fi

# --- Linux Desktop-Integration ---
# .desktop-Datei und PNG-Icon für Dateimanager-Integration nach build/ kopieren.
# Nutzer kann die .desktop-Datei auf den Desktop legen oder nach
# ~/.local/share/applications/ installieren – Doppelklick öffnet dann ssh-easy
# im vom System konfigurierten Terminal-Emulator (Terminal=true im .desktop).
cp "$SCRIPT_DIR/assets/ssh-easy.desktop" "$BUILD_DIR/ssh-easy.desktop"
cp "$SCRIPT_DIR/assets/icon_256.png" "$BUILD_DIR/ssh-easy.png"
echo ">>> Linux .desktop + icon kopiert"

# --- Windows Installer (NSIS) ---
cd "$SCRIPT_DIR"
if command -v makensis &>/dev/null; then
    echo ">>> Windows Installer x64 (NSIS)..."
    run_makensis amd64
    echo "    OK: build/ssh-easy-setup-amd64.exe"

    echo ">>> Windows Installer ARM64 (NSIS)..."
    run_makensis arm64
    echo "    OK: build/ssh-easy-setup-arm64.exe"

    # --- Authenticode: Setups signieren (nach dem Setup-Bau) ---
    if [ "$CODESIGN_ACTIVE" = "1" ]; then
        echo ">>> Signing Windows installers..."
        codesign_files "$BUILD_DIR/ssh-easy-setup-amd64.exe" \
            "$BUILD_DIR/ssh-easy-setup-arm64.exe"
    fi
else
    echo ">>> NSIS not found – installer skipped (apt-get install nsis)"
fi

# --- Signierte Prüfsummenliste (seit Build 51) ---
# Der Auto-Updater installiert nur Dateien, deren SHA-256 in der mit Ed25519
# signierten Liste steht. Der private Schlüssel liegt nur auf dem
# Build-Rechner (nie im Repo, nie bei GitHub). Ohne Schlüssel entsteht ein
# Build ohne Signatur - installierte Versionen lehnen ihn als Update ab.
SIGNING_KEY="${SSH_EASY_SIGNING_KEY:-$HOME/.config/ssh-easy/release-signing.key}"
RELEASE_FILES=(
    "$BUILD_DIR/ssh-easy"
    "$BUILD_DIR/ssh-easy-linux-arm64"
    "$BUILD_DIR/ssh-easy-windows-amd64.exe"
    "$BUILD_DIR/ssh-easy-windows-arm64.exe"
    "$BUILD_DIR/ssh-easy-setup-amd64.exe"
    "$BUILD_DIR/ssh-easy-setup-arm64.exe"
)
rm -f "$BUILD_DIR/ssh-easy-SHA256SUMS" "$BUILD_DIR/ssh-easy-SHA256SUMS.sig"
if [ -f "$SIGNING_KEY" ]; then
    echo ">>> Signing release files..."
    (cd "$SRC_DIR" && $GO run ./cmd/release-sign -key "$SIGNING_KEY" -tag "v0.${MINOR}" \
        -out "$BUILD_DIR" "${RELEASE_FILES[@]}")
    echo "    OK: build/ssh-easy-SHA256SUMS + .sig"
else
    echo ">>> WARNING: signing key $SIGNING_KEY missing – release NOT signed,"
    echo "    auto-update will reject it"
fi

echo ""
echo "=== Build complete ==="
ls -lh "$BUILD_DIR"/
}

# Beim Laden mit "source" und SSH_EASY_BUILD_SOURCE_ONLY=1 nur die Funktionen
# bereitstellen (Tests), sonst bauen
if [ "${SSH_EASY_BUILD_SOURCE_ONLY:-}" = "1" ] && [ "${BASH_SOURCE[0]}" != "$0" ]; then
    return 0
fi
if [ "${1:-}" = "--codesign-uninstaller" ]; then
    shift
    codesign_uninstaller_mode "$@"
    exit 0
fi
main "$@"
