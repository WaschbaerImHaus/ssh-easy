// Paket main - Clipboard-Paste in TUI-Formularfeldern
//
// Ermöglicht das Einfügen aus der Zwischenablage per Strg+V oder Shift+Einf
// in allen Bubbletea-Eingabefeldern (Verbindungsformular, Passwort-Eingabe,
// Keygen-Formular) - z.B. um ein Passwort aus einem Passwort-Manager
// einzufügen. Nutzt denselben readClipboard-Funktionszeiger wie das
// SSH-Terminal-Paste (clipboard_paste.go), damit Tests stubben können.
//
// Da alle Formularfelder einzeilig sind, wird nur die erste Zeile des
// Clipboard-Inhalts übernommen - eingeschleppte Zeilenumbrüche (etwa beim
// Kopieren aus Dokumenten) lösen so kein versehentliches Absenden aus.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:20
package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// isPasteShortcut prüft ob ein Tastendruck ein Einfüge-Kürzel ist.
// Unterstützt Strg+V (überall) und Shift+Einf (klassisches Terminal-Paste).
//
// @param key - Ergebnis von msg.String() eines tea.KeyMsg
// @return bool - true wenn der Nutzer einfügen möchte
// @date   2026-07-03 19:20
func isPasteShortcut(key string) bool {
	switch key {
	case "ctrl+v", "shift+insert":
		return true
	}
	return false
}

// readClipboardLine liest die Zwischenablage und liefert die erste Zeile.
// Zeilenumbrüche (CR, LF, CRLF) beenden die Zeile; führende/folgende
// Leerzeichen bleiben erhalten (könnten Teil eines Passworts sein).
//
// @return string - Erste Zeile des Clipboard-Inhalts
// @return bool - false wenn Clipboard leer oder nicht lesbar
// @date   2026-07-03 19:20
func readClipboardLine() (string, bool) {
	text, err := readClipboard()
	if err != nil {
		return "", false
	}
	// Nur die erste Zeile übernehmen (Formularfelder sind einzeilig)
	if idx := strings.IndexAny(text, "\r\n"); idx >= 0 {
		text = text[:idx]
	}
	if text == "" {
		return "", false
	}
	return text, true
}

// makePasteKeyMsg verpackt Text als Runes-KeyMsg mit Paste-Flag.
// Bubbles' textinput fügt die Runes an der Cursor-Position ein -
// exakt so, als hätte der Nutzer den Text getippt.
//
// @param text - Einzufügender Text (eine Zeile)
// @return tea.KeyMsg - Nachricht für textinput.Update()
// @date   2026-07-03 19:20
func makePasteKeyMsg(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}
