#!/usr/bin/env bash
# installer_wine_check.sh - Prüft das Windows-Setup unter Wine
#
# Installiert zuerst ein altes Setup (Vorgabe: v0.81 von GitHub) und dann das
# neue Setup aus build/ still (/S) darüber - in mehreren Lagen:
#   A  ssh-easy beendet
#   B  ssh-easy.exe läuft (echte laufende EXE: Löschen verboten)
#   C  ssh-easy.exe dauerhaft gesperrt -> Setup muss mit Fehler abbrechen
#   D  Sperre löst sich nach 5 s (Auto-Update: ssh-easy beendet sich)
#   E  Reste einer gescheiterten Installation (ssh-easy-windows-amd64.exe)
#   F  zwei Instanzen: eine schon beiseite gelegte EXE läuft noch
# Danach: Prüfsumme der installierten EXE, Versionsangabe in "Apps",
# Startmenü-Verknüpfung und Registry (der alte Deinstaller darf nichts
# Neues wegräumen).
#
# Voraussetzungen: wine + wine32 (NSIS-Stub ist 32 Bit), gh, Go.
# Alles läuft in einem Wegwerf-WINEPREFIX unter $WORK.
#
# Usage: debugging/installer_wine_check.sh [altes-setup.exe]
#
# @author Kurt Ingwer
# @date   2026-10-27 16:20

set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="${WORK:-$(mktemp -d)}"
NEW_SETUP="${NEW_SETUP:-$ROOT/build/ssh-easy-setup-amd64.exe}"
NEW_EXE="${NEW_EXE:-$ROOT/build/ssh-easy-windows-amd64.exe}"
OLD_SETUP="${1:-}"
export WINEDEBUG=-all

fail=0
# check vergleicht und meldet ein Ergebnis
check() {
    if [ "$2" = "$3" ]; then echo "    ok:   $1"; else echo "    FAIL: $1 (erwartet '$3', ist '$2')"; fail=1; fi
}

# Altes Setup besorgen
if [ -z "$OLD_SETUP" ]; then
    mkdir -p "$WORK/old"
    GH_TOKEN="${GH_TOKEN:-}" gh release download v0.81 --repo WaschbaerImHaus/ssh-easy \
        --clobber -D "$WORK/old" -p 'ssh-easy-setup-amd64.exe' || exit 1
    OLD_SETUP="$WORK/old/ssh-easy-setup-amd64.exe"
fi

# Hilfsprogramm bauen
(cd "$ROOT/debugging/wine_locker" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    /usr/local/go/bin/go build -o "$WORK/locker.exe" main.go) || exit 1

NEW_SUM=$(sha256sum "$NEW_EXE" | cut -d' ' -f1)
BUILD=$(tr -d '[:space:]' < "$ROOT/src/build.txt")

# Basis-Prefix mit alter Installation (einmal anlegen, je Fall kopieren)
export WINEPREFIX="$WORK/base"
timeout 300 wine wineboot -i >/dev/null 2>&1
timeout 120 wine "$OLD_SETUP" /S
sleep 2
wineserver -w

# prepare legt einen frischen Prefix mit alter Installation an
prepare() {
    rm -rf "$WORK/case"
    cp -a "$WORK/base" "$WORK/case"
    export WINEPREFIX="$WORK/case"
    P="$WINEPREFIX/drive_c/Program Files/ssh-easy"
}

# install_new startet das neue Setup still und gibt dessen Exit-Code aus
install_new() {
    timeout 120 wine "$NEW_SETUP" /S
    local rc=$?
    sleep 2
    echo "$rc"
}

# verify prüft die Installation nach einem erfolgreichen Setup
verify() {
    check "Exit-Code" "$1" "0"
    check "installierte EXE ist der neue Build" "$(sha256sum "$P/ssh-easy.exe" 2>/dev/null | cut -d' ' -f1)" "$NEW_SUM"
    check "kein Rest ssh-easy-windows-amd64.exe" "$([ -e "$P/ssh-easy-windows-amd64.exe" ] && echo da || echo weg)" "weg"
    check "Uninstall.exe vorhanden" "$([ -e "$P/Uninstall.exe" ] && echo da || echo weg)" "da"
    local ver
    ver=$(wine reg query 'HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy' /v DisplayVersion 2>/dev/null \
        | grep -o 'Build [0-9a-z]*')
    check "Versionsangabe in Apps" "$ver" "Build $BUILD"
    local lnk
    lnk=$(find "$WINEPREFIX/drive_c" -path '*Start Menu*' -name 'ssh-easy.lnk' 2>/dev/null | head -1)
    check "Startmenü-Verknüpfung" "$([ -n "$lnk" ] && echo da || echo weg)" "da"
}

echo ">>> A: ssh-easy beendet"
prepare
verify "$(install_new)"

echo ">>> B: ssh-easy.exe läuft"
prepare
cp "$WORK/locker.exe" "$P/ssh-easy.exe"
(timeout 90 wine "C:\\Program Files\\ssh-easy\\ssh-easy.exe" sleep 80 >/dev/null 2>&1 &)
sleep 4
verify "$(install_new)"
wineserver -k

echo ">>> C: ssh-easy.exe dauerhaft gesperrt (Setup muss abbrechen, Dauer ca. 30 s)"
prepare
OLD_SUM=$(sha256sum "$P/ssh-easy.exe" | cut -d' ' -f1)
(timeout 90 wine "$WORK/locker.exe" lock "C:\\Program Files\\ssh-easy\\ssh-easy.exe" 80 >/dev/null 2>&1 &)
sleep 4
rc=$(install_new)
check "Exit-Code (Abbruch)" "$rc" "2"
check "alte EXE unverändert" "$(sha256sum "$P/ssh-easy.exe" | cut -d' ' -f1)" "$OLD_SUM"
# Ein Abbruch darf die alte Installation nicht abbauen (Review-Fund Build 83)
check "alte Uninstall.exe noch da" "$([ -e "$P/Uninstall.exe" ] && echo da || echo weg)" "da"
check "Startmenü-Verknüpfung noch da" "$(find "$WINEPREFIX/drive_c" -path '*Start Menu*' -name 'ssh-easy.lnk' 2>/dev/null | grep -c .)" "1"
check "Eintrag in Apps noch da" "$(wine reg query 'HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy' /v UninstallString >/dev/null 2>&1 && echo da || echo weg)" "da"
wineserver -k

echo ">>> D: Sperre löst sich nach 5 s (Auto-Update)"
prepare
(timeout 60 wine "$WORK/locker.exe" lock "C:\\Program Files\\ssh-easy\\ssh-easy.exe" 5 >/dev/null 2>&1 &)
sleep 3
verify "$(install_new)"
wineserver -k

echo ">>> E: Reste einer gescheiterten Installation"
prepare
cp "$OLD_SETUP" "$P/ssh-easy-windows-amd64.exe"
verify "$(install_new)"

echo ">>> F: zwei Instanzen - die früher beiseite gelegte EXE läuft noch"
prepare
cp "$WORK/locker.exe" "$P/ssh-easy.exe"
(timeout 150 wine "C:\\Program Files\\ssh-easy\\ssh-easy.exe" sleep 140 >/dev/null 2>&1 &)
sleep 4
install_new >/dev/null
# Jetzt läuft die erste Instanz als ssh-easy.exe.old; zweite Instanz starten
cp "$WORK/locker.exe" "$P/ssh-easy.exe"
(timeout 120 wine "C:\\Program Files\\ssh-easy\\ssh-easy.exe" sleep 110 >/dev/null 2>&1 &)
sleep 4
verify "$(install_new)"
wineserver -k

echo ""
if [ "$fail" = 0 ]; then echo "=== Alle Fälle ok ==="; else echo "=== FEHLER ==="; fi
exit "$fail"
