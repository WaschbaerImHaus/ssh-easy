// Plattform-spezifisches Leeren des stdin-Puffers für Windows.
//
// Nach dem Ende einer SSH-Terminal-Session können Rest-Bytes (z.B. '\r' vom
// "exit"-Enter) im Console-Input-Puffer stehen. FlushConsoleInputBuffer
// verwirft alle wartenden Eingabe-Records auf einmal – ohne Lesen in einer Schleife.
//
// @author Kurt Ingwer
// @date   2026-03-28 10:00

//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// flushStdinBuffer verwirft alle gepufferten Eingaben aus dem Windows-Console-Puffer.
// Nutzt FlushConsoleInputBuffer, das effizienter als byte-weises Lesen ist.
//
// @date 2026-03-28 10:00
func flushStdinBuffer() {
	handle := windows.Handle(os.Stdin.Fd())
	// Alle ausstehenden Eingabe-Records (Tasten, Mausereignisse usw.) verwerfen
	_ = windows.FlushConsoleInputBuffer(handle)
}
