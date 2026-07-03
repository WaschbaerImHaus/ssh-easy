// Paket main - Konsolen-Scroll-Snap (Unix-Variante, No-Op)
//
// Auf Linux/macOS verwaltet der Terminal-Emulator den Scrollback-Puffer
// selbst - praktisch alle Emulatoren (gnome-terminal, konsole, xterm, ...)
// springen bei Tastatureingabe automatisch zur Eingabezeile zurück
// ("scroll on keystroke"). Eine Anwendung kann und muss dort nichts tun.
// Das Pendant console_scroll_windows.go scrollt den conhost-Viewport aktiv.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:35

//go:build !windows

package main

// snapConsoleToBottom ist auf Unix ein No-Op.
// Der Terminal-Emulator übernimmt das Zurückspringen selbst.
//
// @date   2026-07-03 19:35
func snapConsoleToBottom() {}
