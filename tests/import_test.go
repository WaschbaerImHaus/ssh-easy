// Paket main - Tests für den Import der Verbindungsliste (Build 52)
//
// Gegenstück zum Export: Eine Exportdatei (Format connections.json) wird
// gelesen, die Verbindungen werden zur Auswahl angezeigt und übernommen.
// Die Datei gilt als nicht vertrauenswürdig: Größenlimit, Prüfung jedes
// Eintrags, keine Steuerzeichen, Schlüsselpfade werden nie übernommen.
// Vorhandene Namen werden erkannt und nur auf ausdrücklichen Wunsch ersetzt.
//
// @author Kurt Ingwer
// @date   2026-10-17 23:40
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeImportFixture schreibt eine Importdatei mit den Verbindungen.
func writeImportFixture(t *testing.T, dir, name string, conns ...Connection) string {
	t.Helper()
	cfg := NewAppConfig()
	cfg.Connections = conns
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// importConn erzeugt eine Verbindung für Importdateien.
func importConn(name, host string) Connection {
	c := NewConnection(name, host, 22, "root", AuthPassword)
	c.ID = "file-" + name
	return c
}

// --- Fachlogik: Datei lesen ---

// TestReadImportFileValid prüft das Lesen einer gültigen Datei.
func TestReadImportFileValid(t *testing.T) {
	dir := t.TempDir()
	b := importConn("B", "b.lan")
	b.Tunnels = []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}
	path := writeImportFixture(t, dir, "x.json", importConn("A", "a.lan"), b)

	conns, skipped, err := readImportFile(path)
	if err != nil || skipped != 0 || len(conns) != 2 {
		t.Fatalf("conns=%d skipped=%d err=%v", len(conns), skipped, err)
	}
	if conns[1].Host != "b.lan" || len(conns[1].Tunnels) != 1 {
		t.Errorf("Felder nicht übernommen: %+v", conns[1])
	}
	// Neue, eindeutige IDs (IDs aus der Datei sind nicht vertrauenswürdig)
	if conns[0].ID == "file-A" || conns[0].ID == conns[1].ID || conns[0].ID == "" {
		t.Errorf("IDs nicht neu vergeben: %q, %q", conns[0].ID, conns[1].ID)
	}
}

// TestReadImportFileReadsOwnExport prüft den Kreislauf Export -> Import.
func TestReadImportFileReadsOwnExport(t *testing.T) {
	dir := t.TempDir()
	a := importConn("Büro:NAS", "nas.lan")
	cfg := buildExportConfig([]Connection{a}, map[string]bool{a.ID: true})
	path, err := writeExportFile("e.json", cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	conns, _, err := readImportFile(path)
	if err != nil || len(conns) != 1 || conns[0].Name != "Büro:NAS" {
		t.Errorf("eigener Export nicht importierbar: %+v, %v", conns, err)
	}
}

// TestReadImportFileStripsKeyPath prüft dass Schlüsselpfade nie übernommen
// werden (eine präparierte Datei könnte sonst beliebige Dateien als
// Schlüssel lesen lassen).
func TestReadImportFileStripsKeyPath(t *testing.T) {
	dir := t.TempDir()
	c := importConn("K", "k.lan")
	c.AuthType = AuthKey
	c.KeyPath = "/etc/shadow"
	path := writeImportFixture(t, dir, "x.json", c)
	conns, skipped, err := readImportFile(path)
	if err != nil || skipped != 0 || len(conns) != 1 {
		t.Fatalf("conns=%v skipped=%d err=%v", conns, skipped, err)
	}
	if conns[0].KeyPath != "" || conns[0].AuthType == AuthKey {
		t.Errorf("Schlüssel übernommen: %+v", conns[0])
	}
}

// TestReadImportFileSkipsInvalidEntries prüft dass ungültige Einträge
// übersprungen (und gezählt) werden, gültige aber bleiben.
func TestReadImportFileSkipsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	bad := []Connection{
		importConn("", "leer.lan"),                // kein Name
		importConn("OhneHost", ""),                // kein Host
		importConn("Esc\x1b[2J", "a.lan"),         // Steuerzeichen (Terminal-Escape)
		importConn("Host", "a.lan\x1b]0;x\x07"),   // Steuerzeichen im Host
		importConn(":Gruppe", "a.lan"),            // ungültiger Gruppenname
		importConn("Bidi\u202eexe.txt", "a.lan"),       // Bidi-Steuerzeichen
		importConn(strings.Repeat("n", 300), "a"), // zu lang
		importConn("Gut", "gut.lan"),              // gültig
		importConn("gut", "doppelt.lan"),          // Dublette in der Datei (Groß/klein egal)
	}
	badPort := importConn("Port", "p.lan")
	badPort.Port = 70000
	badUser := importConn("User", "u.lan")
	badUser.User = "root\nrm -rf /"
	badTunnel := importConn("Tunnel", "t.lan")
	badTunnel.Tunnels = []TunnelConfig{{LocalPort: 0, RemotePort: 80, Enabled: true}}
	badAuth := importConn("Auth", "x.lan")
	badAuth.AuthType = "telnet"
	bad = append(bad, badPort, badUser, badTunnel, badAuth)

	path := writeImportFixture(t, dir, "x.json", bad...)
	conns, skipped, err := readImportFile(path)
	if err != nil {
		t.Fatalf("readImportFile: %v", err)
	}
	if len(conns) != 1 || conns[0].Name != "Gut" {
		t.Errorf("gültige Einträge = %+v, erwartet nur 'Gut'", conns)
	}
	if skipped != len(bad)-1 {
		t.Errorf("übersprungen = %d, erwartet %d", skipped, len(bad)-1)
	}
}

// TestReadImportFileAllowsZWNJInName prüft dass ZWNJ (Persisch u.a.) im
// Namen erlaubt bleibt - nur echte Steuerzeichen werden abgelehnt.
func TestReadImportFileAllowsZWNJInName(t *testing.T) {
	dir := t.TempDir()
	path := writeImportFixture(t, dir, "x.json", importConn("سرور\u200cها", "fa.lan"))
	if conns, _, err := readImportFile(path); err != nil || len(conns) != 1 {
		t.Errorf("Name mit ZWNJ abgelehnt: %v, %v", conns, err)
	}
}

// TestReadImportFileErrors prüft die Fehlerfälle der Datei selbst.
func TestReadImportFileErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(content), 0o600)
		return p
	}
	if _, _, err := readImportFile(filepath.Join(dir, "fehlt.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("fehlende Datei: %v", err)
	}
	for name, content := range map[string]string{
		"kaputt.json": "{nicht json",
		"array.json":  `[{"name":"A"}]`,
		"leer.json":   "",
	} {
		if _, _, err := readImportFile(write(name, content)); !errors.Is(err, errImportFormat) {
			t.Errorf("%s: Fehler %v, erwartet errImportFormat", name, err)
		}
	}
	if _, _, err := readImportFile(write("ohne.json", `{"version":1,"connections":[]}`)); !errors.Is(err, errImportEmpty) {
		t.Errorf("ohne Verbindungen: %v, erwartet errImportEmpty", err)
	}
	if _, _, err := readImportFile(write("ungueltig.json", `{"version":1,"connections":[{"name":"","host":""}]}`)); !errors.Is(err, errImportEmpty) {
		t.Errorf("nur ungültige: %v, erwartet errImportEmpty", err)
	}
	big := write("gross.json", `{"version":1,"connections":[`+strings.Repeat(" ", maxImportBytes)+`]}`)
	if _, _, err := readImportFile(big); !errors.Is(err, errImportTooLarge) {
		t.Errorf("zu große Datei: %v, erwartet errImportTooLarge", err)
	}
	if _, _, err := readImportFile(dir); err == nil {
		t.Error("Ordner als Datei akzeptiert")
	}
}

// --- Fachlogik: Zusammenführen ---

// TestMergeImportAddsAndReplaces prüft Hinzufügen, Ersetzen und Auslassen.
func TestMergeImportAddsAndReplaces(t *testing.T) {
	local := NewConnection("Server", "alt.lan", 22, "alt", AuthKey)
	local.ID = "local-1"
	local.KeyPath = "~/.ssh/id_server"
	local.CreatedAt = "2020-01-01T00:00:00Z"
	other := NewConnection("Bleibt", "b.lan", 22, "u", AuthPassword)
	other.ID = "local-2"

	replace := NewConnection("server", "neu.lan", 2200, "neu", AuthPassword) // gleicher Name, andere Schreibweise
	replace.ID = "imp-1"
	replace.Tunnels = []TunnelConfig{{LocalPort: 9000, RemotePort: 9000, Enabled: true}}
	add := NewConnection("Neu", "n.lan", 22, "n", AuthAgent)
	add.ID = "imp-2"
	skip := NewConnection("Nicht", "x.lan", 22, "x", AuthPassword)
	skip.ID = "imp-3"

	merged, res := mergeImport([]Connection{local, other}, []Connection{replace, add, skip},
		map[string]bool{"imp-1": true, "imp-2": true})
	if res.added != 1 || res.replaced != 1 || len(merged) != 3 {
		t.Fatalf("res=%+v, %d Verbindungen", res, len(merged))
	}
	byName := map[string]Connection{}
	for _, c := range merged {
		byName[strings.ToLower(c.Name)] = c
	}
	got := byName["server"]
	if got.ID != "local-1" || got.Host != "neu.lan" || got.Port != 2200 || got.User != "neu" || len(got.Tunnels) != 1 {
		t.Errorf("ersetzte Verbindung = %+v", got)
	}
	// Lokale Schlüsseleinstellung und Erstellzeit bleiben erhalten
	if got.KeyPath != "~/.ssh/id_server" || got.AuthType != AuthKey || got.CreatedAt != "2020-01-01T00:00:00Z" {
		t.Errorf("lokaler Schlüssel/Erstellzeit verloren: %+v", got)
	}
	if byName["neu"].ID != "imp-2" || byName["bleibt"].Host != "b.lan" {
		t.Errorf("hinzugefügt/unberührt falsch: %+v", merged)
	}
	if _, ok := byName["nicht"]; ok {
		t.Error("nicht gewählte Verbindung wurde importiert")
	}
	if res.firstID != "local-1" && res.firstID != "imp-2" {
		t.Errorf("firstID = %q", res.firstID)
	}
}

// TestExistingNames prüft die Dublettenerkennung (Groß-/Kleinschreibung egal).
func TestExistingNames(t *testing.T) {
	names := existingNames([]Connection{importConn("Büro:NAS", "a")})
	if !names["büro:nas"] || names["nas"] {
		t.Errorf("existingNames = %v", names)
	}
}

// TestApplyImportPersists prüft dass der Import gespeichert wird und
// vorhandene Einstellungen der Konfiguration erhalten bleiben.
func TestApplyImportPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	cfg := NewAppConfig()
	cfg.Language = LangDeutsch
	cfg.AutoUpdate = true
	cfg.Connections = []Connection{importConn("Alt", "alt.lan")}
	_ = SaveConfig(path, &cfg)

	n := importConn("Neu", "neu.lan")
	n.ID = "neu-id"
	res, err := applyImport(path, []Connection{n}, map[string]bool{"neu-id": true})
	if err != nil || res.added != 1 {
		t.Fatalf("applyImport: %+v, %v", res, err)
	}
	loaded, _ := LoadConfig(path)
	if len(loaded.Connections) != 2 || loaded.Language != LangDeutsch || !loaded.AutoUpdate {
		t.Errorf("Konfiguration nach Import: %+v", loaded)
	}
}

// TestLatestExportFile prüft den Dateivorschlag (neueste Exportdatei).
func TestLatestExportFile(t *testing.T) {
	dir := t.TempDir()
	if got := latestExportFile(dir); got != "" {
		t.Errorf("leerer Ordner: %q", got)
	}
	old := filepath.Join(dir, "ssh-easy-export-20260101-0000.json")
	newer := filepath.Join(dir, "ssh-easy-export-20261017-2300.json")
	_ = os.WriteFile(old, []byte("{}"), 0o600)
	_ = os.WriteFile(newer, []byte("{}"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "anderes.json"), []byte("{}"), 0o600)
	_ = os.Chtimes(old, time.Now(), time.Now().Add(-time.Hour))
	_ = os.Chtimes(newer, time.Now(), time.Now())
	if got := latestExportFile(dir); got != filepath.Base(newer) {
		t.Errorf("Vorschlag = %q, erwartet %q", got, filepath.Base(newer))
	}
}

// --- Bedienung ---

// newImportTestModel öffnet die Importansicht.
func newImportTestModel(t *testing.T, names ...string) AppModel {
	t.Helper()
	m := newListTestModel(t, names...)
	m.exportHomeDir = t.TempDir()
	return m
}

// TestImportFlow prüft den Ablauf: I, Datei laden, auswählen, Enter.
func TestImportFlow(t *testing.T) {
	m := newImportTestModel(t, "Server")
	writeImportFixture(t, m.exportHomeDir, "ssh-easy-export-20261017-2300.json",
		importConn("Server", "neu.lan"), importConn("Büro:NAS", "nas.lan"), importConn("Web", "web.lan"))

	m = press(t, m, "I")
	if m.state != ViewImportFile {
		t.Fatalf("I öffnet die Importansicht nicht (state=%v)", m.state)
	}
	if got := m.importPathInput.Value(); got != "ssh-easy-export-20261017-2300.json" {
		t.Errorf("Dateivorschlag = %q", got)
	}
	m = press(t, m, "enter")
	if m.state != ViewImportSelect {
		t.Fatalf("Datei nicht geladen: state=%v err=%q", m.state, m.errorMsg)
	}
	text := stripANSI(m.View())
	// Vorhandener Name: markiert und zunächst nicht gewählt
	if !strings.Contains(text, "2 / 3") || !strings.Contains(text, "(vorhanden)") {
		t.Errorf("Auswahlansicht:\n%s", text)
	}

	m = press(t, m, "enter")
	if m.state != ViewList {
		t.Fatalf("nach Import: state=%v err=%q", m.state, m.errorMsg)
	}
	names := []string{}
	for _, c := range m.connections {
		names = append(names, c.Name+"@"+c.Host)
	}
	if got := strings.Join(names, ","); got != "Büro:NAS@nas.lan,Server@10.0.0.1,Web@web.lan" {
		t.Errorf("Verbindungen nach Import: %s", got)
	}
	if !strings.Contains(m.successMsg, "2") {
		t.Errorf("Erfolgsmeldung = %q", m.successMsg)
	}
	// Gespeichert, nicht nur im Speicher
	if cfg, _ := LoadConfig(m.configPath); len(cfg.Connections) != 3 {
		t.Errorf("Import nicht gespeichert: %d Verbindungen", len(cfg.Connections))
	}
}

// TestImportReplaceExisting prüft das bewusste Ersetzen eines vorhandenen Namens.
func TestImportReplaceExisting(t *testing.T) {
	m := newImportTestModel(t, "Server")
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("Server", "neu.lan"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	m = press(t, m, "enter")
	if !strings.Contains(stripANSI(m.View()), "0 / 1") {
		t.Fatalf("Dublette nicht abgewählt:\n%s", stripANSI(m.View()))
	}
	m = press(t, m, " ")
	if !strings.Contains(stripANSI(m.View()), "(vorhanden – wird ersetzt)") {
		t.Errorf("Ersetzen-Hinweis fehlt:\n%s", stripANSI(m.View()))
	}
	m = press(t, m, "enter")
	if len(m.connections) != 1 || m.connections[0].Host != "neu.lan" || m.connections[0].ID != "id-0" {
		t.Errorf("ersetzt: %+v", m.connections)
	}
}

// TestImportNothingSelected prüft Enter ohne Auswahl.
func TestImportNothingSelected(t *testing.T) {
	m := newImportTestModel(t, "Server")
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("Server", "neu.lan"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	m = press(t, m, "enter", "enter")
	if m.state != ViewImportSelect || m.errorMsg == "" {
		t.Errorf("leere Auswahl: state=%v err=%q", m.state, m.errorMsg)
	}
}

// TestImportFileErrorsShown prüft die Meldungen im Dateischritt.
func TestImportFileErrorsShown(t *testing.T) {
	m := newImportTestModel(t)
	m = press(t, m, "I")
	m.importPathInput.SetValue("fehlt.json")
	m = press(t, m, "enter")
	if m.state != ViewImportFile || !strings.Contains(m.errorMsg, "fehlt.json") {
		t.Errorf("fehlende Datei: state=%v err=%q", m.state, m.errorMsg)
	}
	_ = os.WriteFile(filepath.Join(m.exportHomeDir, "kaputt.json"), []byte("{"), 0o600)
	m.importPathInput.SetValue("kaputt.json")
	m = press(t, m, "enter")
	if m.state != ViewImportFile || m.errorMsg == "" || strings.Contains(m.errorMsg, "unexpected") {
		t.Errorf("kaputte Datei: state=%v err=%q (übersetzte Meldung erwartet)", m.state, m.errorMsg)
	}
	m.importPathInput.SetValue("  ")
	m = press(t, m, "enter")
	if m.state != ViewImportFile || m.errorMsg == "" {
		t.Errorf("leerer Name: state=%v err=%q", m.state, m.errorMsg)
	}
}

// TestImportOnEmptyList prüft den Import auf einem frischen Rechner.
func TestImportOnEmptyList(t *testing.T) {
	m := newImportTestModel(t)
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("A", "a.lan"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	m = press(t, m, "enter", "enter")
	if len(m.connections) != 1 || m.state != ViewList {
		t.Errorf("Import in leere Liste: %d Verbindungen, state=%v", len(m.connections), m.state)
	}
}

// TestImportSkippedShown prüft den Hinweis auf übersprungene Einträge.
func TestImportSkippedShown(t *testing.T) {
	m := newImportTestModel(t)
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("A", "a.lan"), importConn("", "x"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	m = press(t, m, "enter")
	if !strings.Contains(stripANSI(m.View()), "übersprungen") || m.importSkipped != 1 {
		t.Errorf("übersprungen nicht angezeigt (%d):\n%s", m.importSkipped, stripANSI(m.View()))
	}
}

// TestImportEscSteps prüft Esc: Auswahl -> Dateischritt -> Liste, nichts gespeichert.
func TestImportEscSteps(t *testing.T) {
	m := newImportTestModel(t, "Alt")
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("Neu", "n.lan"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	m = press(t, m, "enter", "esc")
	if m.state != ViewImportFile {
		t.Errorf("Esc in der Auswahl: state=%v", m.state)
	}
	m = press(t, m, "esc")
	if m.state != ViewList || len(m.connections) != 1 {
		t.Errorf("Esc im Dateischritt: state=%v, %d Verbindungen", m.state, len(m.connections))
	}
}

// TestImportGroupToggle prüft die Gruppenauswahl in der Importliste.
func TestImportGroupToggle(t *testing.T) {
	m := newImportTestModel(t)
	writeImportFixture(t, m.exportHomeDir, "i.json", importConn("Büro:X", "x"), importConn("Büro:Y", "y"), importConn("Z", "z"))
	m = press(t, m, "I")
	m.importPathInput.SetValue("i.json")
	// Zeilen: Büro, X, Y, Z - Gruppe abwählen
	m = press(t, m, "enter", " ", "enter")
	if len(m.connections) != 1 || m.connections[0].Name != "Z" {
		t.Errorf("nach Gruppen-Abwahl importiert: %+v", m.connections)
	}
}

// TestListHelpMentionsImport prüft den Hinweis in der Listenhilfe.
func TestListHelpMentionsImport(t *testing.T) {
	m := newListTestModel(t, "A")
	if !strings.Contains(stripANSI(renderListText(m)), "I:") {
		t.Error("Listenhilfe nennt die Import-Taste nicht")
	}
}

// --- Gemeinsame Auswahlliste ---

// TestConnChecklistToggle prüft die gemeinsame Auswahllogik von Export und Import.
func TestConnChecklistToggle(t *testing.T) {
	conns := []Connection{importConn("A", "a"), importConn("G:X", "x"), importConn("G:Y", "y")}
	list := newConnChecklist(conns, func(c Connection) bool { return c.Name != "G:Y" })
	if list.count() != 2 {
		t.Fatalf("Vorauswahl = %d", list.count())
	}
	// Zeilen: A, G, X, Y - Gruppe ist teilweise gewählt -> Leertaste wählt alle
	list.handleKey("down", 15)
	list.handleKey(" ", 15)
	if list.count() != 3 {
		t.Errorf("Gruppe teilweise -> alle: %d", list.count())
	}
	list.handleKey("a", 15)
	if list.count() != 0 {
		t.Errorf("a bei allen gewählt -> keine: %d", list.count())
	}
	if list.handleKey("x", 15) {
		t.Error("unbekannte Taste als verarbeitet gemeldet")
	}
	list.handleKey("end", 15)
	list.handleKey("down", 15)
	if list.cursor != 3 {
		t.Errorf("Cursor über das Ende hinaus: %d", list.cursor)
	}
}
