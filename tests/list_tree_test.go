// Paket main - Tests für die Verbindungsliste (Build 48)
//
// Prüft:
//   - Gruppierung per Doppelpunkt im Namen ("Abc:123" -> Gruppe "Abc")
//   - Auf-/Zuklappen der Gruppen per Tastatur
//   - Scrollbare Liste ab mehr als 15 Zeilen (inkl. Bildlauf-Hinweisen)
//   - Tunnel-Kurzanzeige "+N weitere" ab dem dritten Port
//   - Auswahl der neu angelegten Verbindung nach dem Speichern
//   - Markierung des Namens auch bei verbundenen Einträgen
//   - Validierung von Gruppennamen im Formular
//
// @author Kurt Ingwer
// @date   2026-10-21 19:45
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// --- Hilfsfunktionen ---

// newListTestModel erstellt ein Modell in der Listenansicht mit den
// angegebenen Verbindungsnamen (Sprache Deutsch, IDs "id-0", "id-1", ...).
func newListTestModel(t *testing.T, names ...string) AppModel {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "connections.json")
	cfg := NewAppConfig()
	cfg.Language = LangDeutsch
	for i, name := range names {
		conn := NewConnection(name, "10.0.0.1", 22, "root", AuthPassword)
		conn.ID = fmt.Sprintf("id-%d", i)
		cfg.Connections = append(cfg.Connections, conn)
	}
	if err := SaveConfig(configPath, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	m := NewAppModel(configPath, "48", NewSSHManager(NewLogger()))
	if m.state != ViewList {
		t.Fatalf("Startansicht = %v, erwartet ViewList", m.state)
	}
	return m
}

// keyMsgFor wandelt einen Tastennamen in ein tea.KeyMsg um.
func keyMsgFor(key string) tea.KeyMsg {
	switch key {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// press sendet nacheinander Tasten an das Modell und liefert das Ergebnis.
func press(t *testing.T, m AppModel, keys ...string) AppModel {
	t.Helper()
	for _, key := range keys {
		updated, _ := m.Update(keyMsgFor(key))
		m = updated.(AppModel)
	}
	return m
}

// renderListText rendert die Listenansicht als String.
func renderListText(m AppModel) string {
	var s strings.Builder
	m.renderList(&s)
	return s.String()
}

// markConnected trägt eine Verbindung als verbunden im SSH-Manager ein,
// ohne echte Netzwerkverbindung (kein Keepalive-Goroutine).
func markConnected(m AppModel, conn Connection, tunnelErrors map[int]string) {
	if tunnelErrors == nil {
		tunnelErrors = map[int]string{}
	}
	_, cancel := context.WithCancel(context.Background())
	m.sshManager.mu.Lock()
	m.sshManager.connections[conn.ID] = &ManagedConnection{
		Status: &ConnectionStatus{
			Connected:    true,
			TunnelErrors: tunnelErrors,
			Cancel:       cancel,
		},
		Config: conn,
	}
	m.sshManager.mu.Unlock()
}

// connByName sucht eine Verbindung im Modell anhand ihres Namens.
func connByName(t *testing.T, m AppModel, name string) Connection {
	t.Helper()
	for _, c := range m.connections {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("Verbindung %q nicht gefunden", name)
	return Connection{}
}

// --- splitGroupName / validGroupName ---

// TestSplitGroupName prüft die Zerlegung am ersten Doppelpunkt.
func TestSplitGroupName(t *testing.T) {
	cases := []struct {
		name, group, child string
		ok                 bool
	}{
		{"Abc:123", "Abc", "123", true},
		{"Abc : 234", "Abc", "234", true},
		{"Büro:Server:1", "Büro", "Server:1", true},
		{"Abc", "", "", false},
		{"", "", "", false},
		{"Abc:", "", "", false},
		{":123", "", "", false},
		{"  :  ", "", "", false},
	}
	for _, c := range cases {
		group, child, ok := splitGroupName(c.name)
		if group != c.group || child != c.child || ok != c.ok {
			t.Errorf("splitGroupName(%q) = (%q, %q, %v), erwartet (%q, %q, %v)",
				c.name, group, child, ok, c.group, c.child, c.ok)
		}
	}
}

// TestValidGroupName prüft die Formular-Validierung für Namen mit Doppelpunkt.
func TestValidGroupName(t *testing.T) {
	valid := []string{"Abc", "Abc:123", "a:b:c", "Büro : PC"}
	for _, name := range valid {
		if !validGroupName(name) {
			t.Errorf("validGroupName(%q) = false, erwartet true", name)
		}
	}
	invalid := []string{"Abc:", ":123", " : ", "Abc:  "}
	for _, name := range invalid {
		if validGroupName(name) {
			t.Errorf("validGroupName(%q) = true, erwartet false", name)
		}
	}
}

// --- buildListRows ---

// TestBuildListRowsGroupsCollapsed prüft dass Verbindungen mit gleichem
// Präfix zu einer zugeklappten Gruppe zusammengefasst werden.
func TestBuildListRowsGroupsCollapsed(t *testing.T) {
	conns := sortConnectionsByName([]Connection{
		{ID: "1", Name: "Abc:123"},
		{ID: "2", Name: "Abc:234"},
		{ID: "3", Name: "Zeta"},
		{ID: "4", Name: "Alpha"},
	})
	rows := buildListRows(conns, map[string]bool{})

	if len(rows) != 3 {
		t.Fatalf("Zeilen = %d, erwartet 3 (Gruppe Abc, Alpha, Zeta): %+v", len(rows), rows)
	}
	if rows[0].kind != rowGroup || rows[0].label != "Abc" || len(rows[0].children) != 2 {
		t.Errorf("Zeile 0 = %+v, erwartet Gruppe Abc mit 2 Kindern", rows[0])
	}
	if rows[1].kind != rowSingle || rows[1].label != "Alpha" {
		t.Errorf("Zeile 1 = %+v, erwartet Alpha", rows[1])
	}
	if rows[2].kind != rowSingle || rows[2].label != "Zeta" {
		t.Errorf("Zeile 2 = %+v, erwartet Zeta", rows[2])
	}
}

// TestBuildListRowsGroupExpanded prüft die Kindzeilen einer aufgeklappten Gruppe.
func TestBuildListRowsGroupExpanded(t *testing.T) {
	conns := sortConnectionsByName([]Connection{
		{ID: "1", Name: "Abc:123"},
		{ID: "2", Name: "Abc:234"},
		{ID: "3", Name: "Zeta"},
	})
	rows := buildListRows(conns, map[string]bool{"abc": true})

	if len(rows) != 4 {
		t.Fatalf("Zeilen = %d, erwartet 4: %+v", len(rows), rows)
	}
	if rows[1].kind != rowChild || rows[1].label != "123" || rows[1].lastChild {
		t.Errorf("Zeile 1 = %+v, erwartet Kind 123 (nicht letztes)", rows[1])
	}
	if rows[2].kind != rowChild || rows[2].label != "234" || !rows[2].lastChild {
		t.Errorf("Zeile 2 = %+v, erwartet Kind 234 (letztes)", rows[2])
	}
	if conns[rows[2].connIndex].ID != "2" {
		t.Errorf("Kind 234 verweist auf %q, erwartet ID 2", conns[rows[2].connIndex].ID)
	}
}

// TestBuildListRowsCaseInsensitiveGroup prüft dass "abc:x" und "ABC:y"
// in derselben Gruppe landen (Schreibweise des ersten Eintrags gewinnt).
func TestBuildListRowsCaseInsensitiveGroup(t *testing.T) {
	conns := sortConnectionsByName([]Connection{
		{ID: "1", Name: "abc:x"},
		{ID: "2", Name: "ABC:y"},
	})
	rows := buildListRows(conns, map[string]bool{})
	if len(rows) != 1 || rows[0].kind != rowGroup || len(rows[0].children) != 2 {
		t.Fatalf("erwartet eine Gruppe mit 2 Kindern, bekommen %+v", rows)
	}
}

// TestBuildListRowsSingleChildStillGroup prüft dass auch ein einzelner
// Eintrag mit Doppelpunkt als Gruppe erscheint (Doppelpunkt = Trennzeichen).
func TestBuildListRowsSingleChildStillGroup(t *testing.T) {
	rows := buildListRows([]Connection{{ID: "1", Name: "Büro:PC"}}, map[string]bool{})
	if len(rows) != 1 || rows[0].kind != rowGroup || rows[0].label != "Büro" {
		t.Fatalf("erwartet Gruppe Büro, bekommen %+v", rows)
	}
}

// TestBuildListRowsInvalidColonNamesStaySingle prüft dass Namen mit
// leerem Teil vor/nach dem Doppelpunkt nicht gruppiert werden.
func TestBuildListRowsInvalidColonNamesStaySingle(t *testing.T) {
	rows := buildListRows([]Connection{{ID: "1", Name: "Abc:"}, {ID: "2", Name: ":x"}}, map[string]bool{})
	if len(rows) != 2 || rows[0].kind != rowSingle || rows[1].kind != rowSingle {
		t.Fatalf("erwartet zwei Einzelzeilen, bekommen %+v", rows)
	}
}

// TestBuildListRowsSingleAndGroupSameName prüft dass eine Einzelverbindung
// "Abc" neben der Gruppe "Abc" bestehen bleibt.
func TestBuildListRowsSingleAndGroupSameName(t *testing.T) {
	conns := sortConnectionsByName([]Connection{
		{ID: "1", Name: "Abc"},
		{ID: "2", Name: "Abc:1"},
	})
	rows := buildListRows(conns, map[string]bool{})
	if len(rows) != 2 || rows[0].kind != rowSingle || rows[1].kind != rowGroup {
		t.Fatalf("erwartet Einzelzeile + Gruppe, bekommen %+v", rows)
	}
}

// TestBuildListRowsEmpty prüft die leere Liste.
func TestBuildListRowsEmpty(t *testing.T) {
	if rows := buildListRows(nil, nil); len(rows) != 0 {
		t.Errorf("leere Liste ergab %d Zeilen", len(rows))
	}
}

// --- Gruppen-Navigation ---

// TestGroupExpandCollapseKeys prüft Enter/Pfeil rechts/Pfeil links auf Gruppen.
func TestGroupExpandCollapseKeys(t *testing.T) {
	m := newListTestModel(t, "Abc:123", "Abc:234", "Zeta")

	// Cursor steht auf der Gruppe Abc; Enter klappt auf
	// (Zeilen jeweils inkl. Datei-Manager-Eintrag ganz unten)
	m = press(t, m, "enter")
	if len(m.listRows()) != 5 {
		t.Fatalf("nach Enter: %d Zeilen, erwartet 5 (aufgeklappt)", len(m.listRows()))
	}
	if m.state != ViewList {
		t.Errorf("Enter auf Gruppe darf nicht verbinden (state=%v)", m.state)
	}

	// Enter erneut klappt wieder zu
	m = press(t, m, "enter")
	if len(m.listRows()) != 3 {
		t.Fatalf("nach zweitem Enter: %d Zeilen, erwartet 3", len(m.listRows()))
	}

	// Pfeil rechts klappt auf, Pfeil runter auf das erste Kind
	m = press(t, m, "right", "down")
	conn, ok := m.selectedConnection()
	if !ok || conn.Name != "Abc:123" {
		t.Fatalf("Auswahl = %q/%v, erwartet Abc:123", conn.Name, ok)
	}

	// Pfeil links auf einem Kind: Gruppe zuklappen, Cursor auf Gruppe
	m = press(t, m, "left")
	if len(m.listRows()) != 3 || m.cursor != 0 {
		t.Errorf("nach links: %d Zeilen, cursor=%d, erwartet 3 Zeilen und cursor 0", len(m.listRows()), m.cursor)
	}
	if _, ok := m.selectedConnection(); ok {
		t.Error("auf einer Gruppenzeile darf keine Verbindung ausgewählt sein")
	}
}

// TestGroupRowIgnoresEditDeleteDisconnect prüft dass e/d/x auf einer
// Gruppenzeile nichts auslösen.
func TestGroupRowIgnoresEditDeleteDisconnect(t *testing.T) {
	m := newListTestModel(t, "Abc:1", "Abc:2")
	for _, key := range []string{"e", "d", "x"} {
		got := press(t, m, key)
		if got.state != ViewList {
			t.Errorf("Taste %q auf Gruppe wechselte zu state %v", key, got.state)
		}
	}
}

// TestChildRowEnterConnects prüft dass Enter auf einem Kind den
// Verbindungsaufbau startet (ViewConnecting + Kommando).
func TestChildRowEnterConnects(t *testing.T) {
	m := newListTestModel(t, "Abc:1", "Abc:2")
	m = press(t, m, "right", "down")
	updated, cmd := m.Update(keyMsgFor("enter"))
	um := updated.(AppModel)
	if um.state != ViewConnecting || cmd == nil {
		t.Errorf("Enter auf Kind: state=%v cmd=%v, erwartet ViewConnecting + Kommando", um.state, cmd != nil)
	}
	if um.activeID != connByName(t, m, "Abc:1").ID {
		t.Errorf("activeID = %q, erwartet ID von Abc:1", um.activeID)
	}
}

// TestGroupRowShowsConnectedDot prüft den grünen Punkt an der Gruppe,
// sobald ein Kind verbunden ist.
func TestGroupRowShowsConnectedDot(t *testing.T) {
	m := newListTestModel(t, "Abc:1", "Abc:2")
	text := renderListText(m)
	if strings.Contains(text, "●") {
		t.Fatal("ohne Verbindung darf kein Punkt erscheinen")
	}
	markConnected(m, connByName(t, m, "Abc:2"), nil)
	text = renderListText(m)
	if !strings.Contains(text, "● ► Abc (2)") {
		t.Errorf("Gruppe mit verbundenem Kind zeigt keinen Punkt:\n%s", text)
	}
}

// TestGroupRendering prüft die Darstellung von Gruppe und Kindern.
func TestGroupRendering(t *testing.T) {
	m := newListTestModel(t, "Abc:123", "Abc:234")
	if text := renderListText(m); !strings.Contains(text, "► Abc (2)") {
		t.Errorf("zugeklappte Gruppe fehlt:\n%s", text)
	}
	m = press(t, m, "right")
	text := renderListText(m)
	for _, want := range []string{"▼ Abc (2)", "├ 123", "└ 234"} {
		if !strings.Contains(text, want) {
			t.Errorf("aufgeklappte Gruppe: %q fehlt:\n%s", want, text)
		}
	}
}

// --- Scrollen ---

// TestScrollOffset prüft die Berechnung des sichtbaren Fensters.
func TestScrollOffset(t *testing.T) {
	cases := []struct {
		total, cursor, offset, limit, want int
	}{
		{10, 0, 0, 15, 0},  // passt komplett
		{20, 0, 0, 15, 0},  // Anfang
		{20, 14, 0, 15, 0}, // letzte sichtbare Zeile
		{20, 15, 0, 15, 1}, // eine Zeile weiter -> scrollen
		{20, 19, 0, 15, 5}, // ganz unten
		{20, 3, 5, 15, 3},  // nach oben aus dem Fenster -> mitscrollen
		{20, 10, 5, 15, 5}, // im Fenster -> Offset bleibt
		{20, 19, 9, 15, 5}, // Offset zu groß -> begrenzen
		{0, 0, 3, 15, 0},   // leere Liste
		{16, 0, 0, 0, 0},   // ungültiges Limit
	}
	for _, c := range cases {
		if got := scrollOffset(c.total, c.cursor, c.offset, c.limit); got != c.want {
			t.Errorf("scrollOffset(%d,%d,%d,%d) = %d, erwartet %d",
				c.total, c.cursor, c.offset, c.limit, got, c.want)
		}
	}
}

// TestVisibleRowLimit prüft die maximale Zeilenzahl (15) und die
// Anpassung an kleine Terminals.
func TestVisibleRowLimit(t *testing.T) {
	if got := visibleRowLimit(0); got != maxVisibleListRows {
		t.Errorf("unbekannte Höhe: %d, erwartet %d", got, maxVisibleListRows)
	}
	if got := visibleRowLimit(100); got != maxVisibleListRows {
		t.Errorf("große Höhe: %d, erwartet %d", got, maxVisibleListRows)
	}
	if got := visibleRowLimit(15); got >= 15 || got < 3 {
		t.Errorf("Höhe 15: %d, erwartet weniger als 15 und mindestens 3", got)
	}
	if got := visibleRowLimit(4); got != 3 {
		t.Errorf("winzige Höhe: %d, erwartet Minimum 3", got)
	}
}

// namesN erzeugt n eindeutige, alphabetisch sortierbare Namen.
func namesN(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("Host%02d", i+1)
	}
	return names
}

// TestListShowsAllUpToFifteen prüft dass bis 15 Zeilen (14 Verbindungen +
// Datei-Manager-Eintrag) alles sichtbar ist und keine Bildlauf-Hinweise
// erscheinen.
func TestListShowsAllUpToFifteen(t *testing.T) {
	m := newListTestModel(t, namesN(14)...)
	text := renderListText(m)
	if !strings.Contains(text, "Host01") || !strings.Contains(text, "Host14") || !strings.Contains(text, m.tr(txtFMListEntry)) {
		t.Errorf("bei 15 Zeilen fehlen Zeilen:\n%s", text)
	}
	if strings.Contains(text, "↑") || strings.Contains(text, "↓") {
		t.Errorf("bei 15 Zeilen darf kein Bildlauf-Hinweis erscheinen:\n%s", text)
	}
}

// TestListScrollsBeyondFifteen prüft Fenster und Hinweise ab 16 Zeilen
// (20 Verbindungen + Datei-Manager-Eintrag = 21 Zeilen).
func TestListScrollsBeyondFifteen(t *testing.T) {
	m := newListTestModel(t, namesN(20)...)
	text := renderListText(m)
	if !strings.Contains(text, "Host15") || strings.Contains(text, "Host16") {
		t.Errorf("Startfenster soll Host01..Host15 zeigen:\n%s", text)
	}
	if !strings.Contains(text, "↓ +6") || strings.Contains(text, "↑") {
		t.Errorf("Startfenster: Hinweis '↓ +6' erwartet, kein '↑':\n%s", text)
	}

	// Ans Ende scrollen: Host07..Host20 + Datei-Manager-Eintrag
	m = press(t, m, "end")
	text = renderListText(m)
	if strings.Contains(text, "Host06") || !strings.Contains(text, "Host20") || !strings.Contains(text, m.tr(txtFMListEntry)) {
		t.Errorf("Ende: Fenster soll Host07..Host20 und den Datei-Manager zeigen:\n%s", text)
	}
	if !strings.Contains(text, "↑ +6") || strings.Contains(text, "↓") {
		t.Errorf("Ende: Hinweis '↑ +6' erwartet, kein '↓':\n%s", text)
	}

	// Schrittweise wieder hoch: Fenster folgt erst am oberen Rand
	m = press(t, m, "up")
	if m.listOffset != 6 {
		t.Errorf("listOffset nach einem Schritt hoch = %d, erwartet 6", m.listOffset)
	}
	m = press(t, m, "home")
	if m.cursor != 0 || m.listOffset != 0 {
		t.Errorf("Home: cursor=%d offset=%d, erwartet 0/0", m.cursor, m.listOffset)
	}
}

// TestListPageKeys prüft Bild-auf/Bild-ab.
func TestListPageKeys(t *testing.T) {
	m := newListTestModel(t, namesN(40)...)
	m = press(t, m, "pgdown")
	if m.cursor != maxVisibleListRows {
		t.Errorf("PgDown: cursor=%d, erwartet %d", m.cursor, maxVisibleListRows)
	}
	// Letzte Zeile ist der Datei-Manager-Eintrag (Index 40)
	m = press(t, m, "pgdown", "pgdown", "pgdown")
	if m.cursor != 40 {
		t.Errorf("PgDown am Ende: cursor=%d, erwartet 40", m.cursor)
	}
	m = press(t, m, "pgup")
	if m.cursor != 40-maxVisibleListRows {
		t.Errorf("PgUp: cursor=%d, erwartet %d", m.cursor, 40-maxVisibleListRows)
	}
}

// TestListDownStopsAtEnd prüft dass der Cursor nicht über das Ende
// (Datei-Manager-Eintrag, Index 2) läuft.
func TestListDownStopsAtEnd(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	m = press(t, m, "down", "down", "down", "down")
	if m.cursor != 2 {
		t.Errorf("cursor=%d, erwartet 2", m.cursor)
	}
	m = press(t, m, "up", "up", "up")
	if m.cursor != 0 {
		t.Errorf("cursor=%d, erwartet 0", m.cursor)
	}
}

// --- Tunnel-Kurzanzeige ---

// TestTunnelSummaryShowsTwoPortsThenCount prüft "+N weitere" ab dem dritten Port.
func TestTunnelSummaryShowsTwoPortsThenCount(t *testing.T) {
	m := newListTestModel(t, "Server")
	m.connections[0].Tunnels = []TunnelConfig{
		{LocalPort: 3306, RemotePort: 3306, Enabled: true},
		{LocalPort: 8080, RemotePort: 8080, Enabled: true},
		{LocalPort: 5432, RemotePort: 5432, Enabled: true},
		{LocalPort: 6379, RemotePort: 6379, Enabled: true},
		{LocalPort: 9000, RemotePort: 9000, Enabled: true},
	}
	text := renderListText(m)
	if !strings.Contains(text, "[Tunnel: 3306,8080 +3 weitere]") {
		t.Errorf("Kurzanzeige fehlt:\n%s", text)
	}
	if strings.Contains(text, "5432") {
		t.Errorf("dritter Port darf nicht mehr genannt werden:\n%s", text)
	}
}

// TestTunnelSummarySingularAndShort prüft Einzahl und kurze Listen.
func TestTunnelSummarySingularAndShort(t *testing.T) {
	m := newListTestModel(t, "Server")
	m.connections[0].Tunnels = []TunnelConfig{
		{LocalPort: 1, RemotePort: 1, Enabled: true},
		{LocalPort: 2, RemotePort: 2, Enabled: true},
		{LocalPort: 3, RemotePort: 3, Enabled: true},
	}
	if text := renderListText(m); !strings.Contains(text, "[Tunnel: 1,2 +1 weiterer]") {
		t.Errorf("Einzahl fehlt:\n%s", text)
	}

	m.connections[0].Tunnels = m.connections[0].Tunnels[:2]
	// Nur die Zeile der Verbindung prüfen (der Datei-Manager-Hinweis darunter
	// enthält "weitere Verbindung")
	if line := stripANSI(selectedLine(t, renderListText(m), "Server")); !strings.Contains(line, "[Tunnel: 1,2]") || strings.Contains(line, "weiter") {
		t.Errorf("zwei Ports sollen vollständig ohne Zusatz erscheinen: %q", line)
	}
}

// TestTunnelSummaryHiddenFailedPortRed prüft den Review-Fund: Ist ein
// verborgener Tunnel (ab Port 3) fehlgeschlagen, wird "+N weitere" rot.
func TestTunnelSummaryHiddenFailedPortRed(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Other", "Server")
	server := connByName(t, m, "Server")
	tunnels := []TunnelConfig{
		{LocalPort: 1001, RemotePort: 1001, Enabled: true},
		{LocalPort: 1002, RemotePort: 1002, Enabled: true},
		{LocalPort: 1003, RemotePort: 1003, Enabled: true},
	}
	for i := range m.connections {
		if m.connections[i].ID == server.ID {
			m.connections[i].Tunnels = tunnels
			server = m.connections[i]
		}
	}
	markConnected(m, server, map[int]string{1003: "belegt"})
	line := selectedLine(t, renderListText(m), "Server")
	idx := strings.Index(line, "+1")
	if idx < 0 {
		t.Fatalf("'+1' fehlt: %q", stripANSI(line))
	}
	before := line[:idx]
	if !strings.Contains(before[strings.LastIndex(before, "\x1b["):], "38;5;196") {
		t.Errorf("'+1 weiterer' mit fehlgeschlagenem verborgenem Tunnel ist nicht rot: %q", line)
	}

	// Ohne Fehler: nicht rot
	markConnected(m, server, nil)
	line = selectedLine(t, renderListText(m), "Server")
	before = line[:strings.Index(line, "+1")]
	if strings.Contains(before[strings.LastIndex(before, "\x1b["):], "38;5;196") {
		t.Errorf("'+1 weiterer' ohne Fehler ist rot: %q", line)
	}
}

// TestTunnelSummaryIgnoresDisabled prüft dass deaktivierte Tunnel nicht zählen.
func TestTunnelSummaryIgnoresDisabled(t *testing.T) {
	m := newListTestModel(t, "Server")
	m.connections[0].Tunnels = []TunnelConfig{
		{LocalPort: 1, RemotePort: 1, Enabled: true},
		{LocalPort: 2, RemotePort: 2, Enabled: false},
		{LocalPort: 3, RemotePort: 3, Enabled: true},
	}
	if text := renderListText(m); !strings.Contains(text, "[Tunnel: 1,3]") {
		t.Errorf("deaktivierter Tunnel wurde mitgezählt:\n%s", text)
	}
}

// TestTunnelSummaryEnglish prüft die englische Variante.
func TestTunnelSummaryEnglish(t *testing.T) {
	m := newListTestModel(t, "Server")
	m.language = LangEnglish
	m.lang = GetTranslations(LangEnglish)
	m.connections[0].Tunnels = []TunnelConfig{
		{LocalPort: 1, RemotePort: 1, Enabled: true},
		{LocalPort: 2, RemotePort: 2, Enabled: true},
		{LocalPort: 3, RemotePort: 3, Enabled: true},
		{LocalPort: 4, RemotePort: 4, Enabled: true},
	}
	if text := renderListText(m); !strings.Contains(text, "[Tunnel: 1,2 +2 more]") {
		t.Errorf("englische Kurzanzeige fehlt:\n%s", text)
	}
}

// --- Auswahl nach dem Speichern ---

// fillForm setzt die Formularfelder direkt.
func fillForm(m AppModel, name, host, tunnels string) AppModel {
	m.inputs[fieldName].SetValue(name)
	m.inputs[fieldHost].SetValue(host)
	m.inputs[fieldUser].SetValue("root")
	m.inputs[fieldTunnels].SetValue(tunnels)
	m.focusedInput = fieldCount - 1
	return m
}

// TestNewConnectionIsSelectedAfterSave prüft dass nach dem Anlegen die
// neue Verbindung markiert ist.
func TestNewConnectionIsSelectedAfterSave(t *testing.T) {
	m := newListTestModel(t, "Alpha", "Mitte", "Zeta")
	m = press(t, m, "n")
	if m.state != ViewCreate {
		t.Fatalf("n öffnet nicht das Formular (state=%v)", m.state)
	}
	m = fillForm(m, "Neu", "10.0.0.9", "")
	m = press(t, m, "enter")
	if m.state != ViewList {
		t.Fatalf("nach Speichern state=%v, Fehler=%q", m.state, m.errorMsg)
	}
	conn, ok := m.selectedConnection()
	if !ok || conn.Name != "Neu" {
		t.Errorf("Auswahl nach Speichern = %q/%v, erwartet Neu", conn.Name, ok)
	}
}

// TestNewGroupedConnectionIsSelectedAndExpanded prüft dass bei einem neuen
// Gruppen-Eintrag die Gruppe aufgeklappt und das Kind ausgewählt wird.
func TestNewGroupedConnectionIsSelectedAndExpanded(t *testing.T) {
	m := newListTestModel(t, "Abc:1", "Abc:2", "Zeta")
	m = press(t, m, "n")
	m = fillForm(m, "Abc:3", "10.0.0.9", "")
	m = press(t, m, "enter")
	conn, ok := m.selectedConnection()
	if !ok || conn.Name != "Abc:3" {
		t.Fatalf("Auswahl = %q/%v, erwartet Abc:3 (Fehler: %q)", conn.Name, ok, m.errorMsg)
	}
	if !m.expandedGroups["abc"] {
		t.Error("Gruppe Abc sollte aufgeklappt sein")
	}
}

// TestNewConnectionSelectedWhenScrolled prüft dass die neue Verbindung
// auch in einer langen Liste sichtbar (im Scroll-Fenster) ist.
func TestNewConnectionSelectedWhenScrolled(t *testing.T) {
	m := newListTestModel(t, namesN(30)...)
	m = press(t, m, "n")
	m = fillForm(m, "Zzz-Neu", "10.0.0.9", "")
	m = press(t, m, "enter")
	conn, ok := m.selectedConnection()
	if !ok || conn.Name != "Zzz-Neu" {
		t.Fatalf("Auswahl = %q/%v, erwartet Zzz-Neu", conn.Name, ok)
	}
	if text := renderListText(m); !strings.Contains(text, "Zzz-Neu") {
		t.Errorf("neue Verbindung liegt außerhalb des sichtbaren Fensters:\n%s", text)
	}
}

// TestEditedConnectionStaysSelected prüft dass nach dem Umbenennen die
// bearbeitete Verbindung ausgewählt bleibt.
func TestEditedConnectionStaysSelected(t *testing.T) {
	m := newListTestModel(t, "Alpha", "Beta", "Gamma")
	m = press(t, m, "down", "e")
	if m.state != ViewEdit {
		t.Fatalf("e öffnet nicht das Formular (state=%v)", m.state)
	}
	m.inputs[fieldName].SetValue("Zulu")
	m.focusedInput = fieldCount - 1
	m = press(t, m, "enter")
	conn, ok := m.selectedConnection()
	if !ok || conn.Name != "Zulu" {
		t.Errorf("Auswahl nach Bearbeiten = %q/%v, erwartet Zulu", conn.Name, ok)
	}
}

// TestFormRejectsInvalidGroupName prüft die Fehlermeldung bei "Abc:".
func TestFormRejectsInvalidGroupName(t *testing.T) {
	m := newListTestModel(t)
	m = press(t, m, "n")
	m = fillForm(m, "Abc:", "10.0.0.9", "")
	m = press(t, m, "enter")
	if m.state != ViewCreate || m.errorMsg == "" {
		t.Errorf("Name 'Abc:' wurde akzeptiert (state=%v, Fehler=%q)", m.state, m.errorMsg)
	}
}

// TestDeleteKeepsCursorInRange prüft den Cursor nach dem Löschen des
// letzten Eintrags.
func TestDeleteKeepsCursorInRange(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	m = press(t, m, "down", "d", "y")
	if m.cursor != 0 {
		t.Errorf("cursor nach Löschen = %d, erwartet 0", m.cursor)
	}
	if _, ok := m.selectedConnection(); !ok {
		t.Error("nach dem Löschen sollte die verbleibende Verbindung ausgewählt sein")
	}
}

// --- Markierung verbundener Einträge ---

// withColorProfile aktiviert für den Test ein ANSI-256-Farbprofil, damit
// lipgloss Escape-Sequenzen erzeugt (im Test ist stdout kein Terminal).
func withColorProfile(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// assertSegmentsHaveBackground prüft dass jedes sichtbare Textstück der
// Zeile den Auswahl-Hintergrund (Farbe 57) trägt.
func assertSegmentsHaveBackground(t *testing.T, line string) {
	t.Helper()
	const background = "48;5;57"
	for _, segment := range strings.Split(line, "\x1b[0m") {
		visible := stripANSI(segment)
		if strings.TrimSpace(visible) == "" && visible == "" {
			continue
		}
		if !strings.Contains(segment, background) {
			t.Errorf("Teilstück %q ohne Auswahl-Hintergrund (Zeile %q)", visible, line)
		}
	}
}

// stripANSI entfernt CSI-Escape-Sequenzen aus einem String.
func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// selectedLine liefert die gerenderte Zeile, die den Namen enthält.
func selectedLine(t *testing.T, text, name string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(stripANSI(line), name) {
			return line
		}
	}
	t.Fatalf("Zeile mit %q nicht gefunden:\n%s", name, text)
	return ""
}

// TestSelectedConnectedRowHighlightsName prüft dass bei einer verbundenen,
// ausgewählten Verbindung auch der Name den Auswahl-Hintergrund hat.
func TestSelectedConnectedRowHighlightsName(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Server")
	m.connections[0].Tunnels = []TunnelConfig{{LocalPort: 8080, RemotePort: 8080, Enabled: true}}
	markConnected(m, m.connections[0], nil)

	line := selectedLine(t, renderListText(m), "Server")
	if !strings.Contains(stripANSI(line), "● Server") {
		t.Fatalf("Zeile ohne Punkt: %q", stripANSI(line))
	}
	assertSegmentsHaveBackground(t, line)
}

// TestSelectedDisconnectedRowHighlightsName prüft den unverbundenen Fall
// (war schon korrekt, darf nicht kaputtgehen).
func TestSelectedDisconnectedRowHighlightsName(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Server")
	assertSegmentsHaveBackground(t, selectedLine(t, renderListText(m), "Server"))
}

// TestSelectedGroupAndChildHighlight prüft Gruppe und Kind mit Verbindung.
func TestSelectedGroupAndChildHighlight(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Abc:1", "Abc:2")
	markConnected(m, connByName(t, m, "Abc:1"), nil)
	assertSegmentsHaveBackground(t, selectedLine(t, renderListText(m), "Abc (2)"))

	m = press(t, m, "right", "down")
	assertSegmentsHaveBackground(t, selectedLine(t, renderListText(m), "├ 1"))
}

// TestFailedTunnelPortRedInList prüft dass ein fehlgeschlagener Tunnel-Port
// einer verbundenen Verbindung in der Liste rot erscheint.
func TestFailedTunnelPortRedInList(t *testing.T) {
	withColorProfile(t)
	m := newListTestModel(t, "Other", "Server")
	server := connByName(t, m, "Server")
	for i := range m.connections {
		if m.connections[i].ID == server.ID {
			m.connections[i].Tunnels = []TunnelConfig{
				{LocalPort: 8080, RemotePort: 8080, Enabled: true},
				{LocalPort: 9090, RemotePort: 9090, Enabled: true},
			}
			server = m.connections[i]
		}
	}
	markConnected(m, server, map[int]string{9090: "address already in use"})

	line := selectedLine(t, renderListText(m), "Server")
	// Cursor steht auf "Other" -> Zeile "Server" ist nicht ausgewählt
	red := "38;5;196"
	idx := strings.Index(line, "9090")
	if idx < 0 {
		t.Fatalf("Port 9090 fehlt: %q", line)
	}
	before := line[:idx]
	lastEsc := strings.LastIndex(before, "\x1b[")
	if lastEsc < 0 || !strings.Contains(before[lastEsc:], red) {
		t.Errorf("Port 9090 ist nicht rot: %q", line)
	}
	idx8080 := strings.Index(line, "8080")
	before8080 := line[:idx8080]
	if strings.Contains(before8080[strings.LastIndex(before8080, "\x1b["):], red) {
		t.Errorf("funktionierender Port 8080 ist rot: %q", line)
	}
	if strings.Contains(stripANSI(line), "already in use") {
		t.Errorf("Fehlermeldung darf in der Liste nicht erscheinen: %q", stripANSI(line))
	}
}
