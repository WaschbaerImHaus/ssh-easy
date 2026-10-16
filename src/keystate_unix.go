// Plattform-spezifische Abfrage des Tastenzustands für Unix/Linux/macOS.
//
// Unter Unix liefert das Terminal Shift+Einfg entweder selbst als Einfügen
// (Bracketed Paste) oder als VT-Sequenz ESC[2;2~ - der Tastenzustand muss
// nicht abgefragt werden.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build !windows

package main

// isKeyPressed ist unter Unix nicht ermittelbar und liefert false.
//
// @param vk - Virtueller Tastencode (ignoriert)
// @return bool - immer false
// @date   2026-10-16 10:00
func isKeyPressed(vk int) bool {
	return false
}
