// Abbrechbare Tastatureingabe für Unix/Linux/macOS.
//
// Nutzt muesli/cancelreader (epoll unter Linux, kqueue/select unter BSD und
// macOS) - dieselbe Bibliothek, mit der Bubbletea seine eigene Eingabe stoppt.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build !windows

package main

import (
	"os"

	"github.com/muesli/cancelreader"
)

// newTerminalInput erstellt eine abbrechbare Eingabe über einer Datei
// (typischerweise os.Stdin).
//
// @param f - Eingabedatei
// @return terminalInput - Abbrechbare Eingabe
// @return error - Fehler beim Einrichten
// @date   2026-10-16 10:00
func newTerminalInput(f *os.File) (terminalInput, error) {
	return cancelreader.NewReader(f)
}
