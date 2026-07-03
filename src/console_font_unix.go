// Paket main - Konsolen-Schriftgröße (Unix-Variante, No-Op)
//
// Unter Linux/macOS läuft ssh-easy im Terminal-Emulator des Nutzers
// (gnome-terminal, konsole, xterm, ...). Dessen Schriftgröße kann eine
// TUI-Anwendung nicht ändern - das ist Sache des Emulators (dort meist
// selbst per Strg+Plus/Minus änderbar). Alle Funktionen sind daher No-Ops.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:40

//go:build !windows

package main

// getConsoleFontSize liefert auf Unix immer 0 (unbekannt/nicht steuerbar).
//
// @return int - 0 = Schriftgröße nicht ermittelbar
// @date   2026-07-03 19:40
func getConsoleFontSize() int {
	return 0
}

// setConsoleFontSize ist auf Unix ein No-Op.
//
// @param size - Gewünschte Schriftgröße (ignoriert)
// @return bool - immer false (nicht unterstützt)
// @date   2026-07-03 19:40
func setConsoleFontSize(size int) bool {
	return false
}
