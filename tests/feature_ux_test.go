// Paket main - Tests fuer die UX-Features aus Build 42
//
// Prueft die fuenf Features:
//   - Clipboard-Paste in TUI-Formularfeldern (Strg+V / Shift+Einf)
//   - Alt+F4-Erkennung im stdin-Forwarder (Programm beenden wie PuTTY)
//   - Schriftgroessen-Logik (Clamping, i18n-Fallback, Config-Persistierung)
//   - AppConfig.FontSize JSON-Roundtrip
// Fenster-Fokus (SetForegroundWindow) und Scroll-Snap (SetConsoleWindowInfo)
// sind reine Windows-API-Aufrufe ohne testbare Logik auf Linux.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:50
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Feature: Clipboard-Paste in Formularen ---

// TestIsPasteShortcut prueft die Erkennung der Einfuege-Kuerzel.
func TestIsPasteShortcut(t *testing.T) {
	positive := []string{"ctrl+v", "shift+insert"}
	for _, key := range positive {
		if !isPasteShortcut(key) {
			t.Errorf("isPasteShortcut(%q) = false, erwartet true", key)
		}
	}

	negative := []string{"v", "ctrl+c", "insert", "alt+v", "enter", ""}
	for _, key := range negative {
		if isPasteShortcut(key) {
			t.Errorf("isPasteShortcut(%q) = true, erwartet false", key)
		}
	}
}

// TestReadClipboardLineFirstLineOnly prueft dass nur die erste Zeile
// uebernommen wird (Formularfelder sind einzeilig).
func TestReadClipboardLineFirstLineOnly(t *testing.T) {
	restore := stubClipboard("zeile1\nzeile2\r\nzeile3", nil)
	defer restore()

	got, ok := readClipboardLine()
	if !ok {
		t.Fatal("readClipboardLine sollte true liefern")
	}
	if got != "zeile1" {
		t.Errorf("readClipboardLine = %q, erwartet %q", got, "zeile1")
	}
}

// TestReadClipboardLinePreservesSpaces prueft dass Leerzeichen erhalten
// bleiben (koennten Teil eines Passworts sein).
func TestReadClipboardLinePreservesSpaces(t *testing.T) {
	restore := stubClipboard("  geheim mit leerzeichen  ", nil)
	defer restore()

	got, ok := readClipboardLine()
	if !ok || got != "  geheim mit leerzeichen  " {
		t.Errorf("readClipboardLine = %q/%v, Leerzeichen muessen erhalten bleiben", got, ok)
	}
}

// TestReadClipboardLineEmptyAndError prueft leeres Clipboard und Lesefehler.
func TestReadClipboardLineEmptyAndError(t *testing.T) {
	restore := stubClipboard("", nil)
	if _, ok := readClipboardLine(); ok {
		t.Error("Leeres Clipboard sollte ok=false liefern")
	}
	restore()

	restore = stubClipboard("text", errors.New("kein Clipboard"))
	defer restore()
	if _, ok := readClipboardLine(); ok {
		t.Error("Clipboard-Fehler sollte ok=false liefern")
	}
}

// TestMakePasteKeyMsg prueft die KeyMsg-Konstruktion fuer textinput.
func TestMakePasteKeyMsg(t *testing.T) {
	msg := makePasteKeyMsg("abc")
	if msg.Type != tea.KeyRunes {
		t.Errorf("KeyMsg.Type = %v, erwartet KeyRunes", msg.Type)
	}
	if string(msg.Runes) != "abc" {
		t.Errorf("KeyMsg.Runes = %q, erwartet %q", string(msg.Runes), "abc")
	}
	if !msg.Paste {
		t.Error("KeyMsg.Paste sollte true sein")
	}
}

// TestFormPasteInsertsClipboard prueft den kompletten Weg: Strg+V im
// Verbindungsformular fuegt den Clipboard-Inhalt ins fokussierte Feld ein.
func TestFormPasteInsertsClipboard(t *testing.T) {
	restore := stubClipboard("mein-server", nil)
	defer restore()

	configPath := filepath.Join(t.TempDir(), "connections.json")
	m := NewAppModel(configPath, "test", NewSSHManager(NewLogger()))
	m.state = ViewCreate
	m.focusedInput = fieldName
	m.inputs[fieldName].Focus()

	updated, _ := m.handleFormKeys(tea.KeyMsg{Type: tea.KeyCtrlV})
	um := updated.(AppModel)

	if got := um.inputs[fieldName].Value(); got != "mein-server" {
		t.Errorf("Feldwert nach Paste = %q, erwartet %q", got, "mein-server")
	}
}

// TestPasswordPasteInsertsClipboard prueft Einfuegen in der Passwort-Eingabe.
func TestPasswordPasteInsertsClipboard(t *testing.T) {
	restore := stubClipboard("s3cr3t!", nil)
	defer restore()

	configPath := filepath.Join(t.TempDir(), "connections.json")
	m := NewAppModel(configPath, "test", NewSSHManager(NewLogger()))
	m.state = ViewConnect
	m.passwordInput.Focus()

	updated, _ := m.handleConnectKeys(tea.KeyMsg{Type: tea.KeyCtrlV})
	um := updated.(AppModel)

	if got := um.passwordInput.Value(); got != "s3cr3t!" {
		t.Errorf("Passwortfeld nach Paste = %q, erwartet %q", got, "s3cr3t!")
	}
}

// TestFormPasteMultilineTakesFirstLine prueft dass mehrzeiliger
// Clipboard-Inhalt im Formular nicht zum Absenden fuehrt.
func TestFormPasteMultilineTakesFirstLine(t *testing.T) {
	restore := stubClipboard("host1\nhost2", nil)
	defer restore()

	configPath := filepath.Join(t.TempDir(), "connections.json")
	m := NewAppModel(configPath, "test", NewSSHManager(NewLogger()))
	m.state = ViewCreate
	m.focusedInput = fieldHost
	m.inputs[fieldHost].Focus()

	updated, _ := m.handleFormKeys(tea.KeyMsg{Type: tea.KeyCtrlV})
	um := updated.(AppModel)

	if got := um.inputs[fieldHost].Value(); got != "host1" {
		t.Errorf("Feldwert = %q, erwartet nur erste Zeile %q", got, "host1")
	}
}

// --- Feature: Alt+F4 ---

// TestAltF4SequenceBytes dokumentiert die erwartete VT-Sequenz fuer Alt+F4
// (CSI 1;3S) als Regression-Schutz.
func TestAltF4SequenceBytes(t *testing.T) {
	expected := []byte{0x1b, '[', '1', ';', '3', 'S'}
	if !bytes.Equal(altF4Seq, expected) {
		t.Errorf("Alt+F4-Sequenz ist falsch: erwartet %q, erhalten %q", expected, altF4Seq)
	}
}

// TestForwardStdinAltF4Callback prueft dass Alt+F4 den Callback ausloest,
// die Bytes davor noch zugestellt werden und die Sequenz selbst sowie alles
// danach NICHT mehr weitergeleitet wird.
func TestForwardStdinAltF4Callback(t *testing.T) {
	restore := stubClipboard("nicht-verwendet", nil)
	defer restore()

	input := append([]byte("vorher"), altF4Seq...)
	input = append(input, []byte("nachher")...)

	r := bytes.NewReader(input)
	var w bytes.Buffer
	called := false

	forwardStdinWithPaste(r, &w, func() { called = true })

	if !called {
		t.Error("Alt+F4-Callback wurde nicht aufgerufen")
	}
	if w.String() != "vorher" {
		t.Errorf("Weitergeleitete Bytes = %q, erwartet nur %q", w.String(), "vorher")
	}
	if strings.Contains(w.String(), "nachher") {
		t.Error("Bytes nach Alt+F4 duerfen nicht mehr weitergeleitet werden")
	}
}

// TestForwardStdinAltF4NilCallback prueft dass ein nil-Callback nicht panikt
// (Alt+F4 wird dann nur verschluckt und die Weiterleitung beendet).
func TestForwardStdinAltF4NilCallback(t *testing.T) {
	restore := stubClipboard("x", nil)
	defer restore()

	r := bytes.NewReader(altF4Seq)
	var w bytes.Buffer

	// Darf nicht paniken
	forwardStdinWithPaste(r, &w, nil)

	if w.Len() != 0 {
		t.Errorf("Alt+F4-Sequenz wurde weitergeleitet: %q", w.String())
	}
}

// TestForwardStdinPasteBeforeAltF4 prueft die Reihenfolge wenn beide
// Sequenzen im selben Buffer stehen: erst Paste ausfuehren, dann beenden.
func TestForwardStdinPasteBeforeAltF4(t *testing.T) {
	restore := stubClipboard("PASTE", nil)
	defer restore()

	var input []byte
	input = append(input, 'a')
	input = append(input, shiftInsertSeq...)
	input = append(input, 'b')
	input = append(input, altF4Seq...)
	input = append(input, 'c')

	r := bytes.NewReader(input)
	var w bytes.Buffer
	called := false

	forwardStdinWithPaste(r, &w, func() { called = true })

	if !called {
		t.Error("Alt+F4-Callback wurde nicht aufgerufen")
	}
	expected := "aPASTEb"
	if w.String() != expected {
		t.Errorf("Weitergeleitete Bytes = %q, erwartet %q", w.String(), expected)
	}
}

// --- Feature: Schriftgroesse ---

// TestClampFontSize prueft die Begrenzung auf den erlaubten Bereich.
func TestClampFontSize(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{in: 0, want: MinFontSize},
		{in: -5, want: MinFontSize},
		{in: MinFontSize, want: MinFontSize},
		{in: 16, want: 16},
		{in: MaxFontSize, want: MaxFontSize},
		{in: 200, want: MaxFontSize},
	}
	for _, c := range cases {
		if got := clampFontSize(c.in); got != c.want {
			t.Errorf("clampFontSize(%d) = %d, erwartet %d", c.in, got, c.want)
		}
	}
}

// TestFontSizeTextFallback prueft den Englisch-Fallback fuer Sprachen
// ohne gepflegte FontSize-Strings.
func TestFontSizeTextFallback(t *testing.T) {
	var empty Translations
	if got := empty.fontSizeChangedText(); !strings.Contains(got, "%d") {
		t.Errorf("Fallback fontSizeChangedText = %q, erwartet Format-String mit %%d", got)
	}
	if got := empty.fontSizeUnsupportedText(); got == "" {
		t.Error("Fallback fontSizeUnsupportedText darf nicht leer sein")
	}

	de := GetTranslations(LangDeutsch)
	if got := de.fontSizeChangedText(); !strings.HasPrefix(got, "Schriftgröße") {
		t.Errorf("Deutsche Uebersetzung fehlt: %q", got)
	}
}

// TestAppConfigFontSizeRoundtrip prueft Speichern und Laden von font_size.
func TestAppConfigFontSizeRoundtrip(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "connections.json")

	cfg := NewAppConfig()
	cfg.FontSize = 20
	if err := SaveConfig(configPath, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.FontSize != 20 {
		t.Errorf("FontSize nach Roundtrip = %d, erwartet 20", loaded.FontSize)
	}
}

// TestAppConfigFontSizeOmitempty prueft dass font_size bei 0 nicht in der
// JSON-Datei auftaucht (Systemstandard bleibt implizit).
func TestAppConfigFontSizeOmitempty(t *testing.T) {
	cfg := NewAppConfig()
	data, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "font_size") {
		t.Errorf("font_size sollte bei 0 weggelassen werden: %s", data)
	}
}

// TestChangeFontSizeUnsupportedPlatform prueft dass auf Plattformen ohne
// Font-Steuerung (Linux-Testumgebung) ein Hinweis erscheint statt eines Crashs.
func TestChangeFontSizeUnsupportedPlatform(t *testing.T) {
	if getConsoleFontSize() > 0 {
		t.Skip("Testumgebung kann Schriftgroesse steuern - Test nur fuer No-Op-Plattformen")
	}

	configPath := filepath.Join(t.TempDir(), "connections.json")
	m := NewAppModel(configPath, "test", NewSSHManager(NewLogger()))

	updated, _ := m.changeFontSize(+FontSizeStep)
	um := updated.(AppModel)

	if um.errorMsg == "" {
		t.Error("changeFontSize sollte auf Linux einen Hinweis in errorMsg setzen")
	}
}

// TestApplyStartupFontSizeZeroIsNoop prueft dass 0 (Systemstandard) keine
// Aktion ausloest - darf insbesondere nicht auf MinFontSize clampen.
func TestApplyStartupFontSizeZeroIsNoop(t *testing.T) {
	// Auf Linux ist setConsoleFontSize ohnehin ein No-Op; der Test stellt
	// sicher dass der Aufruf mit 0 und negativen Werten nicht panikt.
	applyStartupFontSize(0)
	applyStartupFontSize(-3)
}

// TestFontSizeCtrlArrowBindings prueft dass Strg+Pfeil-hoch/-runter die
// Schriftgroessen-Aenderung ausloesen. Hintergrund (Build 42-Bug): Bubbletea
// v1.x verwirft im Windows-coninput-Pfad den Ctrl-Modifier bei Zeichentasten
// (Strg+Plus kommt als Rune 0x00 an) - VK_UP/VK_DOWN mit Ctrl sind dagegen
// explizit als KeyCtrlUp/KeyCtrlDown gemappt und kommen zuverlaessig an.
// Beobachtbar auf Linux: changeFontSize setzt den Unsupported-Hinweis.
func TestFontSizeCtrlArrowBindings(t *testing.T) {
	if getConsoleFontSize() > 0 {
		t.Skip("Testumgebung kann Schriftgroesse steuern - Test nur fuer No-Op-Plattformen")
	}

	configPath := filepath.Join(t.TempDir(), "connections.json")

	for _, keyType := range []tea.KeyType{tea.KeyCtrlUp, tea.KeyCtrlDown} {
		m := NewAppModel(configPath, "test", NewSSHManager(NewLogger()))
		m.state = ViewList

		updated, _ := m.handleKeyPress(tea.KeyMsg{Type: keyType})
		um := updated.(AppModel)

		if um.errorMsg == "" {
			t.Errorf("Taste %v sollte changeFontSize ausloesen (errorMsg erwartet)", keyType)
		}
	}
}

// TestPersistFontSize prueft die Persistierung der Schriftgroesse beim
// Beenden (uebernimmt auch per Strg+Mausrad-Zoom geaenderte Groessen).
func TestPersistFontSize(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "connections.json")

	// Groesse 0 (nicht ermittelbar, z.B. Linux): nichts speichern
	if persistFontSize(configPath, 0) {
		t.Error("persistFontSize(0) sollte nichts speichern")
	}

	// Neue Groesse: speichern
	if !persistFontSize(configPath, 18) {
		t.Error("persistFontSize(18) sollte speichern")
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.FontSize != 18 {
		t.Errorf("FontSize = %d, erwartet 18", cfg.FontSize)
	}

	// Unveraenderte Groesse: kein erneutes Speichern noetig
	if persistFontSize(configPath, 18) {
		t.Error("persistFontSize mit unveraendertem Wert sollte nicht erneut speichern")
	}
}
