// Paket main - Tests für das Kopieren von Verbindungen (Taste c), Build 86
//
// Hintergrund: Fable-Review von Build 85.
//   - A1: Nach "c" steht der Cursor im Namensfeld am Ende des Werts.
//   - A2: Der Name der Kopie ist nie länger als connectionNameLimit (50) Runen;
//     zu lange Grundnamen werden runenweise gekürzt (sonst schneidet das
//     Eingabefeld " #2" ab und das Speichern scheitert an "Name vergeben").
//   - A3: Das Formular kennt nur lokale Ports. Remote-Port und Enabled-Flag
//     bleiben über formTunnels() und formTunnelTemplate erhalten (e und c);
//     bei "n" darf nichts durchsickern.
//
// @author Kurt Ingwer
// @date   2026-10-27 17:59
package main

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Hilfsfunktionen ---

// newCopyModelWith erstellt eine Liste mit genau einer Verbindung "src-id"
// (Host srv.example.org, Port 2222, Benutzer admin) mit frei wählbarem Namen
// und Tunneln. newCopySourceModel bleibt unverändert (feste Tunnel 8080/9090).
//
// @param t - Testkontext
// @param name - Name der Quelle
// @param tunnels - Tunnel der Quelle (nil = keine)
// @return AppModel - Modell mit Cursor auf der Verbindung
// @date 2026-10-27 17:59
func newCopyModelWith(t *testing.T, name string, tunnels []TunnelConfig) AppModel {
	t.Helper()
	m := newListTestModel(t)
	conn := NewConnection(name, "srv.example.org", 2222, "admin", AuthPassword)
	conn.ID = "src-id"
	if tunnels != nil {
		conn.Tunnels = tunnels
	}
	if err := AddConnection(m.configPath, conn); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	m.selectConnectionByID("src-id")
	return m
}

// newTunnelSourceModel erstellt die Quelle mit zwei Tunneln, die vom
// Formular-Standard abweichen: 8080 -> 80 (aktiv) und 9090 -> 9090 (inaktiv).
//
// @param t - Testkontext
// @return AppModel - Modell mit Cursor auf der Verbindung
// @date 2026-10-27 17:59
func newTunnelSourceModel(t *testing.T) AppModel {
	t.Helper()
	return newCopyModelWith(t, "web", []TunnelConfig{
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 9090, RemotePort: 9090, Enabled: false},
	})
}

// pressBackspaces sendet n Rücktasten (keyMsgFor kennt keine Rücktaste).
//
// @param t - Testkontext
// @param m - Modell
// @param n - Anzahl Rücktasten
// @return AppModel - Modell nach den Tasten
// @date 2026-10-27 17:59
func pressBackspaces(t *testing.T, m AppModel, n int) AppModel {
	t.Helper()
	for i := 0; i < n; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(AppModel)
	}
	return m
}

// tabToField springt mit Tab bis zum Formularfeld field.
//
// @param t - Testkontext
// @param m - Modell im Formular
// @param field - Ziel-Feldindex
// @return AppModel - Modell mit Fokus auf field
// @date 2026-10-27 17:59
func tabToField(t *testing.T, m AppModel, field int) AppModel {
	t.Helper()
	for i := 0; m.focusedInput != field; i++ {
		if i > fieldCount {
			t.Fatalf("Feld %d per Tab nicht erreichbar", field)
		}
		m = press(t, m, "tab")
	}
	return m
}

// saveFormNow speichert das Formular wie TestCopySavedAsNewConnection:
// Tab bis zum letzten Feld, dann Enter.
//
// @param t - Testkontext
// @param m - Modell im Formular
// @return AppModel - Modell nach dem Speichern (erwartet ViewList)
// @date 2026-10-27 17:59
func saveFormNow(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m = tabToField(t, m, fieldCount-1)
	m = press(t, m, "enter")
	if m.state != ViewList {
		t.Fatalf("nach dem Speichern Ansicht %v, Fehler %q", m.state, m.errorMsg)
	}
	return m
}

// savedConnections lädt die Konfiguration und liefert Quelle und alle
// übrigen Verbindungen getrennt.
//
// @param t - Testkontext
// @param m - Modell (für configPath)
// @return Connection - Verbindung "src-id"
// @return []Connection - alle anderen Verbindungen
// @date 2026-10-27 17:59
func savedConnections(t *testing.T, m AppModel) (Connection, []Connection) {
	t.Helper()
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	var src Connection
	var others []Connection
	for _, c := range cfg.Connections {
		if c.ID == "src-id" {
			src = c
		} else {
			others = append(others, c)
		}
	}
	return src, others
}

// --- A1: Cursor im Namensfeld ---

// TestCopyCursorAtEndOfName prüft, dass der Cursor nach "c" am Ende des
// Namens steht (Position = Runenzahl), auch bei Umlauten und bei der Kopie
// einer Kopie. Bisher blieb er hinter dem Grundnamen stehen, und zwei
// Rücktasten löschten "e" und das Leerzeichen statt " #2" (Review-Fund).
//
// @date 2026-10-27 17:59
func TestCopyCursorAtEndOfName(t *testing.T) {
	for _, name := range []string{"web", "Küche", "web #2"} {
		m := press(t, newCopyModelWith(t, name, nil), "c")
		if m.state != ViewCreate {
			t.Fatalf("%q: Ansicht = %v, erwartet ViewCreate", name, m.state)
		}
		value := m.inputs[fieldName].Value()
		if got, want := m.inputs[fieldName].Position(), len([]rune(value)); got != want {
			t.Errorf("%q: Cursor auf %d, erwartet %d (Ende von %q)", name, got, want, value)
		}
		// " #N" tippt man mit zwei Rücktasten weg - übrig bleibt "Grundname "
		m = pressBackspaces(t, m, 2)
		want := value[:len(value)-2]
		if got := m.inputs[fieldName].Value(); got != want || !strings.HasSuffix(got, " ") {
			t.Errorf("%q: nach 2x Rücktaste %q, erwartet %q", name, got, want)
		}
	}
}

// --- A2: Namenslänge ---

// TestNameLimitConstant prüft die Konstante und dass das Formular sie als
// Zeichenlimit des Namensfeldes nutzt (Single Source of Truth).
//
// @date 2026-10-27 17:59
func TestNameLimitConstant(t *testing.T) {
	if connectionNameLimit != 50 {
		t.Errorf("connectionNameLimit = %d, erwartet 50", connectionNameLimit)
	}
	if got := createFormInputs()[fieldName].CharLimit; got != connectionNameLimit {
		t.Errorf("CharLimit des Namensfeldes = %d, erwartet connectionNameLimit (%d)", got, connectionNameLimit)
	}
}

// TestNextCopyNameLimit prüft die Namenslänge der Kopie: höchstens 50 Runen
// (nicht Bytes), Grundname runenweise gekürzt, Leerzeichen am Ende entfernt,
// " #N" immer vollständig dabei. Grenzen: 46/47 (passt gerade noch), 48/49/50
// (muss gekürzt werden), Umlaute (2 Bytes je Rune), Leerzeichen an der
// Schnittstelle und zweistelliges N.
//
// @date 2026-10-27 17:59
func TestNextCopyNameLimit(t *testing.T) {
	a := func(n int) string { return strings.Repeat("a", n) }
	ae := func(n int) string { return strings.Repeat("ä", n) }
	cases := []struct {
		label    string
		name     string
		existing []string
		want     string
	}{
		{"50 Zeichen", a(50), []string{a(50)}, a(47) + " #2"},
		{"49 Zeichen", a(49), []string{a(49)}, a(47) + " #2"},
		{"48 Zeichen", a(48), []string{a(48)}, a(47) + " #2"},
		{"47 Zeichen passt gerade", a(47), []string{a(47)}, a(47) + " #2"},
		{"46 Zeichen ungekürzt", a(46), []string{a(46)}, a(46) + " #2"},
		{"50 Umlaute zählen als Runen", ae(50), []string{ae(50)}, ae(47) + " #2"},
		{"49 Zeichen mit ä und ö", strings.Repeat("äö", 24) + "ä", nil, strings.Repeat("äö", 23) + "ä" + " #2"},
		{"Leerzeichen an der Schnittstelle", a(46) + " " + "bbb", []string{a(46) + " bbb"}, a(46) + " #2"},
		{"gekürzte Variante #2 vorhanden", a(50), []string{a(50), a(47) + " #2"}, a(47) + " #3"},
		{"Kopie einer gekürzten Kopie", a(47) + " #2", []string{a(50), a(47) + " #2"}, a(47) + " #3"},
		// Regression: kurze Namen wie bisher
		{"kurz: web", "web", []string{"web"}, "web #2"},
		{"kurz: web #2", "web #2", []string{"web", "web #2"}, "web #3"},
	}
	for _, c := range cases {
		var conns []Connection
		for _, n := range c.existing {
			conns = append(conns, Connection{Name: n})
		}
		got := nextCopyName(c.name, conns)
		if n := len([]rune(got)); n > connectionNameLimit {
			t.Errorf("%s: %d Runen, erlaubt höchstens %d (%q)", c.label, n, connectionNameLimit, got)
		}
		if got != c.want {
			t.Errorf("%s: nextCopyName = %q, erwartet %q", c.label, got, c.want)
		}
		if got == strings.TrimSpace(c.name) {
			t.Errorf("%s: Ergebnis ist gleich der Quelle %q", c.label, got)
		}
	}
}

// TestNextCopyNameLimitTwoDigits prüft, dass auch bei N >= 10 die Grenze
// hält: " #10" ist eine Rune länger als " #9", der Grundname muss weiter
// gekürzt werden. Alle Namen #2 bis #9 sind belegt.
//
// @date 2026-10-27 17:59
func TestNextCopyNameLimitTwoDigits(t *testing.T) {
	base := strings.Repeat("a", 50)
	conns := []Connection{{Name: base}}
	for n := 2; n <= 9; n++ {
		conns = append(conns, Connection{Name: strings.Repeat("a", 47) + " #" + string(rune('0'+n))})
	}
	got := nextCopyName(base, conns)
	if n := len([]rune(got)); n > connectionNameLimit {
		t.Errorf("%d Runen, erlaubt höchstens %d (%q)", n, connectionNameLimit, got)
	}
	if !strings.HasSuffix(got, " #10") {
		t.Errorf("nextCopyName = %q, erwartet Ende \" #10\"", got)
	}
	for _, c := range conns {
		if strings.EqualFold(c.Name, got) {
			t.Errorf("Ergebnis %q ist schon vergeben", got)
		}
	}
}

// TestCopyLongNameSavesWithoutDuplicateError prüft den Ablauf in der
// Oberfläche: "c" auf einer Verbindung mit 50 Zeichen, sofort speichern.
// Vorher schnitt das Eingabefeld (Limit 50) " #2" ab; der Wert war dann
// gleich der Quelle und das Speichern scheiterte an "Name bereits vergeben".
// Geprüft mit ASCII und mit Umlauten (Runen statt Bytes).
//
// @date 2026-10-27 17:59
func TestCopyLongNameSavesWithoutDuplicateError(t *testing.T) {
	for _, name := range []string{strings.Repeat("a", 50), strings.Repeat("ä", 50)} {
		m := press(t, newCopyModelWith(t, name, nil), "c")
		if got := m.inputs[fieldName].Value(); got == name {
			t.Fatalf("Namensfeld ist gleich der Quelle %q (Zusatz abgeschnitten)", got)
		}
		m = tabToField(t, m, fieldCount-1)
		m = press(t, m, "enter")
		if m.errorMsg == m.lang.ErrDuplicateName {
			t.Fatalf("%q: Fehler %q beim Speichern der Kopie", name, m.errorMsg)
		}
		if m.state != ViewList {
			t.Fatalf("%q: nach dem Speichern Ansicht %v, Fehler %q", name, m.state, m.errorMsg)
		}
		src, others := savedConnections(t, m)
		if len(others) != 1 {
			t.Fatalf("%q: %d Kopien, erwartet 1", name, len(others))
		}
		cpy := others[0]
		if src.Name != name {
			t.Errorf("Quelle verändert: %q", src.Name)
		}
		if n := len([]rune(cpy.Name)); n > connectionNameLimit || cpy.Name == name || !strings.HasSuffix(cpy.Name, " #2") {
			t.Errorf("Kopie heißt %q (%d Runen), erwartet <= %d Runen, anders als die Quelle, Ende \" #2\"", cpy.Name, n, connectionNameLimit)
		}
	}
}

// --- A3: Tunnel-Details erhalten ---

// TestFormTunnels prüft die reine Zuordnung Formular-Ports -> Tunnel:
// Treffer im Template werden komplett übernommen (RemotePort, Enabled),
// neue Ports bekommen Port->Port und aktiv, entfernte Ports fallen weg,
// die Reihenfolge der Eingabe gilt. Randfälle: nil-Template, leere Ports,
// doppelte Ports (die Validierung meldet sie später).
//
// @date 2026-10-27 17:59
func TestFormTunnels(t *testing.T) {
	tmpl := []TunnelConfig{
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 9090, RemotePort: 9090, Enabled: false},
	}
	cases := []struct {
		label string
		ports []int
		tmpl  []TunnelConfig
		want  []TunnelConfig
	}{
		{"nil-Template: Standard", []int{8080, 9090}, nil, []TunnelConfig{
			{LocalPort: 8080, RemotePort: 8080, Enabled: true},
			{LocalPort: 9090, RemotePort: 9090, Enabled: true}}},
		{"alles aus dem Template", []int{8080, 9090}, tmpl, tmpl},
		{"Reihenfolge der Eingabe", []int{9090, 8080}, tmpl, []TunnelConfig{
			{LocalPort: 9090, RemotePort: 9090, Enabled: false},
			{LocalPort: 8080, RemotePort: 80, Enabled: true}}},
		{"Port entfernt", []int{8080}, tmpl, []TunnelConfig{
			{LocalPort: 8080, RemotePort: 80, Enabled: true}}},
		{"Port ergänzt", []int{8080, 7070}, tmpl, []TunnelConfig{
			{LocalPort: 8080, RemotePort: 80, Enabled: true},
			{LocalPort: 7070, RemotePort: 7070, Enabled: true}}},
		{"nur neue Ports", []int{7070}, tmpl, []TunnelConfig{
			{LocalPort: 7070, RemotePort: 7070, Enabled: true}}},
		{"doppelter Port bleibt doppelt", []int{8080, 8080}, tmpl, []TunnelConfig{
			{LocalPort: 8080, RemotePort: 80, Enabled: true},
			{LocalPort: 8080, RemotePort: 80, Enabled: true}}},
	}
	for _, c := range cases {
		got := formTunnels(c.ports, c.tmpl)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: formTunnels(%v) = %+v, erwartet %+v", c.label, c.ports, got, c.want)
		}
	}
	// Leere Eingaben: nil oder leer sind beide in Ordnung
	for _, label := range []string{"leere Ports, Template", "leere Ports, nil-Template", "nil-Ports, nil-Template"} {
		var got []TunnelConfig
		switch label {
		case "leere Ports, Template":
			got = formTunnels([]int{}, tmpl)
		case "leere Ports, nil-Template":
			got = formTunnels([]int{}, nil)
		default:
			got = formTunnels(nil, nil)
		}
		if len(got) != 0 {
			t.Errorf("%s: %+v, erwartet keine Tunnel", label, got)
		}
	}
}

// TestFormTunnelsDoesNotModifyTemplate prüft, dass das Ergebnis keinen
// Speicher mit dem Template teilt: Wer das Ergebnis ändert (etwa beim
// Speichern), darf die Vorlage des offenen Formulars nicht verändern.
//
// @date 2026-10-27 17:59
func TestFormTunnelsDoesNotModifyTemplate(t *testing.T) {
	tmpl := []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}
	got := formTunnels([]int{8080}, tmpl)
	got[0].RemotePort = 1
	got[0].Enabled = false
	if tmpl[0].RemotePort != 80 || !tmpl[0].Enabled {
		t.Errorf("Template durch das Ergebnis verändert: %+v", tmpl[0])
	}
}

// TestFillFormKeepsTunnelTemplateCopy prüft, dass fillFormFromConnection die
// Tunnel als KOPIE merkt (spätere Änderung der Quelle wirkt nicht zurück)
// und dass eine Verbindung ohne Tunnel ein leeres Template ergibt.
//
// @date 2026-10-27 17:59
func TestFillFormKeepsTunnelTemplateCopy(t *testing.T) {
	m := newListTestModel(t)
	m.inputs = createFormInputs()
	conn := Connection{Name: "x", Tunnels: []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}}
	m.fillFormFromConnection(conn)
	want := []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}
	if !reflect.DeepEqual(m.formTunnelTemplate, want) {
		t.Fatalf("formTunnelTemplate = %+v, erwartet %+v", m.formTunnelTemplate, want)
	}
	conn.Tunnels[0].RemotePort = 1
	if m.formTunnelTemplate[0].RemotePort != 80 {
		t.Error("Template teilt den Speicher mit der Quelle (keine Kopie)")
	}
	m.fillFormFromConnection(Connection{Name: "y"})
	if len(m.formTunnelTemplate) != 0 {
		t.Errorf("Verbindung ohne Tunnel: Template %+v, erwartet leer", m.formTunnelTemplate)
	}
}

// TestCopyKeepsTunnelDetails prüft "c" und sofort speichern: Die Kopie hat
// genau die Tunnel der Quelle - 8080 -> 80 (nicht 8080 -> 8080) und
// 9090 inaktiv (nicht aktiv).
//
// @date 2026-10-27 17:59
func TestCopyKeepsTunnelDetails(t *testing.T) {
	m := saveFormNow(t, press(t, newTunnelSourceModel(t), "c"))
	src, others := savedConnections(t, m)
	if len(others) != 1 {
		t.Fatalf("%d Kopien, erwartet 1", len(others))
	}
	want := []TunnelConfig{
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 9090, RemotePort: 9090, Enabled: false},
	}
	if !reflect.DeepEqual(others[0].Tunnels, want) {
		t.Errorf("Tunnel der Kopie: %+v, erwartet %+v", others[0].Tunnels, want)
	}
	if !reflect.DeepEqual(src.Tunnels, want) {
		t.Errorf("Tunnel der Quelle verändert: %+v", src.Tunnels)
	}
}

// TestEditKeepsTunnelDetailsUnchanged prüft "e" und unverändert speichern:
// Die Tunnel bleiben exakt erhalten. Bisher wurde jeder Tunnel beim Speichern
// auf Port->Port und aktiv zurückgesetzt - stille Datenänderung.
//
// @date 2026-10-27 17:59
func TestEditKeepsTunnelDetailsUnchanged(t *testing.T) {
	m := saveFormNow(t, press(t, newTunnelSourceModel(t), "e"))
	src, others := savedConnections(t, m)
	if len(others) != 0 {
		t.Fatalf("%d zusätzliche Verbindungen, erwartet 0", len(others))
	}
	want := []TunnelConfig{
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 9090, RemotePort: 9090, Enabled: false},
	}
	if !reflect.DeepEqual(src.Tunnels, want) {
		t.Errorf("Tunnel nach Bearbeiten: %+v, erwartet %+v", src.Tunnels, want)
	}
}

// TestEditChangedTunnelFieldMergesWithTemplate prüft "e" mit geändertem
// Tunnel-Feld "8080,7070": 8080 behält Remote-Port 80, 7070 ist neu
// (7070 -> 7070, aktiv), 9090 ist weg.
//
// @date 2026-10-27 17:59
func TestEditChangedTunnelFieldMergesWithTemplate(t *testing.T) {
	m := tabToField(t, press(t, newTunnelSourceModel(t), "e"), fieldTunnels)
	m = pressBackspaces(t, m, len([]rune(m.inputs[fieldTunnels].Value())))
	m = typeText(t, m, "8080,7070")
	if got := m.inputs[fieldTunnels].Value(); got != "8080,7070" {
		t.Fatalf("Tunnel-Feld = %q, erwartet \"8080,7070\"", got)
	}
	m = saveFormNow(t, m)
	src, _ := savedConnections(t, m)
	want := []TunnelConfig{
		{LocalPort: 8080, RemotePort: 80, Enabled: true},
		{LocalPort: 7070, RemotePort: 7070, Enabled: true},
	}
	if !reflect.DeepEqual(src.Tunnels, want) {
		t.Errorf("Tunnel nach Bearbeiten: %+v, erwartet %+v", src.Tunnels, want)
	}
}

// TestNewConnectionIgnoresTunnelTemplate prüft, dass das Template nach
// "e"/"c" + Esc nicht in eine mit "n" angelegte Verbindung durchsickert:
// Tunnel 8080 wird 8080 -> 8080 und aktiv, nicht 8080 -> 80.
//
// @date 2026-10-27 17:59
func TestNewConnectionIgnoresTunnelTemplate(t *testing.T) {
	for _, first := range []string{"e", "c"} {
		m := press(t, newTunnelSourceModel(t), first, "esc")
		if m.state != ViewList {
			t.Fatalf("%s, Esc: Ansicht %v, erwartet ViewList", first, m.state)
		}
		m = press(t, m, "n")
		if m.state != ViewCreate {
			t.Fatalf("n: Ansicht %v, erwartet ViewCreate", m.state)
		}
		if len(m.formTunnelTemplate) != 0 {
			t.Errorf("%s, Esc, n: formTunnelTemplate = %+v, erwartet leer", first, m.formTunnelTemplate)
		}
		m = typeText(t, m, "neu")
		m = tabToField(t, m, fieldHost)
		m = typeText(t, m, "new.example.org")
		m = tabToField(t, m, fieldUser)
		m = typeText(t, m, "root")
		m = tabToField(t, m, fieldTunnels)
		m = typeText(t, m, "8080")
		m = saveFormNow(t, m)
		_, others := savedConnections(t, m)
		if len(others) != 1 {
			t.Fatalf("%s: %d neue Verbindungen, erwartet 1", first, len(others))
		}
		want := []TunnelConfig{{LocalPort: 8080, RemotePort: 8080, Enabled: true}}
		if others[0].Name != "neu" || !reflect.DeepEqual(others[0].Tunnels, want) {
			t.Errorf("%s: neue Verbindung %q mit Tunneln %+v, erwartet \"neu\" mit %+v", first, others[0].Name, others[0].Tunnels, want)
		}
	}
}

// --- A4: Schlüssel und Erstellzeit beim Bearbeiten erhalten ---

// newKeyedSourceModel erstellt die Quelle "src-id" mit fester Erstellzeit,
// Authentifizierung und Schlüsselpfad (Host srv.example.org, Port 2222).
//
// @param t - Testkontext
// @param auth - Authentifizierungsmethode der Quelle
// @param keyPath - Schlüsselpfad der Quelle (leer bei Agent)
// @return AppModel - Modell mit Cursor auf der Verbindung
// @date 2026-10-27 18:20
func newKeyedSourceModel(t *testing.T, auth AuthMethod, keyPath string) AppModel {
	t.Helper()
	m := newListTestModel(t)
	conn := NewConnection("web", "srv.example.org", 2222, "admin", auth)
	conn.ID = "src-id"
	conn.KeyPath = keyPath
	conn.CreatedAt = "2026-01-01T00:00:00Z"
	if err := AddConnection(m.configPath, conn); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	m.selectConnectionByID("src-id")
	return m
}

// TestEditKeepsKeyAuthAndCreatedAt prüft, dass "e" und Speichern AuthType,
// KeyPath und CreatedAt der Verbindung erhält - unverändert gespeichert und
// nach einer Änderung des Ports. Bisher baute buildConnectionFromForm eine
// frische Verbindung (AuthPassword, kein Schlüssel, neue Erstellzeit), und
// UpdateConnection ersetzte damit die gespeicherte: Der Schlüssel ging beim
// bloßen Bearbeiten still verloren, die Schlüsseldatei blieb verwaist liegen.
// Zusätzlich der Agent-Fall (AuthAgent ohne Schlüssel). UpdatedAt darf sich ändern.
//
// @date 2026-10-27 18:20
func TestEditKeepsKeyAuthAndCreatedAt(t *testing.T) {
	cases := []struct {
		label   string
		auth    AuthMethod
		keyPath string
		newPort bool
	}{
		{"Schlüssel, unverändert", AuthKey, "~/.ssh/id_x", false},
		{"Schlüssel, Port geändert", AuthKey, "~/.ssh/id_x", true},
		{"Agent, unverändert", AuthAgent, "", false},
	}
	for _, c := range cases {
		m := press(t, newKeyedSourceModel(t, c.auth, c.keyPath), "e")
		if c.newPort {
			m = tabToField(t, m, fieldPort)
			m = press(t, m, "end") // Cursor sicher ans Ende (siehe TestFormCursorsAtEndAfterFill)
			m = pressBackspaces(t, m, len([]rune(m.inputs[fieldPort].Value())))
			m = typeText(t, m, "2200")
		}
		m = saveFormNow(t, m)
		src, others := savedConnections(t, m)
		if len(others) != 0 {
			t.Fatalf("%s: %d zusätzliche Verbindungen, erwartet 0", c.label, len(others))
		}
		if src.AuthType != c.auth {
			t.Errorf("%s: AuthType = %q, erwartet %q", c.label, src.AuthType, c.auth)
		}
		if src.KeyPath != c.keyPath {
			t.Errorf("%s: KeyPath = %q, erwartet %q", c.label, src.KeyPath, c.keyPath)
		}
		if src.CreatedAt != "2026-01-01T00:00:00Z" {
			t.Errorf("%s: CreatedAt = %q, erwartet \"2026-01-01T00:00:00Z\"", c.label, src.CreatedAt)
		}
		wantPort := 2222
		if c.newPort {
			wantPort = 2200
		}
		if src.Port != wantPort {
			t.Errorf("%s: Port = %d, erwartet %d (Änderung muss trotzdem greifen)", c.label, src.Port, wantPort)
		}
	}
}

// TestCopyDoesNotInheritKeyOrCreatedAt ist die Gegenprobe zu A4: Die Kopie
// (c) bekommt weder Schlüssel noch Authentifizierung noch Erstellzeit der
// Quelle - ein Schlüssel gehört zu genau einer Verbindung, die Kopie ist neu.
// Die Quelle bleibt dabei unverändert.
//
// @date 2026-10-27 18:20
func TestCopyDoesNotInheritKeyOrCreatedAt(t *testing.T) {
	m := saveFormNow(t, press(t, newKeyedSourceModel(t, AuthKey, "~/.ssh/id_x"), "c"))
	src, others := savedConnections(t, m)
	if len(others) != 1 {
		t.Fatalf("%d Kopien, erwartet 1", len(others))
	}
	cpy := others[0]
	if cpy.KeyPath != "" || cpy.AuthType != AuthPassword {
		t.Errorf("Kopie: KeyPath=%q AuthType=%q, erwartet leer und %q", cpy.KeyPath, cpy.AuthType, AuthPassword)
	}
	if cpy.CreatedAt == "" || cpy.CreatedAt == src.CreatedAt {
		t.Errorf("Kopie: CreatedAt = %q, erwartet neue Zeit (Quelle %q)", cpy.CreatedAt, src.CreatedAt)
	}
	if src.AuthType != AuthKey || src.KeyPath != "~/.ssh/id_x" || src.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("Quelle verändert: %+v", src)
	}
}

// TestFormCursorsAtEndAfterFill prüft, dass nach "e" und "c" in JEDEM
// vorbelegten Feld der Cursor am Ende des Werts steht. Das Port-Feld hat den
// Standardwert "22"; SetValue lässt den Cursor dort stehen, wenn er schon
// ungleich 0 ist - bei "2222" stand er nach "22", und eine Eingabe landete
// mitten in der Zahl (beobachtet: Rücktasten und Tippen ergaben 22022).
//
// @date 2026-10-27 18:20
func TestFormCursorsAtEndAfterFill(t *testing.T) {
	for _, key := range []string{"e", "c"} {
		m := press(t, newKeyedSourceModel(t, AuthKey, "~/.ssh/id_x"), key)
		for field := 0; field < fieldCount; field++ {
			value := m.inputs[field].Value()
			if got, want := m.inputs[field].Position(), len([]rune(value)); got != want {
				t.Errorf("%s: Feld %d (%q): Cursor auf %d, erwartet %d", key, field, value, got, want)
			}
		}
	}
}

// --- Merken und aussetzen: AutoKeyOff / AutoKeyFailures (Nutzerentscheidung Build 86) ---

// newAutoKeyStateModel erstellt die Quelle "src-id" (Passwort, ohne Schlüssel)
// mit AutoKeyOff=true und AutoKeyFailures=1, wie nach "r" und einem
// Fehlversuch. Diese Felder kennt das Formular nicht.
//
// @param t - Testkontext
// @return AppModel - Modell mit Cursor auf der Verbindung
// @date 2026-10-28 14:00
func newAutoKeyStateModel(t *testing.T) AppModel {
	t.Helper()
	m := newKeyedSourceModel(t, AuthPassword, "")
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.Connections[0].AutoKeyOff = true
		cfg.Connections[0].AutoKeyFailures = 1
		return true
	})
	if err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	m.selectConnectionByID("src-id")
	return m
}

// TestEditKeepsAutoKeyFields: "e" und Speichern behalten AutoKeyOff und
// AutoKeyFailures (keepStoredConnectionFields), unverändert gespeichert und
// nach einer Änderung des Ports. Rot heute (nach Einführung der Felder, ohne
// die Übernahme): Das Formular baut eine frische Verbindung, beide Felder
// stehen danach auf null - die ausgesetzte Vergabe liefe nach dem bloßen
// Bearbeiten wieder an.
//
// @date 2026-10-28 14:00
func TestEditKeepsAutoKeyFields(t *testing.T) {
	for _, newPort := range []bool{false, true} {
		m := press(t, newAutoKeyStateModel(t), "e")
		if newPort {
			m = tabToField(t, m, fieldPort)
			m = press(t, m, "end")
			m = pressBackspaces(t, m, len([]rune(m.inputs[fieldPort].Value())))
			m = typeText(t, m, "2200")
		}
		m = saveFormNow(t, m)
		src, others := savedConnections(t, m)
		if len(others) != 0 {
			t.Fatalf("Port geändert=%v: %d zusätzliche Verbindungen", newPort, len(others))
		}
		if !src.AutoKeyOff || src.AutoKeyFailures != 1 {
			t.Errorf("Port geändert=%v: AutoKeyOff=%v AutoKeyFailures=%d, erwartet true und 1", newPort, src.AutoKeyOff, src.AutoKeyFailures)
		}
		if newPort && src.Port != 2200 {
			t.Errorf("Port = %d, erwartet 2200 (Änderung muss greifen)", src.Port)
		}
	}
}

// TestCopyAndNewStartWithoutAutoKeyFields: Die Kopie (c) und eine neue
// Verbindung (n) starten ohne die Felder: AutoKeyOff aus, 0 Fehlversuche, die
// Vergabe läuft also an. Die Quelle bleibt unverändert (Off, 1 Fehler).
//
// @date 2026-10-28 14:00
func TestCopyAndNewStartWithoutAutoKeyFields(t *testing.T) {
	// c: Kopie der Quelle
	m := saveFormNow(t, press(t, newAutoKeyStateModel(t), "c"))
	src, others := savedConnections(t, m)
	if len(others) != 1 {
		t.Fatalf("%d Kopien, erwartet 1", len(others))
	}
	if others[0].AutoKeyOff || others[0].AutoKeyFailures != 0 {
		t.Errorf("Kopie: AutoKeyOff=%v AutoKeyFailures=%d, erwartet false und 0", others[0].AutoKeyOff, others[0].AutoKeyFailures)
	}
	if !connectionNeedsOwnKey(others[0]) {
		t.Error("Kopie bekommt keine automatische Vergabe")
	}
	if !src.AutoKeyOff || src.AutoKeyFailures != 1 {
		t.Errorf("Quelle verändert: AutoKeyOff=%v AutoKeyFailures=%d", src.AutoKeyOff, src.AutoKeyFailures)
	}

	// n: neue Verbindung, nachdem e die Felder der Quelle im Formular hatte
	m = press(t, newAutoKeyStateModel(t), "e", "esc", "n")
	if m.state != ViewCreate {
		t.Fatalf("n: Ansicht %v, erwartet ViewCreate", m.state)
	}
	m = typeText(t, m, "neu")
	m = tabToField(t, m, fieldHost)
	m = typeText(t, m, "new.example.org")
	m = tabToField(t, m, fieldUser)
	m = typeText(t, m, "root")
	m = saveFormNow(t, m)
	_, others = savedConnections(t, m)
	if len(others) != 1 || others[0].Name != "neu" {
		t.Fatalf("neue Verbindungen: %+v", others)
	}
	if others[0].AutoKeyOff || others[0].AutoKeyFailures != 0 {
		t.Errorf("neue Verbindung: AutoKeyOff=%v AutoKeyFailures=%d, erwartet false und 0", others[0].AutoKeyOff, others[0].AutoKeyFailures)
	}
}
