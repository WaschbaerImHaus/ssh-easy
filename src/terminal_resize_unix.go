// Plattform-spezifische Terminal-Resize-Behandlung für Unix/Linux/macOS.
//
// Nutzt SIGWINCH (Signal Window CHange) um Terminalgröße-Änderungen
// effizient zu erkennen – kein Polling nötig.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00

//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// waitForResizeSignal wartet auf SIGWINCH (Terminal-Resize-Signal) oder Stop-Signal.
// Auf Unix/Linux wird kein Polling benötigt – das OS informiert uns direkt.
//
// @param stop - Kanal zum Beenden der Wartefunktion
// @date   2026-03-08 00:00
func waitForResizeSignal(stop <-chan struct{}) {
	// SIGWINCH-Kanal einrichten (gepuffert damit kein Signal verloren geht)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		// Terminalgröße hat sich geändert → zurückkehren damit watchTerminalResize prüft
	case <-stop:
		// Session beendet → Goroutine beenden
	}
}
