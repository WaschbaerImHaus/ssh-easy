// Paket main - Terminal-Launcher für Unix-Systeme (Linux, macOS, BSD)
//
// Hintergrund:
//   ssh-easy ist eine TUI (Bubbletea), keine echte GUI. Wird die Binary aus
//   einem Dateimanager (Nemo unter Mint, Nautilus unter Gnome, Dolphin unter
//   KDE) per Doppelklick gestartet, fehlt ein TTY – Bubbletea kann die
//   Oberfläche nicht rendern und die Anwendung erscheint "tot".
//
//   Analog zu Windows (AllocConsole) stellen wir auf Unix sicher, dass ein
//   Terminal vorhanden ist: Falls nicht, startet sich ssh-easy selbst in einem
//   verfügbaren Terminal-Emulator und beendet den ursprünglichen Prozess.
//
//   Unterscheidung zu headless (SSH ohne TTY, Pipe, Cron):
//   - DISPLAY oder WAYLAND_DISPLAY gesetzt → GUI-Kontext, Relaunch sinnvoll
//   - Kein DISPLAY → kein Weg ein Terminal zu öffnen, Fehler anzeigen
//
// @author Kurt Ingwer
// @date   2026-04-19 00:00

//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/term"
)

// terminalCandidates listet Terminal-Emulatoren in Suchreihenfolge.
// x-terminal-emulator ist die Debian/Ubuntu/Mint-Alternative und spiegelt die
// vom Nutzer konfigurierte Standard-Terminal-App – deshalb zuerst.
var terminalCandidates = []string{
	"x-terminal-emulator",
	"gnome-terminal",
	"konsole",
	"xfce4-terminal",
	"mate-terminal",
	"lxterminal",
	"tilix",
	"alacritty",
	"kitty",
	"urxvt",
	"rxvt",
	"xterm",
}

// findTerminalEmulator sucht einen verfügbaren Terminal-Emulator.
// Erst wird $TERMINAL geprüft (falls der Nutzer eine Präferenz gesetzt hat),
// danach die Kandidatenliste in Reihenfolge via exec.LookPath.
// Rückgabe: absoluter Pfad oder leerer String, wenn nichts gefunden wurde.
//
// @return string - absoluter Pfad zum Emulator oder ""
// @date   2026-04-19 00:00
func findTerminalEmulator() string {
	// Vom Nutzer gesetzte Präferenz hat Vorrang
	if preferred := os.Getenv("TERMINAL"); preferred != "" {
		if path, err := exec.LookPath(preferred); err == nil {
			return path
		}
	}
	// Standard-Kandidaten durchgehen
	for _, name := range terminalCandidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// shouldRelaunchInTerminal entscheidet, ob ein Self-Relaunch sinnvoll ist.
//
// Kriterien:
//   - Kein TTY vorhanden (sonst läuft alles normal)
//   - UND eine GUI-Session ist aktiv ($DISPLAY oder $WAYLAND_DISPLAY)
//
// Ohne Display (echte Headless-Umgebung, SSH ohne -t, Cron) wäre ein Terminal-
// Start unmöglich – dann Fehler auf stderr statt Endlos-Fehlschlag-Schleife.
//
// @param hasTTY - hat stdin/stdout ein angeschlossenes Terminal?
// @param display - Wert der DISPLAY-Umgebungsvariable (X11)
// @param waylandDisplay - Wert der WAYLAND_DISPLAY-Variable (Wayland)
// @return bool - true wenn Relaunch versucht werden soll
// @date   2026-04-19 00:00
func shouldRelaunchInTerminal(hasTTY bool, display, waylandDisplay string) bool {
	if hasTTY {
		return false
	}
	return display != "" || waylandDisplay != ""
}

// ensureTerminal stellt sicher, dass die Anwendung in einem Terminal läuft.
//
// Rückgabe:
//   - true  → Weitermachen, TUI kann gestartet werden (entweder TTY vorhanden
//             oder wir sind bereits das Re-Launched-Kind).
//   - false → Wir haben einen Relaunch gestartet, der Aufrufer soll sofort
//             os.Exit(0) aufrufen damit sich der parent-Prozess (Dateimanager)
//             zurückmelden kann.
//
// Zusatz-Schutz gegen Endlos-Relaunch: die Umgebungsvariable SSH_EASY_RELAUNCHED
// wird beim Relaunch gesetzt. Erkennen wir sie, wird kein zweiter Versuch gestartet.
//
// @return bool - true = weitermachen, false = exit
// @date   2026-04-19 00:00
func ensureTerminal() bool {
	// Schon einmal relaunched? Nicht nochmal versuchen.
	if os.Getenv("SSH_EASY_RELAUNCHED") == "1" {
		return true
	}

	hasTTY := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	display := os.Getenv("DISPLAY")
	wayland := os.Getenv("WAYLAND_DISPLAY")

	if !shouldRelaunchInTerminal(hasTTY, display, wayland) {
		return true
	}

	// Pfad zur eigenen Binary ermitteln (absoluter Pfad, folgt Symlinks auflösend).
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ssh-easy: Eigener Pfad konnte nicht ermittelt werden: %v\n", err)
		return true
	}

	emulator := findTerminalEmulator()
	if emulator == "" {
		// Kein Terminal verfügbar – Fehler auf stderr und weitermachen;
		// die TUI wird zwar nichts sinnvolles rendern, aber ein harter Exit
		// hinterlässt den Nutzer orientierungslos.
		fmt.Fprintln(os.Stderr, "ssh-easy: Kein Terminal-Emulator im PATH gefunden (xterm, gnome-terminal, konsole, ...)")
		fmt.Fprintln(os.Stderr, "Setze $TERMINAL auf einen bevorzugten Emulator oder installiere einen der Standard-Kandidaten.")
		return true
	}

	// Child-Prozess im Terminal starten
	cmd := exec.Command(emulator, "-e", executable)
	// Endlos-Schleifen-Schutz
	cmd.Env = append(os.Environ(), "SSH_EASY_RELAUNCHED=1")
	// Release() → parent kann direkt beenden, das Terminal läuft eigenständig weiter
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ssh-easy: Terminal-Emulator %q konnte nicht gestartet werden: %v\n", emulator, err)
		return true
	}
	_ = cmd.Process.Release()
	// Parent beenden – Dateimanager-Klick ist "fertig", das Terminal zeigt die TUI.
	return false
}
