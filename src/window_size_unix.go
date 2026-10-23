// Fenstergröße für Unix/Linux/macOS.
//
// Das Fenster gehört dem Terminal-Emulator (GNOME Terminal, Konsole, ...),
// der seine Größe selbst verwaltet und merkt. ssh-easy speichert hier nichts.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build !windows

package main

// getConsoleWindowState ist unter Unix nicht ermittelbar.
//
// @return int - 0
// @return int - 0
// @return bool - false (nicht speichern)
// @date   2026-10-16 10:00
func getConsoleWindowState() (int, int, bool) {
	return 0, 0, false
}

// setConsoleWindowSize ist unter Unix ein No-Op.
//
// @param cols - Spalten (ignoriert)
// @param rows - Zeilen (ignoriert)
// @return bool - false
// @date   2026-10-16 10:00
func setConsoleWindowSize(cols, rows int) bool {
	return false
}

// removeHorizontalScroll ist unter Unix ein No-Op (das Terminal regelt das).
//
// @return bool - immer false
// @date   2026-10-23 19:32
func removeHorizontalScroll() bool {
	return false
}
