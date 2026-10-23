// Paket main - Tests für Shift+Einfg in Formular und Passwortfeld (Build 48)
//
// Hintergrund (Bug): Bubbletea v1 kennt unter Windows keine Taste
// "shift+insert" - VK_INSERT ist nicht gemappt und kommt als Rune 0x00 an.
// Der bisherige Vergleich auf "shift+insert" griff deshalb nie. Jetzt wird
// die NUL-Rune zusammen mit dem tatsächlichen Shift-Zustand ausgewertet.
// Unter Unix liefern manche Terminals ESC[2;2~ als unbekannte CSI-Sequenz.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stubKeys setzt den Zustand einzelner Tasten (virtuelle Tastencodes)
// für die Testdauer.
func stubKeys(t *testing.T, down ...int) {
	t.Helper()
	old := keyIsDown
	pressed := map[int]bool{}
	for _, vk := range down {
		pressed[vk] = true
	}
	keyIsDown = func(vk int) bool { return pressed[vk] }
	t.Cleanup(func() { keyIsDown = old })
}

// stubShift simuliert Shift+Einfg (down=true) bzw. keine Taste (down=false).
func stubShift(t *testing.T, down bool) {
	t.Helper()
	if down {
		stubKeys(t, vkShift, vkInsert)
	} else {
		stubKeys(t)
	}
}

// nulKey ist das, was Bubbletea unter Windows für (Shift+)Einfg liefert.
var nulKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}

// TestIsPasteKeyWindowsShiftInsert prüft die NUL-Rune mit gedrückter Shift-Taste.
func TestIsPasteKeyWindowsShiftInsert(t *testing.T) {
	stubShift(t, true)
	if !isPasteKey(nulKey) {
		t.Error("NUL-Rune mit Shift muss als Einfügen gelten")
	}
}

// TestIsPasteKeyPlainInsertIgnored prüft dass Einfg ohne Shift nicht einfügt.
func TestIsPasteKeyPlainInsertIgnored(t *testing.T) {
	stubShift(t, false)
	if isPasteKey(nulKey) {
		t.Error("NUL-Rune ohne Shift darf nicht einfügen")
	}
}

// TestIsPasteKeyShiftWithOtherKeyIgnored prüft den Review-Fund: Unter
// Windows kommen auch Strg, Alt, Feststelltaste und Win als NUL-Rune an.
// Shift+Strg oder Alt+Shift (Tastaturlayout wechseln) darf NICHT einfügen -
// nur Shift zusammen mit Einfg.
func TestIsPasteKeyShiftWithOtherKeyIgnored(t *testing.T) {
	stubKeys(t, vkShift)
	if isPasteKey(nulKey) {
		t.Error("Shift + andere Taste ohne Zeichen (z.B. Strg/Alt) darf nicht einfügen")
	}
	stubKeys(t, vkInsert)
	if isPasteKey(nulKey) {
		t.Error("Einfg ohne Shift darf nicht einfügen")
	}
}

// TestIsPasteKeyClassicShortcuts prüft Strg+V und normale Tasten.
func TestIsPasteKeyClassicShortcuts(t *testing.T) {
	stubShift(t, true)
	if !isPasteKey(tea.KeyMsg{Type: tea.KeyCtrlV}) {
		t.Error("Strg+V muss einfügen")
	}
	if isPasteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}) {
		t.Error("großes A (mit Shift getippt) darf nicht einfügen")
	}
	if isPasteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0, 0}}) {
		t.Error("mehrere NUL-Runen sind kein Einzeltastendruck")
	}
}

// TestIsNulKey prüft die Erkennung reiner NUL-Eingaben.
func TestIsNulKey(t *testing.T) {
	if !isNulKey(nulKey) {
		t.Error("NUL-Rune nicht erkannt")
	}
	if isNulKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) || isNulKey(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Error("normale Tasten fälschlich als NUL erkannt")
	}
}

// fakeCSI ahmt Bubbleteas unexportierten unknownCSISequenceMsg nach
// (ebenfalls ein []byte-Typ).
type fakeCSI []byte

// TestIsShiftInsertSequence prüft die Erkennung von ESC[2;2~.
func TestIsShiftInsertSequence(t *testing.T) {
	if !isShiftInsertSequence(fakeCSI("\x1b[2;2~")) {
		t.Error("ESC[2;2~ nicht erkannt")
	}
	if isShiftInsertSequence(fakeCSI("\x1b[2~")) || isShiftInsertSequence(tea.KeyMsg{Type: tea.KeyEnter}) || isShiftInsertSequence(nil) {
		t.Error("falsche Sequenz als Shift+Einfg erkannt")
	}
}

// newFormModel liefert ein Modell im Verbindungsformular.
func newFormModel(t *testing.T) AppModel {
	t.Helper()
	m := NewAppModel(filepath.Join(t.TempDir(), "c.json"), "48", NewSSHManager(NewLogger()))
	m.state = ViewCreate
	m.focusedInput = fieldHost
	m.inputs[fieldHost].Focus()
	return m
}

// TestFormShiftInsertPastes prüft Shift+Einfg (Windows) im Formular.
func TestFormShiftInsertPastes(t *testing.T) {
	restore := stubClipboard("10.0.0.42", nil)
	defer restore()
	stubShift(t, true)

	updated, _ := newFormModel(t).Update(nulKey)
	if got := updated.(AppModel).inputs[fieldHost].Value(); got != "10.0.0.42" {
		t.Errorf("Host nach Shift+Einfg = %q", got)
	}
}

// TestFormPlainInsertInsertsNothing prüft dass Einfg allein kein NUL ins
// Feld schreibt.
func TestFormPlainInsertInsertsNothing(t *testing.T) {
	restore := stubClipboard("nicht einfügen", nil)
	defer restore()
	stubShift(t, false)

	updated, _ := newFormModel(t).Update(nulKey)
	if got := updated.(AppModel).inputs[fieldHost].Value(); got != "" {
		t.Errorf("Feld nach Einfg = %q, erwartet leer", got)
	}
}

// TestPasswordShiftInsertPastes prüft Shift+Einfg im Passwortfeld.
func TestPasswordShiftInsertPastes(t *testing.T) {
	restore := stubClipboard("p@ss", nil)
	defer restore()
	stubShift(t, true)

	m := NewAppModel(filepath.Join(t.TempDir(), "c.json"), "48", NewSSHManager(NewLogger()))
	m.state = ViewConnect
	m.passwordInput.Focus()
	updated, _ := m.Update(nulKey)
	if got := updated.(AppModel).passwordInput.Value(); got != "p@ss" {
		t.Errorf("Passwort nach Shift+Einfg = %q", got)
	}
}

// TestKeygenShiftInsertPastes prüft Shift+Einfg im Keygen-Formular.
func TestKeygenShiftInsertPastes(t *testing.T) {
	restore := stubClipboard("~/.ssh/id_neu", nil)
	defer restore()
	stubShift(t, true)

	m := NewAppModel(filepath.Join(t.TempDir(), "c.json"), "48", NewSSHManager(NewLogger()))
	m.state = ViewKeygen
	m.keygenInputs = createKeygenInputs()
	m.keygenFocused = keygenFieldPath
	m.keygenInputs[keygenFieldPath].Focus()
	updated, _ := m.Update(nulKey)
	if got := updated.(AppModel).keygenInputs[keygenFieldPath].Value(); got != "~/.ssh/id_neu" {
		t.Errorf("Pfad nach Shift+Einfg = %q", got)
	}
}

// TestUnixShiftInsertSequencePastesIntoForm prüft ESC[2;2~ (Unix-Terminals).
func TestUnixShiftInsertSequencePastesIntoForm(t *testing.T) {
	restore := stubClipboard("server.lan", nil)
	defer restore()

	updated, _ := newFormModel(t).Update(fakeCSI("\x1b[2;2~"))
	if got := updated.(AppModel).inputs[fieldHost].Value(); got != "server.lan" {
		t.Errorf("Host nach ESC[2;2~ = %q", got)
	}
}

// TestIsInsertKeyIgnoresAlt - Einfg (NUL-Rune ohne Alt) ist die Markier-
// Taste; die Alt-Taste allein (NUL-Rune mit Alt, z. B. beim Alt+Tab) nicht.
// Shift+Einfg ist Einfügen, nicht Markieren.
func TestIsInsertKeyIgnoresAlt(t *testing.T) {
	stubShift(t, false)
	if !isInsertKey(tea.KeyMsg{Type: tea.KeyInsert}) {
		t.Error("KeyInsert (Unix) muss als Einfg gelten")
	}
	if !isInsertKey(nulKey) {
		t.Error("NUL-Rune ohne Alt (Windows-Einfg) muss als Einfg gelten")
	}
	altNul := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}, Alt: true}
	if isInsertKey(altNul) {
		t.Error("NUL-Rune mit Alt (Alt-Taste) darf nicht als Einfg gelten")
	}
	if isInsertKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0, 0}}) {
		t.Error("mehrere NUL-Runen sind kein einzelner Tastendruck")
	}
	stubShift(t, true)
	if isInsertKey(nulKey) {
		t.Error("Shift+Einfg ist Einfügen, nicht Markieren")
	}
}

// TestIsPasteKeyIgnoresAlt - Alt+Shift (Layoutwechsel) mit gleichzeitig
// gehaltener Einfg-Taste kommt als NUL-Rune mit Alt: kein Einfügen.
func TestIsPasteKeyIgnoresAlt(t *testing.T) {
	stubShift(t, true)
	if isPasteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}, Alt: true}) {
		t.Error("NUL-Rune mit Alt darf nicht einfügen")
	}
}
