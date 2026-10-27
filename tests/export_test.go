// Paket main - Tests für den Export der Verbindungsliste (Build 51)
//
// Anforderung: Verbindungen lassen sich exportieren - alle oder eine
// Auswahl. SSH-Schlüssel sind nicht Teil des Exports (auch nicht ihr Pfad).
// Die Datei hat das Format von connections.json und kann damit auf einem
// anderen Rechner als Konfiguration verwendet werden.
//
// @author Kurt Ingwer
// @date   2026-10-17 23:10
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Fachlogik ---

// TestBuildExportConfigSelection prüft Auswahl und Reihenfolge.
func TestBuildExportConfigSelection(t *testing.T) {
	a := NewConnection("A", "a.lan", 22, "root", AuthPassword)
	a.ID = "1"
	b := NewConnection("B", "b.lan", 2222, "admin", AuthAgent)
	b.ID = "2"
	b.Tunnels = []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}
	c := NewConnection("C", "c.lan", 22, "x", AuthPassword)
	c.ID = "3"

	cfg := buildExportConfig([]Connection{a, b, c}, map[string]bool{"2": true, "3": true, "gibt-es-nicht": true})
	if cfg.Version != 1 || len(cfg.Connections) != 2 {
		t.Fatalf("Export = %+v", cfg)
	}
	if cfg.Connections[0].Name != "B" || cfg.Connections[1].Name != "C" {
		t.Errorf("Reihenfolge/Auswahl falsch: %s, %s", cfg.Connections[0].Name, cfg.Connections[1].Name)
	}
	if cfg.Connections[0].Port != 2222 || cfg.Connections[0].User != "admin" || len(cfg.Connections[0].Tunnels) != 1 {
		t.Errorf("Felder nicht übernommen: %+v", cfg.Connections[0])
	}
	// Keine programmweiten Einstellungen im Export
	if cfg.Language != "" || cfg.AutoUpdate || cfg.WindowCols != 0 || cfg.FontSize != 0 {
		t.Errorf("Programmeinstellungen im Export: %+v", cfg)
	}
}

// TestBuildExportConfigStripsKeys prüft dass keine Schlüssel-Information
// exportiert wird und die Verbindung trotzdem gültig bleibt.
func TestBuildExportConfigStripsKeys(t *testing.T) {
	conn := NewConnection("Server", "s.lan", 22, "root", AuthKey)
	conn.ID = "k"
	conn.KeyPath = "~/.ssh/id_ed25519_geheim"
	original := []Connection{conn}

	cfg := buildExportConfig(original, map[string]bool{"k": true})
	got := cfg.Connections[0]
	if got.KeyPath != "" {
		t.Errorf("Schlüsselpfad exportiert: %q", got.KeyPath)
	}
	if got.AuthType == AuthKey {
		t.Error("AuthType key ohne Schlüssel wäre ungültig")
	}
	if err := got.Validate(); err != nil {
		t.Errorf("exportierte Verbindung ungültig: %v", err)
	}
	if original[0].KeyPath == "" || original[0].AuthType != AuthKey {
		t.Error("Export hat die Original-Verbindung verändert")
	}
	data, _ := json.Marshal(cfg)
	if strings.Contains(string(data), "id_ed25519") || strings.Contains(string(data), "key_path") {
		t.Errorf("Schlüsselbezug im JSON: %s", data)
	}
}

// TestBuildExportConfigCopiesTunnels prüft dass Tunnel kopiert werden
// (keine geteilte Slice mit dem Modell).
func TestBuildExportConfigCopiesTunnels(t *testing.T) {
	conn := NewConnection("A", "a", 22, "u", AuthPassword)
	conn.ID = "1"
	conn.Tunnels = []TunnelConfig{{LocalPort: 1, RemotePort: 2, Enabled: true}}
	conns := []Connection{conn}
	cfg := buildExportConfig(conns, map[string]bool{"1": true})
	cfg.Connections[0].Tunnels[0].LocalPort = 999
	if conns[0].Tunnels[0].LocalPort != 1 {
		t.Error("Export teilt die Tunnel-Liste mit dem Original")
	}
}

// TestWriteExportFile prüft Schreiben, Format und Rechte.
func TestWriteExportFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	conn := NewConnection("A", "a.lan", 22, "root", AuthPassword)
	cfg := buildExportConfig([]Connection{conn}, map[string]bool{conn.ID: true})

	written, err := writeExportFile(path, cfg, dir)
	if err != nil {
		t.Fatalf("writeExportFile: %v", err)
	}
	if written != path {
		t.Errorf("Pfad = %q, erwartet %q", written, path)
	}
	// Datei ist als Konfiguration ladbar
	loaded, err := LoadConfig(path)
	if err != nil || len(loaded.Connections) != 1 || loaded.Connections[0].Host != "a.lan" {
		t.Fatalf("Export nicht als Konfiguration ladbar: %+v, %v", loaded, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("Rechte = %v, erwartet 0600 (Hosts/Benutzer sind vertraulich)", info.Mode().Perm())
		}
	}
}

// TestWriteExportFileNeverOverwrites prüft dass eine vorhandene Datei
// nicht überschrieben wird.
func TestWriteExportFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	_ = os.WriteFile(path, []byte("wichtig"), 0o600)
	_, err := writeExportFile(path, NewAppConfig(), dir)
	if !errors.Is(err, errExportExists) {
		t.Errorf("Fehler = %v, erwartet errExportExists", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "wichtig" {
		t.Errorf("vorhandene Datei überschrieben: %q", data)
	}
}

// TestWriteExportFileDoesNotFollowSymlink prüft dass ein Symlink am
// Zielpfad nicht verfolgt wird.
func TestWriteExportFileDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "opfer")
	_ = os.WriteFile(victim, []byte("unverändert"), 0o600)
	link := filepath.Join(dir, "export.json")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("Symlink nicht möglich: %v", err)
	}
	if _, err := writeExportFile(link, NewAppConfig(), dir); err == nil {
		t.Error("Symlink als Ziel akzeptiert")
	}
	if data, _ := os.ReadFile(victim); string(data) != "unverändert" {
		t.Errorf("Symlink-Ziel überschrieben: %q", data)
	}
}

// TestResolveExportPath prüft ~, relative Pfade und leere Eingaben.
func TestResolveExportPath(t *testing.T) {
	home := t.TempDir()
	cases := map[string]string{
		"~/x.json":                     filepath.Join(home, "x.json"),
		"x.json":                       filepath.Join(home, "x.json"),
		"  sub/x.json  ":               filepath.Join(home, "sub", "x.json"),
		filepath.Join(home, "a.json"):  filepath.Join(home, "a.json"),
		"~" + string(os.PathSeparator): home,
	}
	for in, want := range cases {
		got, err := resolveExportPath(in, home)
		if err != nil || got != want {
			t.Errorf("resolveExportPath(%q) = %q, %v; erwartet %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   "} {
		if _, err := resolveExportPath(in, home); err == nil {
			t.Errorf("leerer Pfad %q akzeptiert", in)
		}
	}
}

// TestWriteExportFileMissingDirectory prüft einen fehlenden Zielordner
// (wird bewusst nicht angelegt - Tippfehler sollen auffallen).
func TestWriteExportFileMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeExportFile(filepath.Join(dir, "fehlt", "x.json"), NewAppConfig(), dir); err == nil {
		t.Error("fehlender Ordner wurde nicht gemeldet")
	}
}

// TestDefaultExportFileName prüft den Vorschlag für den Dateinamen.
func TestDefaultExportFileName(t *testing.T) {
	name := defaultExportFileName()
	if !strings.HasPrefix(name, "ssh-easy-export-") || !strings.HasSuffix(name, ".json") || strings.ContainsAny(name, `:/\ `) {
		t.Errorf("Dateiname = %q", name)
	}
}

// --- Bedienung ---

// newExportTestModel öffnet die Exportansicht mit den Testverbindungen.
func newExportTestModel(t *testing.T, names ...string) AppModel {
	t.Helper()
	m := newListTestModel(t, names...)
	m.exportHomeDir = t.TempDir()
	m = press(t, m, "E")
	if m.state != ViewExport {
		t.Fatalf("E öffnet die Exportansicht nicht (state=%v)", m.state)
	}
	return m
}

// exportFileOf liest die exportierte Datei aus dem Home-Ordner des Tests.
func exportFileOf(t *testing.T, m AppModel) *AppConfig {
	t.Helper()
	entries, _ := os.ReadDir(m.exportHomeDir)
	if len(entries) != 1 {
		t.Fatalf("erwartet genau eine Exportdatei, gefunden: %v", entries)
	}
	cfg, err := LoadConfig(filepath.Join(m.exportHomeDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// exportedNames liefert die Namen der exportierten Verbindungen.
func exportedNames(cfg *AppConfig) string {
	var names []string
	for _, c := range cfg.Connections {
		names = append(names, c.Name)
	}
	return strings.Join(names, ",")
}

// TestExportAllByDefault prüft: E, Enter exportiert alle Verbindungen.
func TestExportAllByDefault(t *testing.T) {
	m := newExportTestModel(t, "A", "Büro:X", "Büro:Y", "C")
	text := stripANSI(m.View())
	for _, want := range []string{"Büro", "X", "Y", "4 / 4", "SSH"} {
		if !strings.Contains(text, want) {
			t.Errorf("Exportansicht enthält %q nicht:\n%s", want, text)
		}
	}
	m = press(t, m, "enter")
	if m.state != ViewList {
		t.Errorf("nach dem Export zurück zur Liste erwartet, state=%v", m.state)
	}
	if got := exportedNames(exportFileOf(t, m)); got != "A,Büro:X,Büro:Y,C" {
		t.Errorf("exportiert: %s", got)
	}
	if !strings.Contains(m.successMsg, "4") {
		t.Errorf("Erfolgsmeldung = %q", m.successMsg)
	}
}

// TestExportSelective prüft das Abwählen einzelner Einträge per Leertaste.
func TestExportSelective(t *testing.T) {
	m := newExportTestModel(t, "A", "B", "C")
	// Zeilen: A, B, C - B abwählen
	m = press(t, m, "down", " ", "enter")
	if got := exportedNames(exportFileOf(t, m)); got != "A,C" {
		t.Errorf("exportiert: %s, erwartet A,C", got)
	}
}

// TestExportGroupToggle prüft dass die Gruppenzeile alle Mitglieder umschaltet.
func TestExportGroupToggle(t *testing.T) {
	m := newExportTestModel(t, "A", "Büro:X", "Büro:Y")
	// Zeilen: A, Büro, X, Y - Gruppe abwählen, dann Y wieder an
	m = press(t, m, "down", " ")
	if text := stripANSI(m.View()); !strings.Contains(text, "1 / 3") {
		t.Errorf("nach Abwahl der Gruppe:\n%s", text)
	}
	m = press(t, m, "down", "down", " ", "enter")
	if got := exportedNames(exportFileOf(t, m)); got != "A,Büro:Y" {
		t.Errorf("exportiert: %s, erwartet A,Büro:Y", got)
	}
}

// TestExportGroupPartialShown prüft die Anzeige einer teilweise gewählten Gruppe.
func TestExportGroupPartialShown(t *testing.T) {
	m := newExportTestModel(t, "Büro:X", "Büro:Y")
	m = press(t, m, "down", " ") // X abwählen
	lines := strings.Split(stripANSI(m.View()), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Büro") && !strings.Contains(line, "[~]") {
			t.Errorf("teilweise gewählte Gruppe ohne [~]: %q", line)
		}
	}
	// Leertaste auf teilweise gewählter Gruppe: alle an
	m = press(t, m, "up", " ", "enter")
	if got := exportedNames(exportFileOf(t, m)); got != "Büro:X,Büro:Y" {
		t.Errorf("exportiert: %s", got)
	}
}

// TestExportToggleAll prüft 'a': alle ab- bzw. wieder anwählen.
func TestExportToggleAll(t *testing.T) {
	m := newExportTestModel(t, "A", "B")
	m = press(t, m, "a")
	if text := stripANSI(m.View()); !strings.Contains(text, "0 / 2") {
		t.Errorf("a wählt nicht alle ab:\n%s", text)
	}
	// Ohne Auswahl: Fehlermeldung, kein Export, Ansicht bleibt
	m = press(t, m, "enter")
	if m.state != ViewExport || m.errorMsg == "" {
		t.Errorf("leere Auswahl: state=%v err=%q", m.state, m.errorMsg)
	}
	if entries, _ := os.ReadDir(m.exportHomeDir); len(entries) != 0 {
		t.Errorf("ohne Auswahl wurde exportiert: %v", entries)
	}
	m = press(t, m, "a", "enter")
	if got := exportedNames(exportFileOf(t, m)); got != "A,B" {
		t.Errorf("exportiert: %s", got)
	}
}

// TestExportCustomFileName prüft Tab ins Dateinamenfeld und eigene Eingabe.
// Im Feld sind Leertaste und 'a' normale Zeichen.
func TestExportCustomFileName(t *testing.T) {
	m := newExportTestModel(t, "A", "B")
	m = press(t, m, "tab")
	m.exportPathInput.SetValue("")
	m = press(t, m, "m", "e", "i", "n", " ", "a", ".", "j", "s", "o", "n")
	if got := m.exportPathInput.Value(); got != "mein a.json" {
		t.Fatalf("Feldinhalt = %q", got)
	}
	m = press(t, m, "enter")
	cfg, err := LoadConfig(filepath.Join(m.exportHomeDir, "mein a.json"))
	if err != nil || len(cfg.Connections) != 2 {
		t.Errorf("eigener Dateiname: %+v, %v", cfg, err)
	}
}

// TestExportExistingFileError prüft die Meldung bei vorhandener Datei.
func TestExportExistingFileError(t *testing.T) {
	m := newExportTestModel(t, "A")
	path := filepath.Join(m.exportHomeDir, "da.json")
	_ = os.WriteFile(path, []byte("alt"), 0o600)
	m.exportPathInput.SetValue("da.json")
	m = press(t, m, "enter")
	if m.state != ViewExport || !strings.Contains(m.errorMsg, "da.json") {
		t.Errorf("vorhandene Datei: state=%v err=%q", m.state, m.errorMsg)
	}
	if data, _ := os.ReadFile(path); string(data) != "alt" {
		t.Error("vorhandene Datei überschrieben")
	}
}

// TestExportEscCancels prüft Esc: zurück zur Liste, nichts geschrieben.
func TestExportEscCancels(t *testing.T) {
	m := newExportTestModel(t, "A")
	m = press(t, m, "esc")
	if m.state != ViewList {
		t.Errorf("Esc: state=%v", m.state)
	}
	if entries, _ := os.ReadDir(m.exportHomeDir); len(entries) != 0 {
		t.Errorf("Esc hat exportiert: %v", entries)
	}
}

// TestExportEmptyList prüft E ohne Verbindungen: keine Exportansicht.
func TestExportEmptyList(t *testing.T) {
	m := newListTestModel(t)
	m = press(t, m, "E")
	if m.state != ViewList || m.errorMsg == "" {
		t.Errorf("leere Liste: state=%v err=%q", m.state, m.errorMsg)
	}
}

// TestExportScrollsLongList prüft das Scroll-Fenster bei vielen Einträgen.
func TestExportScrollsLongList(t *testing.T) {
	var names []string
	for i := 0; i < 30; i++ {
		names = append(names, "Server"+string(rune('A'+i/26))+string(rune('a'+i%26)))
	}
	m := newExportTestModel(t, names...)
	m = press(t, m, "end")
	text := stripANSI(m.View())
	if !strings.Contains(text, names[29]) || strings.Contains(text, names[0]) || !strings.Contains(text, "↑ +") {
		t.Errorf("Scroll-Fenster folgt dem Cursor nicht:\n%s", text)
	}
}

// TestListHelpMentionsExport prüft den Hinweis in der Listenhilfe.
func TestListHelpMentionsExport(t *testing.T) {
	m := newListTestModel(t, "A")
	// Tastenhilfe steht im Hauptmenü, das erst m einblendet (Build 84)
	m = press(t, m, "m")
	if !strings.Contains(stripANSI(renderListText(m)), "E:") {
		t.Error("Listenhilfe nennt die Export-Taste nicht")
	}
}
