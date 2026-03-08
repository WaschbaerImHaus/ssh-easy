// Plattform-spezifische Terminal-Behandlung für Windows.
//
// Windows hat kein SIGWINCH-Äquivalent. Stattdessen wird im kurzen Intervall
// gepollt ob sich die Konsolengröße geändert hat.
//
// Außerdem muss auf Windows die Virtual-Terminal-Verarbeitung explizit
// aktiviert werden, damit ANSI/VT-Escape-Sequenzen (Farben, Cursor usw.)
// korrekt dargestellt werden statt als Rohtext (z.B. "←[01;32m").
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00

//go:build windows

package main

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// waitForResizeSignal wartet auf Windows durch kurzes Polling (250ms Intervall).
// Windows bietet kein SIGWINCH, daher ist regelmäßiges Prüfen die einzige Option.
//
// @param stop - Kanal zum Beenden der Wartefunktion
// @date   2026-03-08 00:00
func waitForResizeSignal(stop <-chan struct{}) {
	select {
	case <-time.After(250 * time.Millisecond):
		// Nach 250ms zurückkehren damit watchTerminalResize die Größe prüft
	case <-stop:
		// Session beendet → Goroutine beenden
	}
}

// setupConsoleVT aktiviert die Virtual-Terminal-Verarbeitung auf der Windows-Konsole.
// Ohne diese Einstellung werden ANSI-Escape-Sequenzen (Farben, Cursorbewegungen)
// als Rohtext angezeigt, z.B. "←[01;32m" statt grüner Schrift.
//
// Gibt eine Funktion zurück die den ursprünglichen Konsolenmodus wiederherstellt.
//
// @return func() - Wiederherstellungsfunktion (immer aufrufen wenn fertig)
// @date   2026-03-08 00:00
func setupConsoleVT() func() {
	stdoutHandle := windows.Handle(os.Stdout.Fd())
	stdinHandle := windows.Handle(os.Stdin.Fd())

	// Aktuelle Konsolenmodi sichern für spätere Wiederherstellung
	var origOutMode, origInMode uint32
	windows.GetConsoleMode(stdoutHandle, &origOutMode)
	windows.GetConsoleMode(stdinHandle, &origInMode)

	// Ausgabe: ANSI/VT-Sequenzen verarbeiten statt als Text ausgeben.
	// ENABLE_PROCESSED_OUTPUT (0x0001): Grundlegende Ausgabeverarbeitung
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING (0x0004): Farben, Cursor, VT100-Emulation
	newOutMode := origOutMode |
		windows.ENABLE_PROCESSED_OUTPUT |
		windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	windows.SetConsoleMode(stdoutHandle, newOutMode)

	// Eingabe: VT-Sequenzen von der Tastatur zulassen (Pfeiltasten, F-Tasten usw.)
	// ENABLE_VIRTUAL_TERMINAL_INPUT (0x0200): Pfeiltasten als VT-Escape-Sequenzen senden
	newInMode := origInMode | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	windows.SetConsoleMode(stdinHandle, newInMode)

	// Wiederherstellungsfunktion zurückgeben (via defer in Run() aufrufen)
	return func() {
		windows.SetConsoleMode(stdoutHandle, origOutMode)
		windows.SetConsoleMode(stdinHandle, origInMode)
	}
}
