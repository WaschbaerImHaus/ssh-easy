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
// @date   2026-10-16 10:00
package main

import (
	"reflect"
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

// Virtuelle Tastencodes (Windows) für die Shift+Einfg-Erkennung
const (
	// vkShift - linke oder rechte Shift-Taste
	vkShift = 0x10
	// vkInsert - Einfg-Taste
	vkInsert = 0x2D
)

// keyIsDown fragt ab, ob eine Taste (virtueller Tastencode) gerade gedrückt
// ist. Funktionszeiger, damit Tests den Zustand vorgeben können.
// Windows: GetAsyncKeyState; andere Plattformen: immer false.
var keyIsDown = isKeyPressed

// shiftInsertPressed prüft ob Shift UND Einfg gedrückt sind. Shift allein
// reicht nicht: Unter Windows kommen auch Strg, Alt, Feststelltaste und Win
// als NUL-Rune an - Shift+Strg oder Alt+Shift (Layoutwechsel) würde sonst
// einfügen.
//
// @return bool - true bei Shift+Einfg
// @date   2026-10-16 10:00
func shiftInsertPressed() bool {
	return keyIsDown(vkShift) && keyIsDown(vkInsert)
}

// shiftInsertCSI ist die VT-Sequenz für Shift+Einfg (ESC [ 2 ; 2 ~), die
// manche Unix-Terminals senden. Bubbletea kennt sie nicht und meldet sie als
// unbekannte CSI-Sequenz.
const shiftInsertCSI = "\x1b[2;2~"

// isPasteKey prüft ob ein Tastendruck "Einfügen" bedeutet.
//
// Windows-Besonderheit: Bubbletea v1 bildet VK_INSERT nicht ab - Einfg und
// Shift+Einfg kommen als einzelne Rune 0x00 an, "shift+insert" entsteht nie.
// Deshalb wird bei der NUL-Rune der echte Tastenzustand abgefragt: Shift und
// Einfg gedrückt = Einfügen, alles andere wird ignoriert.
//
// @param msg - Tastendruck
// @return bool - true wenn eingefügt werden soll
// @date   2026-10-16 10:00
func isPasteKey(msg tea.KeyMsg) bool {
	if isPasteShortcut(msg.String()) {
		return true
	}
	return isNulKey(msg) && len(msg.Runes) == 1 && shiftInsertPressed()
}

// isNulKey prüft ob ein Tastendruck nur aus NUL-Runen besteht (unter
// Windows z.B. Einfg oder Strg+Plus). Solche Eingaben dürfen nicht als
// Zeichen in ein Eingabefeld gelangen.
//
// @param msg - Tastendruck
// @return bool - true bei reiner NUL-Eingabe
// @date   2026-10-16 10:00
func isNulKey(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return false
	}
	for _, r := range msg.Runes {
		if r != 0 {
			return false
		}
	}
	return true
}

// isShiftInsertSequence erkennt ESC[2;2~ in einer Bubbletea-Nachricht.
// Bubbletea liefert unbekannte CSI-Sequenzen als unexportierten []byte-Typ;
// geprüft wird deshalb per Reflection auf "Byte-Slice mit diesem Inhalt".
//
// @param msg - Beliebige Bubbletea-Nachricht
// @return bool - true bei Shift+Einfg-Sequenz
// @date   2026-10-16 10:00
func isShiftInsertSequence(msg tea.Msg) bool {
	if msg == nil {
		return false
	}
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return false
	}
	return string(v.Bytes()) == shiftInsertCSI
}

// pasteIntoFocusedInput fügt die erste Zeile der Zwischenablage in das
// fokussierte Eingabefeld der aktuellen Ansicht ein (Verbindungsformular,
// Passwortfeld oder Keygen-Formular). In anderen Ansichten passiert nichts.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando des Eingabefelds
// @date   2026-10-16 10:00
func (m AppModel) pasteIntoFocusedInput() (tea.Model, tea.Cmd) {
	text, ok := readClipboardLine()
	if !ok {
		return m, nil
	}
	paste := makePasteKeyMsg(text)
	var cmd tea.Cmd
	switch m.state {
	case ViewCreate, ViewEdit:
		m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(paste)
	case ViewConnect:
		m.passwordInput, cmd = m.passwordInput.Update(paste)
	case ViewKeygen:
		m.keygenInputs[m.keygenFocused], cmd = m.keygenInputs[m.keygenFocused].Update(paste)
	case ViewExport:
		if m.exportFocusPath {
			m.exportPathInput, cmd = m.exportPathInput.Update(paste)
		}
	}
	return m, cmd
}
