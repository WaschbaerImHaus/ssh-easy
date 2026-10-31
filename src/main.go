// Paket main - Einstiegspunkt für ssh-easy
//
// SSH-Verbindungsmanager mit TUI (Terminal User Interface).
// Ermöglicht das Verwalten, Speichern und Aufbauen von SSH-Verbindungen
// mit Local-Port-Forwarding-Tunneln.
//
// @author Kurt Ingwer
// @date   2026-10-31 07:02
package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// main ist der Einstiegspunkt des Programms.
// Initialisiert Logger, SSH-Manager, Konfiguration und startet die TUI.
// Beim Beenden: Verbindungen trennen, lokale Kopien löschen, Schrift-/
// Fenstergröße merken und ggf. das Update installieren - auch wenn das
// Fenster über das Schließkreuz oder Alt+F4 (Windows Terminal) geschlossen
// wird (Linux: SIGHUP, Windows: CTRL_CLOSE_EVENT = SIGTERM; Build 87).
//
// Mit "--install-update <setup> <sha256> <pid>" als erstem Argument läuft
// das Programm nur als Helfer, der das Setup nach dem Ende des Hauptprozesses
// startet (siehe runInstallUpdateHelper), und nie als TUI.
//
// @date   2026-10-31 07:02
func main() {
	// Helfer-Modus für das Update (Windows): kein Terminal, keine TUI, keine
	// Aufräumarbeiten - das Setup-Verzeichnis gehört gerade diesem Helfer.
	// Deshalb VOR ensureTerminal und cleanupUpdateLeftovers.
	if isInstallHelperInvocation(os.Args) {
		os.Exit(runInstallUpdateHelper(os.Args[2:], NewLogger()))
	}

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
	// Korrekturen am waagrechten Bildlaufbalken der Konsole protokollieren
	hscrollLog = logger.Info
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
	// Vorher den Systemstandard festhalten - Ziel von "r" (zurücksetzen)
	defaults := captureConsoleDefaults()
	if cfg, cfgErr := LoadConfig(configPath); cfgErr == nil {
		applyStartupFontSize(cfg.FontSize)
		applyStartupWindowSize(cfg.WindowCols, cfg.WindowRows)
	}

	// Auto-Updater: Reste früherer Updates entfernen (z.B. ssh-easy.exe.old).
	// Setup-Ordner erst ab setupLeftoverMinAge: Ein Helfer eines eben
	// beendeten Laufs wartet evtl. noch auf die UAC-Bestätigung.
	updater := NewUpdater(buildNumber)
	cleanupUpdateLeftovers(updater.exePath, updater.downloadDir, setupLeftoverMinAge)

	// Datei-Manager: lokale Kopien früherer Sitzungen (F9 "Öffnen") entfernen
	cleanupStaleOpenDirs("", 24*time.Hour)

	// SSH-Manager erstellen
	sshManager := NewSSHManager(logger)

	// TUI-Modell erstellen und Programm starten
	model := NewAppModel(configPath, buildNumber, sshManager)
	model.SetUpdater(updater)
	model.SetConsoleDefaults(defaults)
	// Update beim Beenden installieren? Der Updater muss den Schalter von
	// Anfang an kennen: Der Notfall-Weg (Signal-Goroutine) kann das Modell
	// nicht lesen. toggleAutoUpdate hält den Wert danach aktuell. Doppelt
	// setzen (falls SetUpdater es auch tut) schadet nicht.
	updater.SetInstallOnExit(model.autoUpdate)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Aufgaben beim Beenden - je genau einmal, ob normal (nach p.Run) oder im
	// Notfall (Fenster geschlossen, normales Ende kommt nicht rechtzeitig):
	//   - disconnect: alle Verbindungen sauber trennen
	//   - cleanup: lokale Kopien dieser Sitzung (F9 "Öffnen") löschen
	//   - persist: Schrift-/Fenstergröße merken (nur Windows liefert Werte;
	//     deckt auch den conhost-Zoom per Strg+Mausrad ab; Werte gleich dem
	//     Systemstandard werden als 0 gespeichert, siehe persistConsoleState)
	//   - apply: heruntergeladenes Update installieren, wenn Auto-Update
	//     eingeschaltet ist (ApplyOnExit prüft das selbst)
	tasks := &exitTasks{
		disconnect: sshManager.DisconnectAll,
		cleanup:    cleanupExternalOpen,
		persist:    func() { persistConsoleState(configPath, defaults) },
		apply:      func() { updater.ApplyOnExit(logger) },
	}

	// Schließen des Fensters abfangen (Linux: SIGHUP/SIGTERM, Windows:
	// CTRL_CLOSE_EVENT als SIGTERM). signal.Stop wird BEWUSST nie gerufen:
	// Unter Windows hält der registrierte Handler den Prozess am Leben, bis
	// die Aufgaben erledigt sind (sonst sofort ExitProcess). Puffer 2, damit
	// ein zweites Signal nicht verloren geht oder blockiert.
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, closeSignals()...)
	allDone := make(chan struct{})
	var closeRequested atomic.Bool
	// Der Watcher ruft quit per "go" auf: p.Quit() blockiert, solange die
	// Ereignisschleife steht (z. B. während tea.Exec im SSH-Terminal).
	go watchCloseSignals(sigs, func() {
		closeRequested.Store(true)
		p.Quit()
	}, allDone, tasks, closeGracePeriod, os.Exit, logger)

	_, err = p.Run()
	// Auch bei einem Fehler aus Run() laufen alle Aufgaben (vorher endete
	// das Programm mit os.Exit(1), und das Update wurde nie installiert).
	code := finishRun(err, closeRequested.Load(), tasks, allDone, logger)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "Fehler in der TUI: %v\n", err)
	}
	logger.Info("ssh-easy beendet")
	if code != 0 {
		os.Exit(code)
	}
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
