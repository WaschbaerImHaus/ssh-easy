// Paket main - Terminal-Launcher für Windows (No-Op)
//
// Auf Windows wird das Terminal-Problem bereits durch AllocConsole() in
// icon_windows.go gelöst – ssh-easy ist als GUI-Subsystem (-H windowsgui)
// gebaut und allokiert sich selbst eine Konsole. ensureTerminal() ist deshalb
// auf Windows ein No-Op und gibt immer true zurück.
//
// @author Kurt Ingwer
// @date   2026-04-19 00:00

//go:build windows

package main

// ensureTerminal ist auf Windows ein No-Op.
// AllocConsole() hat bereits beim Programmstart eine Konsole bereitgestellt.
//
// @return bool - immer true (weitermachen)
// @date   2026-04-19 00:00
func ensureTerminal() bool {
	return true
}
