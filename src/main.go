// Paket main - Einstiegspunkt fuer ssh-easy
//
// SSH-Verbindungsmanager mit TUI (Terminal User Interface).
// Ermoeglicht das Verwalten, Speichern und Aufbauen von SSH-Verbindungen
// mit Local-Port-Forwarding-Tunneln.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 21:00
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// main ist der Einstiegspunkt des Programms.
// Initialisiert Logger, SSH-Manager, Konfiguration und startet die TUI.
//
// @date   2026-03-07 21:00
func main() {
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

	// SSH-Manager erstellen
	sshManager := NewSSHManager(logger)

	// TUI-Modell erstellen und Programm starten
	model := NewAppModel(configPath, buildNumber, sshManager)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		logger.Error("TUI-Fehler: %v", err)
		fmt.Fprintf(os.Stderr, "Fehler beim Starten der TUI: %v\n", err)
		os.Exit(1)
	}

	// Alle Verbindungen sauber trennen
	sshManager.DisconnectAll()
	logger.Info("ssh-easy beendet")
}

// readBuildNumber liest die Build-Nummer aus der build.txt-Datei.
//
// @return string - Build-Nummer oder "dev" als Fallback
// @date   2026-03-07 21:00
func readBuildNumber() string {
	paths := []string{}

	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		paths = append(paths,
			filepath.Join(execDir, "build.txt"),
			filepath.Join(execDir, "..", "src", "build.txt"),
		)
	}

	cwd, err := os.Getwd()
	if err == nil {
		paths = append(paths,
			filepath.Join(cwd, "build.txt"),
			filepath.Join(cwd, "src", "build.txt"),
		)
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}

	return "dev"
}

// getBuildInfo gibt Build-Informationen als formatierten String zurueck.
//
// @return string - Build-Info (Version, OS, Architektur)
// @date   2026-03-07 21:00
func getBuildInfo() string {
	return fmt.Sprintf("ssh-easy Build %s (%s/%s)",
		readBuildNumber(), runtime.GOOS, runtime.GOARCH)
}
