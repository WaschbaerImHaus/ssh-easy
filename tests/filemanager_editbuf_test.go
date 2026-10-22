// Paket main - Tests für die Puffer des Editors (Text und Hex, Build 61)
//
// Prüft die reine Bearbeitungslogik ohne Oberfläche: Laden/Speichern ohne
// Verlust (CRLF, fehlender Zeilenumbruch am Ende, \r mitten im Text),
// Einfügen, Zeilen teilen/verbinden, Blöcke, Suchen/Ersetzen, Anzeige-
// spalten (Tabs, breite Zeichen) und das Überschreiben im Hex-Puffer.
//
// @author Kurt Ingwer
// @date   2026-10-22 01:30
package main

import (
	"strings"
	"testing"
)

// TestTextBufferRoundTrip - Laden und unverändert Speichern ergibt exakt
// dieselben Bytes.
func TestTextBufferRoundTrip(t *testing.T) {
	cases := []string{
		"", "a", "a\n", "a\nb", "a\nb\n", "\n\n", "a\r\nb\r\n", "a\r\nb",
		"gemischt\r\nunix\n", "cr\rmitten\n", "ümlaut 日本\ttab\n", "\r\n",
	}
	for _, in := range cases {
		buf := newTextBuffer([]byte(in))
		if got := string(buf.bytes()); got != in {
			t.Errorf("Rundreise %q -> %q", in, got)
		}
	}
}

// TestTextBufferDetectsCRLF - nur reine CRLF-Dateien gelten als CRLF.
func TestTextBufferDetectsCRLF(t *testing.T) {
	if !newTextBuffer([]byte("a\r\nb\r\n")).crlf {
		t.Error("CRLF nicht erkannt")
	}
	if newTextBuffer([]byte("a\r\nb\n")).crlf {
		t.Error("gemischte Datei als CRLF erkannt")
	}
	buf := newTextBuffer([]byte("a\r\nb\r\n"))
	buf.insert(textPos{1, 1}, []rune("\nneu"))
	if got := string(buf.bytes()); got != "a\r\nb\r\nneu\r\n" {
		t.Errorf("neue Zeile ohne CRLF: %q", got)
	}
}

// TestTextBufferInsertAndSplit prüft Einfügen inkl. Zeilenumbrüchen.
func TestTextBufferInsertAndSplit(t *testing.T) {
	buf := newTextBuffer([]byte("Hallo Welt"))
	pos := buf.insert(textPos{0, 5}, []rune(","))
	if string(buf.bytes()) != "Hallo, Welt" || pos != (textPos{0, 6}) {
		t.Errorf("Einfügen: %q %+v", buf.bytes(), pos)
	}
	pos = buf.insert(pos, []rune("\n"))
	if string(buf.bytes()) != "Hallo,\n Welt" || pos != (textPos{1, 0}) {
		t.Errorf("Teilen: %q %+v", buf.bytes(), pos)
	}
	pos = buf.insert(textPos{1, 5}, []rune("!\nzwei\ndrei"))
	if string(buf.bytes()) != "Hallo,\n Welt!\nzwei\ndrei" || pos != (textPos{3, 4}) {
		t.Errorf("mehrzeilig: %q %+v", buf.bytes(), pos)
	}
}

// TestTextBufferBackspaceDelete prüft Rücktaste und Entf (inkl. Verbinden).
func TestTextBufferBackspaceDelete(t *testing.T) {
	buf := newTextBuffer([]byte("ab\ncd"))
	pos := buf.backspace(textPos{1, 0})
	if string(buf.bytes()) != "abcd" || pos != (textPos{0, 2}) {
		t.Errorf("Rücktaste am Zeilenanfang: %q %+v", buf.bytes(), pos)
	}
	pos = buf.backspace(textPos{0, 0})
	if string(buf.bytes()) != "abcd" || pos != (textPos{0, 0}) {
		t.Errorf("Rücktaste am Anfang: %q %+v", buf.bytes(), pos)
	}
	buf = newTextBuffer([]byte("ab\ncd"))
	buf.deleteForward(textPos{0, 2})
	if string(buf.bytes()) != "abcd" {
		t.Errorf("Entf am Zeilenende: %q", buf.bytes())
	}
	buf.deleteForward(textPos{0, 4})
	if string(buf.bytes()) != "abcd" {
		t.Errorf("Entf am Ende: %q", buf.bytes())
	}
	buf.deleteForward(textPos{0, 1})
	if string(buf.bytes()) != "acd" {
		t.Errorf("Entf: %q", buf.bytes())
	}
}

// TestTextBufferDeleteLine prüft Strg+Y (Zeile löschen).
func TestTextBufferDeleteLine(t *testing.T) {
	buf := newTextBuffer([]byte("a\nb\nc\n"))
	buf.deleteLine(1)
	if string(buf.bytes()) != "a\nc\n" {
		t.Errorf("mittlere Zeile: %q", buf.bytes())
	}
	buf = newTextBuffer([]byte("nur"))
	buf.deleteLine(0)
	if string(buf.bytes()) != "" || len(buf.lines) != 1 {
		t.Errorf("letzte Zeile: %q %d", buf.bytes(), len(buf.lines))
	}
}

// TestTextBufferBlocks prüft Text, Löschen und Kopieren von Blöcken.
func TestTextBufferBlocks(t *testing.T) {
	buf := newTextBuffer([]byte("eins\nzwei\ndrei\n"))
	start, end := textPos{0, 2}, textPos{2, 1}
	if got := string(buf.textBetween(start, end)); got != "ns\nzwei\nd" {
		t.Errorf("Blocktext %q", got)
	}
	if got := string(buf.textBetween(end, start)); got != "ns\nzwei\nd" {
		t.Errorf("umgekehrte Reihenfolge %q", got)
	}
	buf.deleteBetween(start, end)
	if string(buf.bytes()) != "eirei\n" {
		t.Errorf("Löschen: %q", buf.bytes())
	}
	buf = newTextBuffer([]byte("ab"))
	if got := string(buf.textBetween(textPos{0, 1}, textPos{0, 1})); got != "" {
		t.Errorf("leerer Block %q", got)
	}
}

// TestTextBufferFind prüft die Suche ohne Groß-/Kleinschreibung.
func TestTextBufferFind(t *testing.T) {
	buf := newTextBuffer([]byte("Alpha beta\nGAMMA Beta\n"))
	pos, ok := buf.find("beta", textPos{0, 0})
	if !ok || pos != (textPos{0, 6}) {
		t.Errorf("erster Treffer %+v %v", pos, ok)
	}
	pos, ok = buf.find("beta", textPos{0, 7})
	if !ok || pos != (textPos{1, 6}) {
		t.Errorf("zweiter Treffer %+v %v", pos, ok)
	}
	if _, ok = buf.find("beta", textPos{1, 7}); ok {
		t.Error("Treffer nach dem letzten")
	}
	if _, ok = buf.find("", textPos{0, 0}); ok {
		t.Error("leerer Suchbegriff gefunden")
	}
	// Treffer mit Umlauten: Positionen in Zeichen, nicht Bytes
	buf = newTextBuffer([]byte("äöü Ziel"))
	if pos, ok = buf.find("ziel", textPos{0, 0}); !ok || pos != (textPos{0, 4}) {
		t.Errorf("Umlaute: %+v %v", pos, ok)
	}
}

// TestTextBufferReplaceAt ersetzt einen Treffer.
func TestTextBufferReplaceAt(t *testing.T) {
	buf := newTextBuffer([]byte("foo bar foo"))
	next := buf.replaceAt(textPos{0, 8}, len([]rune("foo")), []rune("baz"))
	if string(buf.bytes()) != "foo bar baz" || next != (textPos{0, 11}) {
		t.Errorf("Ersetzen: %q %+v", buf.bytes(), next)
	}
}

// TestDisplayColumn prüft die Anzeigespalte (Tabs, breite Zeichen).
func TestDisplayColumn(t *testing.T) {
	line := []rune("a\tb日c")
	cases := map[int]int{0: 0, 1: 1, 2: 8, 3: 9, 4: 11, 5: 12}
	for col, want := range cases {
		if got := displayColumn(line, col); got != want {
			t.Errorf("displayColumn(%d) = %d, erwartet %d", col, got, want)
		}
	}
	if got := renderEditLine(line); got != "a       b日c" {
		t.Errorf("renderEditLine = %q", got)
	}
	if got := renderEditLine([]rune("x\x1by\r")); got != "x?y?" {
		t.Errorf("Steuerzeichen: %q", got)
	}
}

// TestHexBufferOverwrite prüft das Überschreiben per Hex-Ziffern.
func TestHexBufferOverwrite(t *testing.T) {
	h := newHexBuffer([]byte{0xAD, 0x00, 0x7f})
	h.typeHex('a') // oberes Halbbyte von 0xAD -> 0xAD bleibt (a)
	h.typeHex('f') // unteres Halbbyte -> 0xAF, Cursor weiter
	if h.data[0] != 0xAF || h.cursor != 1 || h.low {
		t.Errorf("AD -> AF: % x cursor %d low %v", h.data, h.cursor, h.low)
	}
	h.typeHex('8')
	h.typeHex('0')
	if h.data[1] != 0x80 || h.cursor != 2 {
		t.Errorf("00 -> 80: % x", h.data)
	}
	if !h.changed[0] || !h.changed[1] || h.changed[2] {
		t.Errorf("Änderungen %v", h.changed)
	}
	// Ungültige Zeichen ändern nichts
	if h.typeHex('x') || h.data[2] != 0x7f {
		t.Error("ungültige Hex-Ziffer angenommen")
	}
	// Am Ende bleibt der Cursor auf dem letzten Byte
	h.typeHex('1')
	h.typeHex('2')
	if h.data[2] != 0x12 || h.cursor != 2 {
		t.Errorf("letztes Byte: % x cursor %d", h.data, h.cursor)
	}
	if len(h.data) != 3 {
		t.Error("Größe verändert")
	}
}

// TestHexBufferASCII prüft das Überschreiben in der Zeichenspalte.
func TestHexBufferASCII(t *testing.T) {
	h := newHexBuffer([]byte("abc"))
	if !h.typeASCII('X') || string(h.data) != "Xbc" || h.cursor != 1 {
		t.Errorf("ASCII: %q cursor %d", h.data, h.cursor)
	}
	if h.typeASCII('ä') || h.typeASCII('\x01') {
		t.Error("nicht druckbares Zeichen angenommen")
	}
}

// TestHexBufferMove prüft Cursorbewegung und Grenzen.
func TestHexBufferMove(t *testing.T) {
	h := newHexBuffer(make([]byte, 40))
	h.move(5, 16)
	h.move(16, 16)
	if h.cursor != 21 {
		t.Errorf("cursor %d", h.cursor)
	}
	h.move(-100, 16)
	if h.cursor != 0 {
		t.Errorf("Anfang: %d", h.cursor)
	}
	h.move(1000, 16)
	if h.cursor != 39 {
		t.Errorf("Ende: %d", h.cursor)
	}
	empty := newHexBuffer(nil)
	empty.move(1, 16)
	if empty.typeHex('a') || empty.cursor != 0 {
		t.Error("leerer Puffer verändert")
	}
}

// TestParseHexQuery - Hex-Suche: "ad ff" als Bytes, sonst Text.
func TestParseHexQuery(t *testing.T) {
	if got := parseHexQuery("ad ff"); string(got) != "\xad\xff" {
		t.Errorf("Hex: % x", got)
	}
	if got := parseHexQuery("ADFF"); string(got) != "\xad\xff" {
		t.Errorf("Hex ohne Leerzeichen: % x", got)
	}
	if got := parseHexQuery("hallo"); string(got) != "hallo" {
		t.Errorf("Text: %q", got)
	}
	if got := parseHexQuery("abc"); string(got) != "abc" {
		t.Errorf("ungerade Länge als Text: %q", got)
	}
	_ = strings.ToLower
}
