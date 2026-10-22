// Paket main - Einstiegspunkt für ssh-easy
//
// SSH-Verbindungsmanager mit TUI (Terminal User Interface).
// Ermöglicht das Verwalten, Speichern und Aufbauen von SSH-Verbindungen
// mit Local-Port-Forwarding-Tunneln.
//
// @author Reisen macht Spaß... mit Pia und Dirk e.Kfm.
// @date   2026-10-16 10:00
package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// main ist der Einstiegspunkt des Programms.
// Initialisiert Logger, SSH-Manager, Konfiguration und startet die TUI.
// Beim Beenden: Schrift-/Fenstergröße merken, ggf. Update installieren.
//
// @date   2026-10-16 10:00
func main() {
	// Sicherstellen dass ein Terminal (TTY) vorhanden ist. Auf Linux/macOS:
	// Wird die Binary aus einem Dateimanager doppelgeklickt, fehlt ein TTY –
	// die Bubbletea-TUI wäre unsichtbar. ensureTerminal() startet die App dann
	// in einem verfügbaren Terminal-Emulator neu und gibt false zurück, damit
	// der parent-Prozess sofort beendet. Auf Windows ist dies ein No-Op
	// (AllocConsole regelt das bereits).
	if !ensureTerminal() {
		return
	}

	// Logger initialisieren
	logger := NewLogger()
	logger.Info("ssh-easy gestartet (%s/%s)", runtime.GOOS, runtime.GOARCH)

	// Build-Nummer lesen
	buildNumber := readBuildNumber()

	// Konfigurationsverzeichnis sicherstellen
	if err := EnsureConfigDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: Konfigurationsverzeichnis konnte nicht erstellt werden: %v\n", err)
		os.Exit(1)
	}

	// Konfigurationspfad ermitteln
	configPath, err := GetConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: Konfigurationspfad konnte nicht ermittelt werden: %v\n", err)
		os.Exit(1)
	}

	// Persistierte Konsolen-Schriftgröße und Fenstergröße anwenden (nur
	// Windows wirksam). Muss vor dem TUI-Start passieren, damit Bubbletea
	// gleich mit der korrekten Fenstergröße rendert. Reihenfolge: erst
	// Schrift, dann Fenster (die maximale Fenstergröße hängt von der Schrift ab).
	if cfg, cfgErr := LoadConfig(configPath); cfgErr == nil {
		applyStartupFontSize(cfg.FontSize)
		applyStartupWindowSize(cfg.WindowCols, cfg.WindowRows)
	}

	// Auto-Updater: Reste früherer Updates entfernen (z.B. ssh-easy.exe.old)
	updater := NewUpdater(buildNumber)
	cleanupUpdateLeftovers(updater.exePath, updater.downloadDir)

	// Datei-Manager: lokale Kopien früherer Sitzungen (F9 "Öffnen") entfernen
	cleanupStaleOpenDirs("", 24*time.Hour)

	// SSH-Manager erstellen
	sshManager := NewSSHManager(logger)

	// TUI-Modell erstellen und Programm starten
	model := NewAppModel(configPath, buildNumber, sshManager)
	model.SetUpdater(updater)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		logger.Error("TUI-Fehler: %v", err)
		fmt.Fprintf(os.Stderr, "Fehler beim Starten der TUI: %v\n", err)
		os.Exit(1)
	}

	// Alle Verbindungen sauber trennen
	sshManager.DisconnectAll()

	// Lokale Kopien dieser Sitzung (F9 "Öffnen") löschen
	cleanupExternalOpen()

	// Aktuelle Schriftgröße persistieren (nur Windows liefert > 0).
	// Deckt auch den nativen conhost-Zoom per Strg+Mausrad ab, der an der
	// Anwendung vorbeiläuft und sonst beim nächsten Start verloren wäre.
	persistFontSize(configPath, getConsoleFontSize())

	// Letzte Fenstergröße merken (nur wenn nicht maximiert/Vollbild)
	cols, rows, normal := queryWindowState()
	persistWindowSize(configPath, cols, rows, normal)

	// Heruntergeladenes Update beim Beenden installieren - nur wenn
	// Auto-Update beim Beenden noch eingeschaltet ist
	if final, ok := finalModel.(AppModel); ok && final.autoUpdate {
		if pending := updater.Pending(); pending != nil {
			if err := updater.Apply(); err != nil {
				logger.Error("Update auf v0.%d fehlgeschlagen: %v", pending.build, err)
			} else {
				logger.Info("Update auf v0.%d installiert", pending.build)
			}
		}
	}

	logger.Info("ssh-easy beendet")
}

// embeddedBuild enthält den Inhalt von build.txt zum Zeitpunkt des Kompilierens.
// Per go:embed fest in die Binary eingebaut - installierte Versionen haben
// keine build.txt neben der EXE (früher stand im Menü dann "vdev").
//
//go:embed build.txt
var embeddedBuild string

// readBuildNumber liefert die beim Kompilieren eingebettete Build-Nummer.
//
// @return string - Build-Nummer oder "dev" als Fallback
// @date   2026-10-14 16:10
func readBuildNumber() string {
	return parseEmbeddedBuild(embeddedBuild)
}

// parseEmbeddedBuild normalisiert den Inhalt von build.txt (Leerzeichen und
// Zeilenumbrüche entfernen). Leerer Inhalt ergibt "dev".
//
// @param raw - Rohinhalt der build.txt
// @return string - Build-Nummer oder "dev"
// @date   2026-10-14 16:10
func parseEmbeddedBuild(raw string) string {
	build := strings.TrimSpace(raw)
	if build == "" {
		return "dev"
	}
	return build
}

// formatAppTitle erzeugt den Programmtitel für das Hauptmenü. Das Format
// entspricht dem Release-Schema v0.<Build>, z.B. "ssh-easy v0.46 (Build 46)".
//
// @param build - Build-Nummer ("dev" oder leer = Entwicklungsversion)
// @return string - Titelzeile
// @date   2026-10-14 16:10
func formatAppTitle(build string) string {
	build = strings.TrimSpace(build)
	if build == "" || build == "dev" {
		return "ssh-easy (Build dev)"
	}
	return fmt.Sprintf("ssh-easy v0.%s (Build %s)", build, build)
}

// getBuildInfo gibt Build-Informationen als formatierten String zurück.
//
// @return string - Build-Info (Version, OS, Architektur)
// @date   2026-03-07 21:00
func getBuildInfo() string {
	return fmt.Sprintf("ssh-easy Build %s (%s/%s)",
		readBuildNumber(), runtime.GOOS, runtime.GOARCH)
}
