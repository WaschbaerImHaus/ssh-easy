#!/bin/bash
##
# @file claude.sh
# @brief Startet Claude Code in einem Projektverzeichnis und erstellt
#        anschließend einen Session-Export als Textdatei.
#
# @description
#   Dieses Skript darf nur innerhalb von /home/claude-code/project/[name]
#   ausgeführt werden. Es nutzt eine Markierungsdatei (.claude-open), um
#   zu erkennen, ob eine bestehende Session fortgesetzt werden muss:
#   - .claude/.claude-open existiert → Claude wird mit --continue fortgesetzt
#   - .claude/.claude-open fehlt     → Claude wird als neue Session gestartet
#   Die Datei .claude/.claude-open wird beim Start angelegt und nach der Session
#   entfernt, sodass beim nächsten regulären Start eine neue Session beginnt.
#   Bei einem Absturz bleibt .claude-open bestehen und die Session wird
#   beim nächsten Start automatisch fortgesetzt.
#   Nach der Session wird die letzte .jsonl-Datei in einen lesbaren
#   Textexport umgewandelt (exports/YYYY-MM-DD-export-N.txt).
#   Pfade werden aus config.sh geladen (O-004).
#
# @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
# @date   2026-02-19 06:00
##

# --- Root-Schutz (O-013 / SR-005) --------------------------------------------
# Claude Code sollte nie als root ausgeführt werden — maximiert den Schaden
# bei Prompt-Injection in Kombination mit --dangerously-skip-permissions.
if [ "$(id -u)" -eq 0 ]; then
	echo "FEHLER: Dieses Skript darf nicht als root ausgeführt werden." >&2
	exit 1
fi

# Nur unter project/[name] ausführbar
case "$(pwd)" in
	/home/claude-code/project/*/|/home/claude-code/project/*) ;;
	*) echo "Bitte kopiere die Datei ins Projektverzeichnis und führe sie dort aus."; exit 1 ;;
esac

# --- Zentrale Konfiguration laden (O-004) ------------------------------------
if [ -f "$(dirname "$0")/config.sh" ]; then
	source "$(dirname "$0")/config.sh"
elif [ -f "$HOME/config.sh" ]; then
	source "$HOME/config.sh"
else
	HOME_DIR="/home/claude-code"  # Fallback falls config.sh fehlt
fi

# --- Export-Funktion einbinden (liegt immer im Home-Verzeichnis) -------------
if [ -f "$HOME_DIR/export-functions.sh" ]; then
	source "$HOME_DIR/export-functions.sh"
else
	echo "WARNUNG: $HOME_DIR/export-functions.sh nicht gefunden. Export wird übersprungen."
	export_session() { :; }  # Leere Funktion als Fallback
fi

# --- Claude-Code ausführen ---------------------------------------------------
# .claude-Verzeichnis anlegen, falls noch nicht vorhanden
mkdir -p .claude

# Markierungsdatei prüfen: Existiert .claude-open, wurde die letzte Session
# nicht sauber beendet (z.B. Absturz) → mit --continue fortsetzen.
# Fehlt sie, wird eine neue Session gestartet.
MARKER=".claude/.claude-open"

if [ -f "$MARKER" ]; then
	echo "Vorherige Session erkannt – setze mit --continue fort."
	claude --dangerously-skip-permissions --continue
else
	echo "Keine offene Session – starte neue Session."
	touch "$MARKER"
	claude --dangerously-skip-permissions
fi

# --- Sauberes Ende: Markierungsdatei entfernen ------------------------------
# Wenn Claude regulär beendet wird, wird .claude-open gelöscht.
# Beim nächsten Start beginnt dann eine neue Session.
rm -f "$MARKER"

# --- Export nach Session erstellen ------------------------------------------
export_session

