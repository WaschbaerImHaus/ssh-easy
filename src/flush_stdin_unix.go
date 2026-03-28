// Plattform-spezifisches Leeren des stdin-Puffers für Unix/Linux/macOS.
//
// Nach dem Ende einer SSH-Terminal-Session können Rest-Bytes (z.B. '\r' vom
// "exit"-Enter) im Kernel-tty-Puffer stehen. Wenn Bubbletea seinen Input-
// Reader neu startet, liest er diesen Rest-Byte als ersten "Tastendruck" –
// der Nutzer muss dann zweimal drücken. Diese Funktion leert den Puffer.
//
// @author Kurt Ingwer
// @date   2026-03-28 10:00

//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushStdinBuffer verwirft alle gepufferten Bytes in stdin.
// Setzt den Dateideskriptor kurz auf nicht-blockierend, liest ihn leer,
// stellt dann blockierendes Lesen wieder her.
//
// @date 2026-03-28 10:00
func flushStdinBuffer() {
	fd := int(os.Stdin.Fd())

	// Nicht-blockierenden Modus aktivieren: Read gibt sofort EAGAIN zurück wenn leer
	if err := unix.SetNonblock(fd, true); err != nil {
		return
	}
	// Blockierenden Modus beim Verlassen immer wiederherstellen
	defer unix.SetNonblock(fd, false)

	// Puffer leeren bis nichts mehr da ist
	buf := make([]byte, 256)
	for {
		n, err := unix.Read(fd, buf)
		if n == 0 || err != nil {
			// Puffer leer (EAGAIN) oder Fehler → fertig
			return
		}
	}
}
