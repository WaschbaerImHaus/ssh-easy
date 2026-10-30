// Paket main - Tests für die Schlüsselvergabe im Datei-Manager (Build 86)
//
// Nutzerwunsch 2026-10-27: Nach jeder erfolgreichen Anmeldung im Datei-Manager
// (Passwort ODER Auto-Auth ohne eigenen Schlüssel) bekommt die Verbindung
// ungefragt einen Schlüssel - im Fenster mit zwei Verbindungen genauso wie bei
// "Datei-Manager (eine Verbindung)".
//   - Naht fmDeployKey: Tests ersetzen sie durch einen Stub (kein echter sshd).
//     Der Stub schreibt wie AutoDeployKey den GANZEN Eintrag per
//     UpdateConnection zurück (AuthKey + KeyPath) - so zeigt sich, ob der
//     frische Eintrag mit Tunneln übergeben wurde und nichts verloren geht.
//   - Regel (fmNeedsKeyDeploy): nie Lokaler PC; sonst Passwort benutzt oder
//     Verbindung ohne eigenen Schlüssel auf "Passwort".
//   - Meldung: in der Fensteransicht sofort, sonst (Verbindungsaufbau,
//     Passwortabfrage) erst beim Wechsel dorthin (fm.keyNotice); Fehler nur
//     als bereinigte Meldung, die Verbindung bleibt bestehen.
//   - Veraltete Sitzung, gelöschte Verbindung, Gleichzeitigkeit (fmDeployMu)
//     und das gleichzeitige Merken des Pfades (rememberFMPath).
//
// Die Tests tauschen die globale Naht aus und laufen deshalb nie mit
// t.Parallel().
//
// @author Kurt Ingwer
// @date   2026-10-28 13:09
package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
)

// --- Hilfsfunktionen ---

// deployStub ersetzt fmDeployKey: zeichnet Aufrufe auf, zählt überlappende
// Aufrufe und schreibt bei Erfolg AuthKey + KeyPath per UpdateConnection.
type deployStub struct {
	mu sync.Mutex
	// Verbindungen (Stand beim Aufruf) und Dateisysteme je Aufruf
	calls []Connection
	fsys  []remoteFS
	// Fehler, den der Stub liefert (nil = Erfolg)
	err error
	// Wartezeit im Aufruf, damit Überlappungen sichtbar werden
	delay time.Duration
	// gerade laufende Aufrufe und deren Höchstwert
	active    int
	maxActive int
}

// stubKeyPath liefert den Schlüsselpfad, den der Stub für eine ID vergibt.
//
// @date 2026-10-28 13:09
func stubKeyPath(id string) string {
	return "~/.ssh/id_test_" + id
}

// call ist die Ersatzfunktion für fmDeployKey.
//
// @date 2026-10-28 13:09
func (s *deployStub) call(conn Connection, fsys remoteFS, configPath string) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, conn)
	s.fsys = append(s.fsys, fsys)
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	delay, failure := s.delay, s.err
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()
	if delay > 0 {
		time.Sleep(delay)
	}
	if failure != nil {
		return "", failure
	}
	// Wie AutoDeployKey: den übergebenen Eintrag als Ganzes speichern
	conn.AuthType = AuthKey
	conn.KeyPath = stubKeyPath(conn.ID)
	if err := UpdateConnection(configPath, conn); err != nil {
		return "", err
	}
	return conn.KeyPath, nil
}

// count liefert die Zahl der Aufrufe.
//
// @date 2026-10-28 13:09
func (s *deployStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// ids liefert die Verbindungs-IDs aller Aufrufe, sortiert.
//
// @date 2026-10-28 13:09
func (s *deployStub) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, len(s.calls))
	for i, c := range s.calls {
		ids[i] = c.ID
	}
	sort.Strings(ids)
	return ids
}

// installDeployStub setzt fmDeployKey auf einen Stub (err = nil: Erfolg)
// und stellt die vorherige Funktion per t.Cleanup wieder her.
//
// @date 2026-10-28 13:09
func installDeployStub(t *testing.T, err error) *deployStub {
	t.Helper()
	stub := &deployStub{err: err}
	previous := fmDeployKey
	t.Cleanup(func() { fmDeployKey = previous })
	fmDeployKey = stub.call
	return stub
}

// editTestConfig ändert die Konfigurationsdatei und lädt das Modell neu
// (m.connections). So bekommen Verbindungen Tunnel, Agent-Auth oder KeyPath.
//
// @date 2026-10-28 13:09
func editTestConfig(t *testing.T, m AppModel, edit func(cfg *AppConfig)) AppModel {
	t.Helper()
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		edit(cfg)
		return true
	})
	if err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	return m
}

// setTestAuth setzt AuthType und KeyPath einer Verbindung in der Konfiguration.
//
// @date 2026-10-28 13:09
func setTestAuth(t *testing.T, cfg *AppConfig, id string, auth AuthMethod, keyPath string) {
	t.Helper()
	for i := range cfg.Connections {
		if cfg.Connections[i].ID == id {
			cfg.Connections[i].AuthType = auth
			cfg.Connections[i].KeyPath = keyPath
			return
		}
	}
	t.Fatalf("Verbindung %q fehlt in der Konfiguration", id)
}

// loadTestConfig lädt die Konfigurationsdatei des Modells.
//
// @date 2026-10-28 13:09
func loadTestConfig(t *testing.T, m AppModel) *AppConfig {
	t.Helper()
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// testConnByID sucht eine Verbindung in der Konfiguration.
//
// @date 2026-10-28 13:09
func testConnByID(t *testing.T, cfg *AppConfig, id string) Connection {
	t.Helper()
	for _, c := range cfg.Connections {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("Verbindung %q fehlt in der Konfiguration", id)
	return Connection{}
}

// newKeyDeployModel baut ein Modell mit "Links" (id-0) und "Rechts" (id-1),
// einem Verbindungsrekorder und optional geänderter Konfiguration.
//
// @date 2026-10-28 13:09
func newKeyDeployModel(t *testing.T, rec *fmConnectRecorder, edit func(cfg *AppConfig)) AppModel {
	t.Helper()
	m := newListTestModel(t, "Links", "Rechts")
	m.fmConnect = rec.connect
	m.termWidth, m.termHeight = 100, 30
	if edit != nil {
		m = editTestConfig(t, m, edit)
	}
	return m
}

// pickBothSides wählt Links und Rechts (zwei Fenster) und führt alle
// Folgekommandos aus - auch die der Schlüsselvergabe.
//
// @date 2026-10-28 13:09
func pickBothSides(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	return pickSide(t, m, "Rechts")
}

// errAuthFailed ist ein Anmeldefehler, der zur Passwortabfrage führt.
var errAuthFailed = errors.New("ssh: unable to authenticate")

// noControlRunes prüft, dass ein Text keine Steuerzeichen enthält.
//
// @date 2026-10-28 13:09
func noControlRunes(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

// --- Fälle 1-3: Wann wird vergeben ---

// TestKeyDeployAfterPasswordTwoWindows (Fall 1): Links scheitert die Auth,
// das Passwort wird eingegeben, rechts ist Agent. Der Stub muss genau einmal
// mit dem FRISCHEN Eintrag aus der Konfiguration laufen - mit Tunnel; das
// Fenster hält nur eine Kopie ohne Tunnel, und das Zurückschreiben dieser
// Kopie würde den Tunnel löschen (Datenverlust). Danach: KeyPath gespeichert,
// Tunnel erhalten, Liste neu geladen, Pfad in der Meldung.
//
// @date 2026-10-28 13:09
func TestKeyDeployAfterPasswordTwoWindows(t *testing.T) {
	stub := installDeployStub(t, nil)
	rec := newConnectRecorder()
	rec.results["id-0"] = errAuthFailed
	tunnel := TunnelConfig{LocalPort: 8080, RemotePort: 80, Enabled: true}
	m := newKeyDeployModel(t, rec, func(cfg *AppConfig) {
		cfg.Connections[0].Tunnels = []TunnelConfig{tunnel}
		setTestAuth(t, cfg, "id-1", AuthAgent, "")
	})
	m = pickBothSides(t, m)
	if m.state != ViewFMPassword || m.fm.passwordSide != 0 {
		t.Fatalf("state = %v/Seite %d, erwartet Passwortabfrage links", m.state, m.fm.passwordSide)
	}
	if stub.count() != 0 {
		t.Fatalf("Vergabe vor erfolgreicher Anmeldung: %d Aufrufe", stub.count())
	}
	delete(rec.results, "id-0")
	m = typeText(t, m, "geheim")
	m = pressRun(t, m, "enter")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if stub.count() != 1 || stub.ids()[0] != "id-0" {
		t.Fatalf("Aufrufe = %v, erwartet genau einmal id-0 (rechts ist Agent)", stub.ids())
	}
	got := stub.calls[0]
	if len(got.Tunnels) != 1 || got.Tunnels[0] != tunnel {
		t.Errorf("Stub bekam Tunnel %v, erwartet den frischen Eintrag mit %v", got.Tunnels, tunnel)
	}
	if got.KeyPath != "" {
		t.Errorf("KeyPath beim Aufruf = %q, erwartet leer", got.KeyPath)
	}
	if stub.fsys[0] != remoteFS(rec.created["id-0"]) {
		t.Error("Stub bekam nicht das Dateisystem des linken Fensters")
	}
	cfg := loadTestConfig(t, m)
	saved := testConnByID(t, cfg, "id-0")
	if saved.KeyPath != stubKeyPath("id-0") || saved.AuthType != AuthKey {
		t.Errorf("Konfiguration: %q/%q, erwartet gespeicherten Schlüssel", saved.AuthType, saved.KeyPath)
	}
	if len(saved.Tunnels) != 1 || saved.Tunnels[0] != tunnel {
		t.Errorf("Tunnel nach der Vergabe = %v, erwartet %v (nicht verloren)", saved.Tunnels, tunnel)
	}
	if other := testConnByID(t, cfg, "id-1"); other.KeyPath != "" || other.AuthType != AuthAgent {
		t.Errorf("rechte Verbindung verändert: %q/%q", other.AuthType, other.KeyPath)
	}
	if got := connByName(t, m, "Links").KeyPath; got != stubKeyPath("id-0") {
		t.Errorf("m.connections nicht neu geladen: KeyPath %q", got)
	}
	want := fmt.Sprintf(m.lang.KeyDeployedMsg, stubKeyPath("id-0"))
	if !strings.Contains(m.fm.message, want) {
		t.Errorf("fm.message = %q, erwartet %q", m.fm.message, want)
	}
	// Die Infozeile eines Fensters kürzt auf dessen Breite (hier 50 Spalten,
	// zu schmal für den ganzen Text): im breiten Terminal muss die Meldung
	// samt Pfad zu sehen sein
	m.termWidth = 200
	if !strings.Contains(stripANSI(m.View()), want) {
		t.Errorf("Meldung %q fehlt in der Ansicht", want)
	}
}

// TestKeyDeployAutoAuthWithoutOwnKey (Fall 2): Beide Verbindungen melden sich
// automatisch an (ohne Passwort) und stehen auf "Passwort" ohne Schlüssel.
// Beide bekommen einen Schlüssel (zwei Aufrufe) - sonst bliebe die
// Verbindung dauerhaft auf "Passwort". Die Anmeldung bleibt ohne Passwort.
//
// @date 2026-10-28 13:09
func TestKeyDeployAutoAuthWithoutOwnKey(t *testing.T) {
	stub := installDeployStub(t, nil)
	rec := newConnectRecorder()
	m := newKeyDeployModel(t, rec, nil)
	m = pickBothSides(t, m)
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if want := []string{"id-0", "id-1"}; !reflect.DeepEqual(stub.ids(), want) {
		t.Fatalf("Aufrufe = %v, erwartet %v", stub.ids(), want)
	}
	if !reflect.DeepEqual(rec.calls, []string{"id-0/", "id-1/"}) {
		t.Errorf("Anmeldungen = %v, erwartet beide ohne Passwort", rec.calls)
	}
	cfg := loadTestConfig(t, m)
	for _, id := range []string{"id-0", "id-1"} {
		if got := testConnByID(t, cfg, id).KeyPath; got != stubKeyPath(id) {
			t.Errorf("%s: KeyPath %q, erwartet %q", id, got, stubKeyPath(id))
		}
	}
	for _, name := range []string{"Links", "Rechts"} {
		if connByName(t, m, name).KeyPath == "" {
			t.Errorf("%s: m.connections nicht neu geladen", name)
		}
	}
	if !strings.Contains(m.fm.message, "id_test_id-") {
		t.Errorf("keine Meldung über die Vergabe: %q", m.fm.message)
	}
}

// TestKeyDeployAfterPasswordDespiteKeyPath (Fall 3): Die Verbindung hat schon
// einen KeyPath, der Server lehnt ihn aber ab, und das Passwort wird
// eingegeben. Wie im Hauptablauf wird nach einer Passwort-Anmeldung trotzdem
// vergeben (der alte Schlüssel taugt offenbar nicht mehr).
//
// @date 2026-10-28 13:09
func TestKeyDeployAfterPasswordDespiteKeyPath(t *testing.T) {
	stub := installDeployStub(t, nil)
	rec := newConnectRecorder()
	rec.results["id-0"] = errAuthFailed
	m := newKeyDeployModel(t, rec, func(cfg *AppConfig) {
		setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_alt")
		setTestAuth(t, cfg, "id-1", AuthAgent, "")
	})
	m = pickBothSides(t, m)
	if m.state != ViewFMPassword {
		t.Fatalf("state = %v, erwartet Passwortabfrage", m.state)
	}
	delete(rec.results, "id-0")
	m = typeText(t, m, "geheim")
	m = pressRun(t, m, "enter")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
		t.Fatalf("Aufrufe = %v, erwartet %v", stub.ids(), want)
	}
	if stub.calls[0].KeyPath != "~/.ssh/id_alt" {
		t.Errorf("Stub bekam KeyPath %q, erwartet den vorhandenen Eintrag", stub.calls[0].KeyPath)
	}
	if got := testConnByID(t, loadTestConfig(t, m), "id-0").KeyPath; got != stubKeyPath("id-0") {
		t.Errorf("KeyPath nach der Vergabe = %q", got)
	}
}

// --- Fälle 4-5: Wann NICHT vergeben wird ---

// TestKeyDeployNotNeeded (Fall 4): Agent und Verbindungen mit eigenem
// Schlüssel, angemeldet ohne Passwort: kein Aufruf, keine Meldung, die
// Konfiguration bleibt. Randfälle: AuthKey ohne KeyPath und "Passwort" MIT
// KeyPath zählen ebenfalls nicht als "braucht Schlüssel" (connectionNeedsOwnKey
// meint nur "Passwort" ohne KeyPath).
//
// @date 2026-10-28 13:09
func TestKeyDeployNotNeeded(t *testing.T) {
	cases := []struct {
		name      string
		leftAuth  AuthMethod
		leftKey   string
		rightAuth AuthMethod
		rightKey  string
	}{
		{"Agent und Schlüssel", AuthAgent, "", AuthKey, "~/.ssh/id_x"},
		{"zwei Schlüssel", AuthKey, "~/.ssh/id_a", AuthKey, "~/.ssh/id_b"},
		{"zweimal Agent", AuthAgent, "", AuthAgent, ""},
		{"Schlüssel ohne KeyPath", AuthKey, "", AuthAgent, ""},
		{"Passwort mit KeyPath", AuthPassword, "~/.ssh/id_p", AuthKey, "~/.ssh/id_q"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := installDeployStub(t, nil)
			rec := newConnectRecorder()
			m := newKeyDeployModel(t, rec, func(cfg *AppConfig) {
				setTestAuth(t, cfg, "id-0", tc.leftAuth, tc.leftKey)
				setTestAuth(t, cfg, "id-1", tc.rightAuth, tc.rightKey)
			})
			before := loadTestConfig(t, m).Connections
			m = pickBothSides(t, m)
			if m.state != ViewFileManager {
				t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
			}
			if stub.count() != 0 {
				t.Errorf("Vergabe trotz Schlüssel/Agent: %v", stub.ids())
			}
			if m.fm.message != "" || m.fm.keyNotice != "" {
				t.Errorf("Meldung ohne Vergabe: %q / %q", m.fm.message, m.fm.keyNotice)
			}
			if after := loadTestConfig(t, m).Connections; !reflect.DeepEqual(before, after) {
				t.Errorf("Konfiguration verändert:\nvorher %v\nnachher %v", before, after)
			}
		})
	}
}

// TestKeyDeployNeverForLocalPC (Fall 5): Der Lokale PC bekommt nie einen
// Schlüssel - weder links (zwei Fenster) noch im Fenster mit einer
// Verbindung. Rechts ist Agent, damit nur die lokale Seite zählt.
//
// @date 2026-10-28 13:09
func TestKeyDeployNeverForLocalPC(t *testing.T) {
	t.Run("zwei Fenster", func(t *testing.T) {
		stub := installDeployStub(t, nil)
		m := newListTestModel(t, "Rechts")
		m = editTestConfig(t, m, func(cfg *AppConfig) { setTestAuth(t, cfg, "id-0", AuthAgent, "") })
		m.fmConnect = newConnectRecorder().connect
		m.fmLocalConnect = func() (remoteFS, error) { return newFakeFS(), nil }
		m.termWidth, m.termHeight = 100, 30
		m = openPick(t, m)
		m = pickSide(t, m, m.tr(txtFMLocalPC))
		m = pickSide(t, m, "Rechts")
		if m.state != ViewFileManager || m.fm.panels[0].conn.ID != fmLocalConnID {
			t.Fatalf("state = %v, erwartet Datei-Manager mit Lokalem PC links", m.state)
		}
		if stub.count() != 0 || m.fm.message != "" {
			t.Errorf("Vergabe für den Lokalen PC: %v, Meldung %q", stub.ids(), m.fm.message)
		}
	})
	t.Run("eine Verbindung", func(t *testing.T) {
		stub := installDeployStub(t, nil)
		m := newListTestModel(t, "Server")
		m.fmConnect = newConnectRecorder().connect
		m.fmLocalConnect = func() (remoteFS, error) { return newFakeFS(), nil }
		m.termWidth, m.termHeight = 100, 30
		m = openSinglePick(t, m)
		m = pickSide(t, m, m.tr(txtFMLocalPC))
		if m.state != ViewFileManager || !m.fm.single {
			t.Fatalf("state = %v, erwartet Datei-Manager mit einer Verbindung", m.state)
		}
		if stub.count() != 0 || m.fm.message != "" {
			t.Errorf("Vergabe für den Lokalen PC: %v, Meldung %q", stub.ids(), m.fm.message)
		}
	})
}

// TestFMNeedsKeyDeployRule prüft die Regel als reine Funktion, auch den
// Wächter für den Lokalen PC: Selbst mit Passwort und AuthPassword (kann der
// Lokale PC real nicht haben) darf er nie einen Schlüssel bekommen.
//
// @date 2026-10-28 13:09
func TestFMNeedsKeyDeployRule(t *testing.T) {
	password := Connection{ID: "x", AuthType: AuthPassword}
	cases := []struct {
		name         string
		conn         Connection
		withPassword bool
		want         bool
	}{
		{"Passwort ohne Schlüssel, Auto-Auth", password, false, true},
		{"Passwort ohne Schlüssel, mit Passwort", password, true, true},
		{"Passwort mit KeyPath, Auto-Auth", Connection{ID: "x", AuthType: AuthPassword, KeyPath: "~/.ssh/k"}, false, false},
		{"Passwort mit KeyPath, mit Passwort", Connection{ID: "x", AuthType: AuthPassword, KeyPath: "~/.ssh/k"}, true, true},
		{"Agent, Auto-Auth", Connection{ID: "x", AuthType: AuthAgent}, false, false},
		{"Schlüssel, Auto-Auth", Connection{ID: "x", AuthType: AuthKey, KeyPath: "~/.ssh/k"}, false, false},
		{"leerer AuthType, Auto-Auth", Connection{ID: "x"}, false, false},
		{"Lokaler PC, Auto-Auth", Connection{ID: fmLocalConnID}, false, false},
		{"Lokaler PC, mit Passwort", Connection{ID: fmLocalConnID, AuthType: AuthPassword}, true, false},
	}
	for _, tc := range cases {
		if got := fmNeedsKeyDeploy(tc.conn, tc.withPassword); got != tc.want {
			t.Errorf("%s: fmNeedsKeyDeploy = %v, erwartet %v", tc.name, got, tc.want)
		}
	}
}

// --- Fälle 6-7: Fehlschlag ---

// assertKeyDeployFailureNotice prüft die Fehlermeldung: Präfix aus der
// Sprachdatei, Fehlertext erhalten, aber ohne Steuerzeichen (der Text kann
// vom Server stammen - Escape-Injektion).
//
// @date 2026-10-28 13:09
func assertKeyDeployFailureNotice(t *testing.T, m AppModel) {
	t.Helper()
	if !strings.HasPrefix(m.fm.message, m.lang.KeyDeployFailed) {
		t.Errorf("fm.message = %q, erwartet Präfix %q", m.fm.message, m.lang.KeyDeployFailed)
	}
	if !noControlRunes(m.fm.message) {
		t.Errorf("Steuerzeichen in der Meldung: %q", m.fm.message)
	}
	if !strings.Contains(m.fm.message, "serverfehler") {
		t.Errorf("Fehlertext fehlt in der Meldung: %q", m.fm.message)
	}
	if !noControlRunes(m.fm.keyNotice) {
		t.Errorf("Steuerzeichen in keyNotice: %q", m.fm.keyNotice)
	}
}

// evilDeployError ist ein Fehlertext mit Escape-Folgen (Bildschirm löschen,
// Fenstertitel setzen, Klingeln) - so etwas könnte ein Server liefern.
var evilDeployError = errors.New("serverfehler \x1b[2J\x1b]0;pwn\x07 ende\x1b[31m")

// TestKeyDeployFailureTwoWindows (Fall 6): Die Vergabe scheitert in beiden
// Fenstern. Der Datei-Manager bleibt offen, beide Dateisysteme sind verbunden
// und werden nicht geschlossen (die Anmeldung steht ja), die Meldung
// beginnt mit KeyDeployFailed und ist von ESC bereinigt, die Konfiguration
// (Verbindungen) bleibt unverändert.
//
// @date 2026-10-28 13:09
func TestKeyDeployFailureTwoWindows(t *testing.T) {
	stub := installDeployStub(t, evilDeployError)
	rec := newConnectRecorder()
	m := newKeyDeployModel(t, rec, nil)
	before := loadTestConfig(t, m).Connections
	m = pickBothSides(t, m)
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if stub.count() != 2 {
		t.Fatalf("Aufrufe = %v, erwartet beide Fenster", stub.ids())
	}
	for side := 0; side < 2; side++ {
		if m.fm.panels[side].fs == nil {
			t.Errorf("Fenster %d hat kein Dateisystem mehr", side)
		}
	}
	for id, fsys := range rec.created {
		if fsys.closed {
			t.Errorf("Dateisystem von %s wurde nach dem Fehler geschlossen", id)
		}
	}
	assertKeyDeployFailureNotice(t, m)
	if after := loadTestConfig(t, m).Connections; !reflect.DeepEqual(before, after) {
		t.Errorf("Konfiguration trotz Fehlschlag verändert:\nvorher %v\nnachher %v", before, after)
	}
	for _, name := range []string{"Links", "Rechts"} {
		if connByName(t, m, name).KeyPath != "" {
			t.Errorf("%s hat nach dem Fehlschlag einen KeyPath", name)
		}
	}
	if view := stripANSI(m.View()); !strings.Contains(view, m.lang.KeyDeployFailed) {
		t.Error("Fehlermeldung fehlt in der Ansicht")
	}
}

// TestKeyDeployFailureSingle (Fall 7): Wie Fall 6 im Datei-Manager mit einer
// Verbindung (fm.single; Zeile "Datei-Manager (eine Verbindung)").
//
// @date 2026-10-28 13:09
func TestKeyDeployFailureSingle(t *testing.T) {
	stub := installDeployStub(t, evilDeployError)
	rec := newConnectRecorder()
	m := newListTestModel(t, "Server", "Zweit")
	m.fmConnect = rec.connect
	m.termWidth, m.termHeight = 100, 30
	before := loadTestConfig(t, m).Connections
	m = openSinglePick(t, m)
	m = pickSide(t, m, "Server")
	if m.state != ViewFileManager || !m.fm.single {
		t.Fatalf("state = %v, single = %v", m.state, m.fm.single)
	}
	if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
		t.Fatalf("Aufrufe = %v, erwartet %v (nur die gewählte Verbindung)", stub.ids(), want)
	}
	if m.fm.panels[0].fs == nil || rec.created["id-0"].closed {
		t.Error("Dateisystem fehlt oder wurde nach dem Fehler geschlossen")
	}
	assertKeyDeployFailureNotice(t, m)
	if after := loadTestConfig(t, m).Connections; !reflect.DeepEqual(before, after) {
		t.Errorf("Konfiguration trotz Fehlschlag verändert:\nvorher %v\nnachher %v", before, after)
	}
}

// TestKeyDeploySuccessSingle ergänzt Fall 7 um den Erfolg: Auch im Fenster mit
// einer Verbindung wird vergeben, gespeichert und gemeldet.
//
// @date 2026-10-28 13:09
func TestKeyDeploySuccessSingle(t *testing.T) {
	stub := installDeployStub(t, nil)
	rec := newConnectRecorder()
	m := openSingleManagerWith(t, rec.connect)
	if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
		t.Fatalf("Aufrufe = %v, erwartet %v", stub.ids(), want)
	}
	if got := testConnByID(t, loadTestConfig(t, m), "id-0").KeyPath; got != stubKeyPath("id-0") {
		t.Errorf("KeyPath = %q, erwartet %q", got, stubKeyPath("id-0"))
	}
	if got := connByName(t, m, "Server").KeyPath; got != stubKeyPath("id-0") {
		t.Errorf("m.connections nicht neu geladen: %q", got)
	}
	if want := fmt.Sprintf(m.lang.KeyDeployedMsg, stubKeyPath("id-0")); !strings.Contains(m.fm.message, want) {
		t.Errorf("fm.message = %q, erwartet %q", m.fm.message, want)
	}
}

// --- Fall 8: Meldung während der Passwortabfrage ---

// TestKeyDeployNoticeDuringPasswordPrompt (Fall 8): Die Vergabe der linken
// Seite ist fertig, während die rechte Seite ein Passwort verlangt. Die
// Meldung darf die Passwortabfrage nicht stören (dort steht "falsches
// Passwort" o. ä. in fm.message), sondern wartet in fm.keyNotice und erscheint
// erst nach dem Wechsel in die Fensteransicht in fm.message. Dieser Wechsel
// wird hier ohne Folgekommando durchgeführt, damit nicht die Vergabe der
// rechten Seite (nach der Passwort-Anmeldung) die Meldung überschreibt.
//
// @date 2026-10-28 13:09
func TestKeyDeployNoticeDuringPasswordPrompt(t *testing.T) {
	stub := installDeployStub(t, nil)
	rec := newConnectRecorder()
	rec.results["id-1"] = errAuthFailed
	m := newKeyDeployModel(t, rec, nil)
	m = pickBothSides(t, m)
	if m.state != ViewFMPassword || m.fm.passwordSide != 1 {
		t.Fatalf("state = %v/Seite %d, erwartet Passwortabfrage rechts", m.state, m.fm.passwordSide)
	}
	if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
		t.Fatalf("Aufrufe = %v, erwartet %v", stub.ids(), want)
	}
	path := stubKeyPath("id-0")
	if !strings.Contains(m.fm.keyNotice, path) {
		t.Errorf("keyNotice = %q, erwartet die Meldung mit %q", m.fm.keyNotice, path)
	}
	if strings.Contains(m.fm.message, path) {
		t.Errorf("Meldung schon in der Passwortabfrage: %q", m.fm.message)
	}
	if strings.Contains(stripANSI(m.View()), path) {
		t.Error("Meldung der Vergabe erscheint in der Passwortabfrage")
	}
	// Passwort eingeben, Verbindung ohne Folgekommandos durchspielen
	delete(rec.results, "id-1")
	m = typeText(t, m, "geheim")
	updated, cmd := m.Update(fmKey("enter"))
	m = updated.(AppModel)
	if m.state != ViewFMConnecting || cmd == nil {
		t.Fatalf("state = %v, erwartet ViewFMConnecting mit Kommando", m.state)
	}
	updated, _ = m.Update(cmd())
	m = updated.(AppModel)
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if want := fmt.Sprintf(m.lang.KeyDeployedMsg, path); !strings.Contains(m.fm.message, want) {
		t.Errorf("fm.message = %q, erwartet %q", m.fm.message, want)
	}
	if m.fm.keyNotice != "" {
		t.Errorf("keyNotice nach dem Wechsel = %q, erwartet leer", m.fm.keyNotice)
	}
}

// TestKeyDeployMessageInOtherStatesWaits ergänzt Fall 8 direkt über die
// Nachricht: Trifft das Ergebnis in der Passwortabfrage ein, wird die
// Konfiguration trotzdem neu geladen (der Schlüssel liegt ja schon), die
// Meldung aber nur vorgemerkt - und zwei Meldungen werden aneinandergehängt.
//
// @date 2026-10-28 13:09
func TestKeyDeployMessageInOtherStatesWaits(t *testing.T) {
	rec := newConnectRecorder()
	rec.results["id-1"] = errAuthFailed
	m := newKeyDeployModel(t, rec, nil)
	m = pickBothSides(t, m)
	if m.state != ViewFMPassword {
		t.Fatalf("state = %v, erwartet Passwortabfrage", m.state)
	}
	// Schlüssel wie die Vergabe in die Konfiguration schreiben
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_direkt")
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	send := func(keyPath string) {
		updated, cmd := m.Update(fmKeyDeployedMsg{session: m.fm.session, side: 0, connID: "id-0", keyPath: keyPath})
		m = updated.(AppModel)
		if cmd != nil {
			t.Error("handleFMKeyDeployed liefert ein Folgekommando")
		}
	}
	send("~/.ssh/id_direkt")
	if m.state != ViewFMPassword {
		t.Errorf("state = %v, die Meldung darf die Ansicht nicht wechseln", m.state)
	}
	if got := connByName(t, m, "Links").KeyPath; got != "~/.ssh/id_direkt" {
		t.Errorf("Konfiguration nicht neu geladen: %q", got)
	}
	if !strings.Contains(m.fm.keyNotice, "~/.ssh/id_direkt") {
		t.Errorf("keyNotice = %q", m.fm.keyNotice)
	}
	first := m.fm.keyNotice
	send("~/.ssh/id_zweit")
	if !strings.HasPrefix(m.fm.keyNotice, first) || !strings.Contains(m.fm.keyNotice, "id_zweit") {
		t.Errorf("zweite Meldung nicht angehängt: %q", m.fm.keyNotice)
	}
}

// --- Fall 9: Standard-Naht ---

// TestKeyDeployDefaultSeamSkipsWithoutSSH (Fall 9): Ohne SSH-Verbindung unter
// dem Dateisystem (Testfälschung, sftpFS ohne Client, nil) liefert die
// Standard-Naht errFMKeyDeploySkipped - und handleFMKeyDeployed zeigt dazu
// keine Meldung und ändert nichts, auch nicht bei einem eingewickelten Fehler.
//
// @date 2026-10-28 13:09
func TestKeyDeployDefaultSeamSkipsWithoutSSH(t *testing.T) {
	conn := Connection{ID: "id-0", AuthType: AuthPassword}
	for name, fsys := range map[string]remoteFS{
		"fakeRemoteFS":     &fakeRemoteFS{},
		"sftpFS ohne SSH":  &sftpFS{},
		"kein Dateisystem": nil,
	} {
		path, err := fmDeployKeyOverSSH(conn, fsys, "/nirgendwo/connections.json")
		if !errors.Is(err, errFMKeyDeploySkipped) || path != "" {
			t.Errorf("%s: (%q, %v), erwartet errFMKeyDeploySkipped", name, path, err)
		}
	}
	m, _ := openFileManagerFor(t)
	m.fm.message = "vorher"
	for _, err := range []error{errFMKeyDeploySkipped, fmt.Errorf("verpackt: %w", errFMKeyDeploySkipped)} {
		updated, cmd := m.Update(fmKeyDeployedMsg{session: m.fm.session, side: 0, connID: "id-0", err: err})
		got := updated.(AppModel)
		if cmd != nil || got.fm.message != "vorher" || got.fm.keyNotice != "" || got.state != ViewFileManager {
			t.Errorf("Meldung/Zustand bei %v verändert: %q / %q / %v", err, got.fm.message, got.fm.keyNotice, got.state)
		}
	}
}

// TestKeyDeployDefaultSeamEndToEnd: Mit der Standard-Naht (kein Stub) und
// Dateisystemen ohne SSH (fakeRemoteFS) läuft der ganze Ablauf still durch:
// keine Meldung, kein KeyPath, Datei-Manager offen - auch bei Passwort-
// Verbindungen, die eigentlich vergeben würden.
//
// @date 2026-10-28 13:09
func TestKeyDeployDefaultSeamEndToEnd(t *testing.T) {
	previous := fmDeployKey
	t.Cleanup(func() { fmDeployKey = previous })
	fmDeployKey = fmDeployKeyOverSSH
	m, _ := openFileManagerFor(t)
	if m.state != ViewFileManager || m.fm.message != "" || m.fm.keyNotice != "" {
		t.Errorf("state %v, Meldung %q / %q", m.state, m.fm.message, m.fm.keyNotice)
	}
	for _, c := range loadTestConfig(t, m).Connections {
		if c.KeyPath != "" {
			t.Errorf("%s: KeyPath %q ohne SSH-Verbindung", c.ID, c.KeyPath)
		}
	}
}

// --- Fälle 10-11: Ergebnis kommt zu spät / Verbindung weg ---

// TestKeyDeployStaleSession (Fall 10): Der Datei-Manager wird geschlossen
// (oder neu geöffnet), bevor das Ergebnis eintrifft. Bei Erfolg ist die
// Konfigurationsdatei trotzdem geändert und wird neu geladen (m.connections
// hat KeyPath) - eine veraltete Liste würde den Schlüssel überschreiben
// lassen. Die Meldung wandert nicht in die Meldungen des (neuen) Datei-
// Managers (fm.message/fm.keyNotice bleiben leer). Seit der Nachbesserung zu
// Build 86 (F7) geht sie aber auch nicht verloren: Wurde der Datei-Manager nur
// geschlossen (Liste sichtbar), erscheint sie dort - Erfolg in m.successMsg,
// Fehler in m.errorMsg (bereinigt). Vorher stand hier "wird nicht gesetzt".
// Wurde er neu geöffnet (Auswahl sichtbar), ist nur festgelegt, dass nichts in
// die neue Sitzung läuft. Bei einem Fehler ändert sich die Konfiguration nicht.
//
// @date 2026-10-28 13:09
func TestKeyDeployStaleSession(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := fmt.Sprintf("neu geöffnet=%v, Fehler=%v", reopen, fail)
			t.Run(name, func(t *testing.T) {
				m, _ := openSingleManager(t)
				var failure error
				if fail {
					failure = errors.New("serverfehler")
				}
				stub := installDeployStub(t, failure)
				cmd := m.fmDeployKeyCmd(0, m.fm.panels[0].fs, true)
				m = m.closeFileManager()
				if reopen {
					m = openSinglePick(t, m)
				}
				msg, ok := cmd().(fmKeyDeployedMsg)
				if !ok {
					t.Fatal("Kommando liefert keine fmKeyDeployedMsg")
				}
				if stub.count() != 1 {
					t.Fatalf("Aufrufe = %d, erwartet 1", stub.count())
				}
				updated, follow := m.Update(msg)
				m = updated.(AppModel)
				if follow != nil {
					t.Error("Folgekommando bei veralteter Sitzung")
				}
				if m.fm.message != "" || m.fm.keyNotice != "" {
					t.Errorf("Meldung aus alter Sitzung im Datei-Manager: %q / %q", m.fm.message, m.fm.keyNotice)
				}
				got := connByName(t, m, "Server").KeyPath
				if fail && got != "" {
					t.Errorf("KeyPath nach Fehlschlag = %q", got)
				}
				if !fail && got != stubKeyPath("id-0") {
					t.Errorf("Konfiguration nicht neu geladen: KeyPath %q, erwartet %q", got, stubKeyPath("id-0"))
				}
				if reopen {
					return
				}
				// Datei-Manager nur geschlossen: Meldung in der Verbindungsliste
				if m.state != ViewList {
					t.Fatalf("state = %v, erwartet ViewList", m.state)
				}
				if fail {
					if !strings.HasPrefix(m.errorMsg, m.lang.KeyDeployFailed) || !strings.Contains(m.errorMsg, "serverfehler") {
						t.Errorf("errorMsg = %q, erwartet %q + Fehlertext", m.errorMsg, m.lang.KeyDeployFailed)
					}
					if m.successMsg != "" {
						t.Errorf("successMsg = %q bei Fehler, erwartet leer", m.successMsg)
					}
				} else {
					if want := fmt.Sprintf(m.lang.KeyDeployedMsg, stubKeyPath("id-0")); !strings.Contains(m.successMsg, want) {
						t.Errorf("successMsg = %q, erwartet %q", m.successMsg, want)
					}
					if m.errorMsg != "" {
						t.Errorf("errorMsg = %q bei Erfolg, erwartet leer", m.errorMsg)
					}
				}
				if view := stripANSI(m.View()); !strings.Contains(view, "id_test_id-0") && !fail {
					t.Errorf("Erfolgsmeldung fehlt in der Listenansicht:\n%s", view)
				}
			})
		}
	}
}

// TestKeyDeploySkippedWhenConnectionChanged (Fall 11): Zwischen Anmeldung und
// Vergabe ändert sich die Konfiguration - Verbindung gelöscht, Datei
// unlesbar oder die Verbindung hat inzwischen einen Schlüssel (erneute
// Prüfung der Regel unter fmDeployMu, ohne Passwort). Dann gibt es keinen
// Aufruf und keine Meldung. Nach dem Löschen darf auch nichts wieder
// auftauchen (kein Zurückschreiben einer Kopie).
//
// @date 2026-10-28 13:09
func TestKeyDeploySkippedWhenConnectionChanged(t *testing.T) {
	cases := []struct {
		name         string
		change       func(t *testing.T, m AppModel)
		withPassword bool
	}{
		{"gelöscht", func(t *testing.T, m AppModel) {
			if err := DeleteConnection(m.configPath, "id-0"); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"Konfiguration unlesbar", func(t *testing.T, m AppModel) {
			if err := os.WriteFile(m.configPath, []byte("{kaputt"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"inzwischen Schlüssel, ohne Passwort", func(t *testing.T, m AppModel) {
			err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
				setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_inzwischen")
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := openSingleManager(t)
			stub := installDeployStub(t, nil)
			cmd := m.fmDeployKeyCmd(0, m.fm.panels[0].fs, tc.withPassword)
			tc.change(t, m)
			msg, ok := cmd().(fmKeyDeployedMsg)
			if !ok {
				t.Fatal("Kommando liefert keine fmKeyDeployedMsg")
			}
			if !errors.Is(msg.err, errFMKeyDeploySkipped) {
				t.Errorf("err = %v, erwartet errFMKeyDeploySkipped", msg.err)
			}
			if stub.count() != 0 {
				t.Errorf("Vergabe trotz geänderter Konfiguration: %v", stub.ids())
			}
			updated, _ := m.Update(msg)
			m = updated.(AppModel)
			if m.fm.message != "" || m.fm.keyNotice != "" || m.state != ViewFileManager {
				t.Errorf("Meldung/Zustand verändert: %q / %q / %v", m.fm.message, m.fm.keyNotice, m.state)
			}
			if tc.name == "gelöscht" {
				for _, c := range loadTestConfig(t, m).Connections {
					if c.ID == "id-0" {
						t.Error("gelöschte Verbindung ist wieder in der Konfiguration")
					}
				}
			}
		})
	}
}

// --- Fälle 12-13: Gleichzeitigkeit ---

// TestKeyDeployRunsOneAtATime (Fall 12): Zwei fmDeployKeyCmd laufen parallel
// (beide Fenster melden sich per Passwort an, etwa an einer Verbindung und
// ihrer Kopie). Die Vergaben dürfen nie überlappen (fmDeployMu): Sonst
// sähen beide denselben Konfigurationsstand und wählten denselben
// Schlüsselnamen. Der Stub zählt Überlappungen unter einem eigenen Mutex und
// wartet im Aufruf, damit sie sicher auffielen. Auch mit -race laufen.
//
// @date 2026-10-28 13:09
func TestKeyDeployRunsOneAtATime(t *testing.T) {
	m, _ := openFileManagerFor(t)
	stub := installDeployStub(t, nil)
	stub.delay = 40 * time.Millisecond
	cmds := []func() any{
		func() any { return m.fmDeployKeyCmd(0, m.fm.panels[0].fs, true)() },
		func() any { return m.fmDeployKeyCmd(1, m.fm.panels[1].fs, true)() },
	}
	results := make([]any, len(cmds))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, run := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = run()
		}()
	}
	close(start)
	wg.Wait()
	if stub.count() != 2 {
		t.Fatalf("Aufrufe = %v, erwartet beide", stub.ids())
	}
	if stub.maxActive != 1 {
		t.Errorf("%d Vergaben liefen gleichzeitig, erwartet nie mehr als 1", stub.maxActive)
	}
	// Jedes Ergebnis gehört zu genau einer Verbindung (beide kommen vor) und
	// trägt deren Schlüsselpfad. Vorher prüfte eine Bedingung "zwei
	// verschiedene Pfade", die immer wahr war, weil stubKeyPath von der ID
	// abhängt. Aussagekräftig ist, dass BEIDE Schlüssel in der Konfiguration
	// stehen: Läse die zweite Vergabe einen veralteten Stand, überschriebe sie
	// den Eintrag der ersten (fmDeployMu + frischer Eintrag verhindern das).
	seen := map[string]bool{}
	for _, result := range results {
		msg, ok := result.(fmKeyDeployedMsg)
		if !ok || msg.err != nil {
			t.Fatalf("Ergebnis %#v, erwartet Erfolg", result)
		}
		if msg.keyPath != stubKeyPath(msg.connID) {
			t.Errorf("Ergebnis für %s trägt den Pfad %q, erwartet %q", msg.connID, msg.keyPath, stubKeyPath(msg.connID))
		}
		seen[msg.connID] = true
	}
	if !seen["id-0"] || !seen["id-1"] {
		t.Errorf("Ergebnisse für %v, erwartet id-0 und id-1", seen)
	}
	cfg := loadTestConfig(t, m)
	for _, id := range []string{"id-0", "id-1"} {
		if got := testConnByID(t, cfg, id).KeyPath; got != stubKeyPath(id) {
			t.Errorf("%s: KeyPath in der Konfiguration = %q, erwartet %q (von der anderen Vergabe überschrieben?)", id, got, stubKeyPath(id))
		}
	}
}

// TestKeyDeployAndRememberPathTogether (Fall 13): rememberFMPath (nach jedem
// Einlesen) und die Vergabe im Hintergrund schreiben gleichzeitig in dieselbe
// Konfiguration. Ohne gemeinsame Sperre (updateConfig) überschreibt eines den
// Stand des anderen - dann fehlte KeyPath (Schlüssel auf dem Server, aber
// unbekannt) oder der gemerkte Pfad. Mehrere Durchläufe erhöhen die
// Trefferchance; mit -race laufen.
//
// @date 2026-10-28 13:09
func TestKeyDeployAndRememberPathTogether(t *testing.T) {
	const rounds = 25
	base := fmDeployKey
	t.Cleanup(func() { fmDeployKey = base })
	for i := 0; i < rounds; i++ {
		// Die Fenster öffnen mit der Standard-Naht (still übergangen), erst
		// danach kommt der Stub - sonst wäre der Schlüssel schon vergeben
		fmDeployKey = base
		m, _ := openFileManagerFor(t)
		stub := installDeployStub(t, nil)
		want := fmt.Sprintf("/home/kurt/runde%d", i)
		m.fm.panels[0].path = want
		cmd := m.fmDeployKeyCmd(0, m.fm.panels[0].fs, true)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cmd()
		}()
		go func() {
			defer wg.Done()
			m.rememberFMPath(0)
		}()
		wg.Wait()
		if stub.count() != 1 {
			t.Fatalf("Runde %d: Aufrufe = %v", i, stub.ids())
		}
		cfg := loadTestConfig(t, m)
		if got := testConnByID(t, cfg, "id-0").KeyPath; got != stubKeyPath("id-0") {
			t.Fatalf("Runde %d: KeyPath = %q, erwartet %q (von rememberFMPath überschrieben?)", i, got, stubKeyPath("id-0"))
		}
		if got := cfg.FileManagerLastPaths["id-0"]; got != want {
			t.Fatalf("Runde %d: gemerkter Pfad = %q, erwartet %q (von der Vergabe überschrieben?)", i, got, want)
		}
	}
}

// --- Nachbesserung Build 86 (Fable-Review): F5b und F7 im Datei-Manager ---

// deployedNotice liefert den Erfolgstext der Schlüsselvergabe für einen Pfad,
// so wie ihn das Modell anzeigt.
//
// @param m    - Modell (Sprache)
// @param path - Schlüsselpfad
// @return string - Meldungstext
// @date 2026-10-28 13:09
func deployedNotice(m AppModel, path string) string {
	return fmt.Sprintf(m.lang.KeyDeployedMsg, path)
}

// TestKeyDeployNoticeKeepsUnreadableStartDirMessage (F7): Das Startverzeichnis
// ist nicht lesbar, deshalb steht beim Öffnen schon eine Meldung (kein Zugriff)
// in fm.message. Kommt danach das Ergebnis der Schlüsselvergabe, darf es diese
// Meldung nicht ÜBERSCHREIBEN: Beide Texte stehen in fm.message (angehängt über
// joinFMNotice). Rot heute: handleFMKeyDeployed setzt fm.message = notice.
//
// @date 2026-10-28 13:09
func TestKeyDeployNoticeKeepsUnreadableStartDirMessage(t *testing.T) {
	installDeployStub(t, nil)
	connect := func(conn Connection, password string) (remoteFS, error) {
		fsys := newFakeFS()
		fsys.denied[fsys.home] = true // Startverzeichnis nicht lesbar
		return fsys, nil
	}
	m := openSingleManagerWith(t, connect)
	dirMsg := fmt.Sprintf(m.tr(txtFMErrPermission), "/home/kurt")
	if !strings.Contains(m.fm.message, dirMsg) {
		t.Errorf("fm.message = %q, erwartet die Meldung zum Startverzeichnis %q", m.fm.message, dirMsg)
	}
	if want := deployedNotice(m, stubKeyPath("id-0")); !strings.Contains(m.fm.message, want) {
		t.Errorf("fm.message = %q, erwartet zusätzlich %q", m.fm.message, want)
	}
}

// TestKeyDeployNoticeAppendsToExistingMessage (F7): Direkt über die Nachricht
// in der Fensteransicht. Eine vorhandene Meldung (hier von einer anderen
// Aktion) bleibt vorn stehen, die Schlüsselmeldung wird mit zwei Leerzeichen
// angehängt - bei Erfolg wie bei Fehler (Fehlertext bereinigt). Ohne
// vorhandene Meldung steht nur die Schlüsselmeldung da, ohne führende
// Leerzeichen. Rot heute: Überschreiben.
//
// @date 2026-10-28 13:09
func TestKeyDeployNoticeAppendsToExistingMessage(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		err      error
	}{
		{"Erfolg, mit Meldung", "Löschen nicht möglich: x", nil},
		{"Fehler, mit Meldung", "Löschen nicht möglich: x", evilDeployError},
		{"Erfolg, ohne Meldung", "", nil},
		{"Fehler, ohne Meldung", "", evilDeployError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := openSingleManager(t)
			m.fm.message = tc.existing
			updated, _ := m.Update(fmKeyDeployedMsg{session: m.fm.session, side: 0, connID: "id-0", keyPath: "~/.ssh/id_neu", err: tc.err})
			m = updated.(AppModel)
			if m.state != ViewFileManager {
				t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
			}
			var notice string
			if tc.err != nil {
				if !strings.HasPrefix(m.fm.message, tc.existing) || !strings.Contains(m.fm.message, m.lang.KeyDeployFailed) ||
					!strings.Contains(m.fm.message, "serverfehler") {
					t.Errorf("fm.message = %q, erwartet %q + %q + Fehlertext", m.fm.message, tc.existing, m.lang.KeyDeployFailed)
				}
				notice = m.lang.KeyDeployFailed
			} else {
				notice = deployedNotice(m, "~/.ssh/id_neu")
				if !strings.HasPrefix(m.fm.message, tc.existing) || !strings.HasSuffix(m.fm.message, notice) {
					t.Errorf("fm.message = %q, erwartet %q vorn und %q hinten", m.fm.message, tc.existing, notice)
				}
			}
			if tc.existing != "" && !strings.Contains(m.fm.message, tc.existing) {
				t.Errorf("vorhandene Meldung %q ging verloren: %q", tc.existing, m.fm.message)
			}
			if tc.existing == "" && !strings.HasPrefix(m.fm.message, notice) {
				t.Errorf("fm.message = %q, erwartet nur die Schlüsselmeldung (%q) ohne Vorspann", m.fm.message, notice)
			}
			if !noControlRunes(m.fm.message) {
				t.Errorf("Steuerzeichen in der Meldung: %q", m.fm.message)
			}
		})
	}
}

// TestKeyDeployEscInPasswordPromptKeepsNotice (F7): Die Vergabe der linken
// Seite ist fertig (Meldung wartet in fm.keyNotice), die rechte fragt nach dem
// Passwort, und der Nutzer bricht mit Esc ab. Der Datei-Manager schließt, die
// Meldung geht nicht verloren, sondern erscheint in der Verbindungsliste:
// Erfolg in m.successMsg, Fehler in m.errorMsg (ohne Steuerzeichen). fm.keyNotice
// ist danach leer, die andere Meldungsart bleibt leer. Rot heute: Beide Felder
// bleiben leer, die Meldung verschwindet mit dem Datei-Manager.
//
// @date 2026-10-28 13:09
func TestKeyDeployEscInPasswordPromptKeepsNotice(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("Fehler=%v", fail), func(t *testing.T) {
			var failure error
			if fail {
				failure = evilDeployError
			}
			installDeployStub(t, failure)
			rec := newConnectRecorder()
			rec.results["id-1"] = errAuthFailed
			m := newKeyDeployModel(t, rec, nil)
			m = pickBothSides(t, m)
			if m.state != ViewFMPassword || m.fm.keyNotice == "" {
				t.Fatalf("state = %v, keyNotice = %q; erwartet Passwortabfrage mit wartender Meldung", m.state, m.fm.keyNotice)
			}
			updated, _ := m.Update(fmKey("esc"))
			m = updated.(AppModel)
			if m.state != ViewList {
				t.Fatalf("state = %v, erwartet ViewList", m.state)
			}
			if m.fm.keyNotice != "" {
				t.Errorf("keyNotice nach dem Schließen = %q, erwartet leer", m.fm.keyNotice)
			}
			if fail {
				if !strings.Contains(m.errorMsg, m.lang.KeyDeployFailed) || !strings.Contains(m.errorMsg, "serverfehler") {
					t.Errorf("errorMsg = %q, erwartet %q + Fehlertext", m.errorMsg, m.lang.KeyDeployFailed)
				}
				if !noControlRunes(m.errorMsg) {
					t.Errorf("Steuerzeichen in errorMsg: %q", m.errorMsg)
				}
				if m.successMsg != "" {
					t.Errorf("successMsg = %q bei Fehler, erwartet leer", m.successMsg)
				}
				return
			}
			want := deployedNotice(m, stubKeyPath("id-0"))
			if !strings.Contains(m.successMsg, want) {
				t.Errorf("successMsg = %q, erwartet %q", m.successMsg, want)
			}
			if m.errorMsg != "" {
				t.Errorf("errorMsg = %q bei Erfolg, erwartet leer", m.errorMsg)
			}
			if view := stripANSI(m.View()); !strings.Contains(view, want) {
				t.Errorf("Meldung fehlt in der Listenansicht:\n%s", view)
			}
		})
	}
}

// TestKeyDeployNetworkErrorRightKeepsNotice (F7): Wie oben, aber die rechte
// Seite scheitert mit einem Netzwerkfehler. Erfolg: Die Liste zeigt den
// Verbindungsfehler (errorMsg nennt "Rechts") UND die Erfolgsmeldung der
// linken Schlüsselvergabe (successMsg). Fehler (Klarstellung des Auftrags):
// Schlüsselmeldung und Verbindungsfehler werden zusammengeführt, BEIDE stehen
// in errorMsg (Verbindungsfehler und KeyDeployFailed samt Fehlertext, ohne
// Steuerzeichen); successMsg bleibt leer. Rot heute: successMsg bleibt leer
// bzw. die Schlüsselfehlermeldung fehlt in errorMsg.
//
// @date 2026-10-28 14:00
func TestKeyDeployNetworkErrorRightKeepsNotice(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("Fehler=%v", fail), func(t *testing.T) {
			var failure error
			if fail {
				failure = evilDeployError
			}
			installDeployStub(t, failure)
			rec := newConnectRecorder()
			rec.results["id-1"] = &networkError{err: errors.New("dial tcp 10.0.0.1:22: i/o timeout")}
			m := newKeyDeployModel(t, rec, nil)
			m = pickBothSides(t, m)
			if m.state != ViewList {
				t.Fatalf("state = %v, erwartet ViewList", m.state)
			}
			if !strings.Contains(m.errorMsg, "Rechts") {
				t.Errorf("errorMsg = %q, erwartet den Verbindungsfehler zu Rechts", m.errorMsg)
			}
			if m.fm.keyNotice != "" {
				t.Errorf("keyNotice = %q, erwartet leer", m.fm.keyNotice)
			}
			if fail {
				if !strings.Contains(m.errorMsg, m.lang.KeyDeployFailed) || !strings.Contains(m.errorMsg, "serverfehler") {
					t.Errorf("errorMsg = %q, erwartet zusätzlich %q + Fehlertext", m.errorMsg, m.lang.KeyDeployFailed)
				}
				if !noControlRunes(m.errorMsg) {
					t.Errorf("Steuerzeichen in errorMsg: %q", m.errorMsg)
				}
				if m.successMsg != "" {
					t.Errorf("successMsg = %q bei Fehler, erwartet leer", m.successMsg)
				}
				return
			}
			if want := deployedNotice(m, stubKeyPath("id-0")); !strings.Contains(m.successMsg, want) {
				t.Errorf("successMsg = %q, erwartet %q", m.successMsg, want)
			}
		})
	}
}

// TestKeyDeployStaleSkippedShowsNothing (F7, Randfall): Das Ergebnis "still
// übergangen" (errFMKeyDeploySkipped) trifft erst nach dem Schließen des
// Datei-Managers ein. Es gibt keine Meldung, weder in der Liste noch im
// Datei-Manager. Läuft heute grün (Schutz: die neue Weitergabe in die Liste
// darf übergangene Vergaben nicht melden).
//
// @date 2026-10-28 13:09
func TestKeyDeployStaleSkippedShowsNothing(t *testing.T) {
	m, _ := openSingleManager(t)
	session := m.fm.session
	m = m.closeFileManager()
	updated, cmd := m.Update(fmKeyDeployedMsg{session: session, side: 0, connID: "id-0", err: errFMKeyDeploySkipped})
	m = updated.(AppModel)
	if cmd != nil || m.successMsg != "" || m.errorMsg != "" || m.fm.message != "" || m.fm.keyNotice != "" {
		t.Errorf("Meldung trotz Übergehen: success=%q error=%q fm=%q/%q", m.successMsg, m.errorMsg, m.fm.message, m.fm.keyNotice)
	}
}

// authCacheFMModel öffnet den Datei-Manager mit einer Verbindung ("id-0")
// und füllt den Auth-Cache für "id-0" und eine fremde ID mit "agent". HOME
// zeigt vorher auf ein Temp-Verzeichnis, damit nie die echte
// ~/.ssh-easy/auth-cache.json beschrieben wird.
//
// @param t - Testkontext
// @return AppModel - Modell im Datei-Manager
// @date 2026-10-28 13:09
func authCacheFMModel(t *testing.T) AppModel {
	t.Helper()
	setTestHome(t)
	m, _ := openSingleManager(t)
	m.sshManager.authCache.RecordSuccess("id-0", authCacheMethodAgent)
	m.sshManager.authCache.RecordSuccess("id-fremd", authCacheMethodAgent)
	return m
}

// TestKeyDeployFMClearsAuthCacheOnSuccessOnly (F5b, Datei-Manager): Nach
// erfolgreicher Vergabe (handleFMKeyDeployed) ist der Auth-Cache-Eintrag der
// Verbindung weg, damit die nächste Anmeldung den neuen Schlüssel probiert.
// Das gilt auch, wenn die Sitzung veraltet ist, denn der Schlüssel liegt ja
// auf dem Server. Bei Fehler und bei "übergangen" bleibt der Eintrag
// unverändert. Der Eintrag einer anderen Verbindung bleibt immer. Rot heute:
// In keinem Erfolgsfall wird der Cache geleert.
//
// @date 2026-10-28 13:09
func TestKeyDeployFMClearsAuthCacheOnSuccessOnly(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		staleness int // Abweichung der Sitzungsnummer (-1 = veraltet)
		cleared   bool
	}{
		{"Erfolg", nil, 0, true},
		{"Erfolg, Sitzung veraltet", nil, -1, true},
		{"Fehler", errors.New("serverfehler"), 0, false},
		{"Fehler, Sitzung veraltet", errors.New("serverfehler"), -1, false},
		{"übergangen", errFMKeyDeploySkipped, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := authCacheFMModel(t)
			msg := fmKeyDeployedMsg{session: m.fm.session + tc.staleness, side: 0, connID: "id-0", keyPath: "~/.ssh/id_neu", err: tc.err}
			updated, _ := m.Update(msg)
			m = updated.(AppModel)
			entry := m.sshManager.authCache.Get("id-0")
			if tc.cleared && entry != nil {
				t.Errorf("Auth-Cache von id-0 = %+v, erwartet leer", entry)
			}
			if !tc.cleared && (entry == nil || entry.Method != authCacheMethodAgent || entry.FailureCount != 0) {
				t.Errorf("Auth-Cache von id-0 = %+v, erwartet unverändert (agent)", entry)
			}
			if other := m.sshManager.authCache.Get("id-fremd"); other == nil || other.Method != authCacheMethodAgent {
				t.Errorf("Eintrag einer anderen Verbindung verändert: %+v", other)
			}
		})
	}
}

// --- Merken und aussetzen: AutoKeyOff / AutoKeyFailures (Nutzerentscheidung Build 86) ---

// TestFMNeedsKeyDeployAutoKeyFields: Die Regel fmNeedsKeyDeploy mit den neuen
// Feldern. Ohne Passwort vergibt nur eine Verbindung ohne Aussetzen
// (AutoKeyOff aus, weniger als autoKeyMaxFailures Fehlversuche); mit Passwort
// wird IMMER vergeben (auch bei Off und erreichter Grenze), nur der Lokale PC
// nie.
//
// @date 2026-10-28 14:00
func TestFMNeedsKeyDeployAutoKeyFields(t *testing.T) {
	cases := []struct {
		name         string
		off          bool
		failures     int
		withPassword bool
		want         bool
	}{
		{"Grundfall, Auto-Auth", false, 0, false, true},
		{"Off, Auto-Auth", true, 0, false, false},
		{"Off, mit Passwort", true, 0, true, true},
		{"1 Fehler, Auto-Auth", false, 1, false, true},
		{"Grenze, Auto-Auth", false, autoKeyMaxFailures, false, false},
		{"Grenze, mit Passwort", false, autoKeyMaxFailures, true, true},
		{"über der Grenze, mit Passwort", true, autoKeyMaxFailures + 1, true, true},
	}
	for _, tc := range cases {
		conn := Connection{ID: "x", AuthType: AuthPassword, AutoKeyOff: tc.off, AutoKeyFailures: tc.failures}
		if got := fmNeedsKeyDeploy(conn, tc.withPassword); got != tc.want {
			t.Errorf("%s: fmNeedsKeyDeploy = %v, erwartet %v", tc.name, got, tc.want)
		}
	}
	local := Connection{ID: fmLocalConnID, AuthType: AuthPassword}
	if fmNeedsKeyDeploy(local, true) || fmNeedsKeyDeploy(local, false) {
		t.Error("Lokaler PC bekommt einen Schlüssel")
	}
}

// TestKeyDeploySuspendedAutoAuthTwoWindows: Beide Seiten melden sich ohne
// Passwort an. Die linke Verbindung hat AutoKeyOff, die rechte hat
// entweder die Fehlergrenze erreicht (dann gar keine Vergabe) oder erst einen
// Fehlversuch (dann vergibt nur die rechte). Bei ausgesetzter Vergabe keine
// Meldung und keine Änderung der Konfiguration.
//
// @date 2026-10-28 14:00
func TestKeyDeploySuspendedAutoAuthTwoWindows(t *testing.T) {
	cases := []struct {
		name          string
		rightFailures int
		wantIDs       []string
	}{
		{"beide ausgesetzt", autoKeyMaxFailures, nil},
		{"rechts noch ein Versuch", 1, []string{"id-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := installDeployStub(t, nil)
			rec := newConnectRecorder()
			m := newKeyDeployModel(t, rec, func(cfg *AppConfig) {
				cfg.Connections[0].AutoKeyOff = true
				cfg.Connections[1].AutoKeyFailures = tc.rightFailures
			})
			m = pickBothSides(t, m)
			if m.state != ViewFileManager {
				t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
			}
			if got := stub.ids(); !reflect.DeepEqual(got, tc.wantIDs) && !(len(got) == 0 && len(tc.wantIDs) == 0) {
				t.Errorf("Aufrufe = %v, erwartet %v", got, tc.wantIDs)
			}
			if len(tc.wantIDs) == 0 {
				if m.fm.message != "" || m.fm.keyNotice != "" {
					t.Errorf("Meldung ohne Vergabe: %q / %q", m.fm.message, m.fm.keyNotice)
				}
				for _, c := range loadTestConfig(t, m).Connections {
					if c.KeyPath != "" {
						t.Errorf("%s: KeyPath %q trotz Aussetzen", c.ID, c.KeyPath)
					}
				}
			}
			cfg := loadTestConfig(t, m)
			if !testConnByID(t, cfg, "id-0").AutoKeyOff {
				t.Error("AutoKeyOff der linken Verbindung ging verloren")
			}
		})
	}
}

// TestKeyDeployPasswordLoginIgnoresSuspension: Auch bei AutoKeyOff und bei
// erreichter Fehlergrenze vergibt die Anmeldung MIT Passwort (links, zwei
// Fenster; rechts ist Agent). Der Stub bekommt den frischen Eintrag samt der
// beiden Felder. Bei Erfolg ist die Vergabe wieder eingeschaltet (hier durch
// den Stub nicht abgebildet; das echte AutoDeployKey setzt zurück, siehe
// key_deploy_server_test.go).
//
// @date 2026-10-28 14:00
func TestKeyDeployPasswordLoginIgnoresSuspension(t *testing.T) {
	cases := []struct {
		name     string
		off      bool
		failures int
	}{
		{"AutoKeyOff", true, 0},
		{"Fehlergrenze", false, autoKeyMaxFailures},
		{"beides", true, autoKeyMaxFailures + 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := installDeployStub(t, nil)
			rec := newConnectRecorder()
			rec.results["id-0"] = errAuthFailed
			m := newKeyDeployModel(t, rec, func(cfg *AppConfig) {
				cfg.Connections[0].AutoKeyOff = tc.off
				cfg.Connections[0].AutoKeyFailures = tc.failures
				setTestAuth(t, cfg, "id-1", AuthAgent, "")
			})
			m = pickBothSides(t, m)
			if m.state != ViewFMPassword {
				t.Fatalf("state = %v, erwartet Passwortabfrage", m.state)
			}
			delete(rec.results, "id-0")
			m = typeText(t, m, "geheim")
			m = pressRun(t, m, "enter")
			if m.state != ViewFileManager {
				t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
			}
			if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
				t.Fatalf("Aufrufe = %v, erwartet %v", stub.ids(), want)
			}
			if got := stub.calls[0]; got.AutoKeyOff != tc.off || got.AutoKeyFailures != tc.failures {
				t.Errorf("Stub bekam AutoKeyOff=%v AutoKeyFailures=%d, erwartet %v und %d", got.AutoKeyOff, got.AutoKeyFailures, tc.off, tc.failures)
			}
		})
	}
}

// TestKeyDeploySuspendedSingle: Wie oben im Fenster mit einer Verbindung
// ("Server" = id-0): Auto-Auth bei AutoKeyOff vergibt nicht (keine Meldung),
// die Anmeldung mit Passwort vergibt.
//
// @date 2026-10-28 14:00
func TestKeyDeploySuspendedSingle(t *testing.T) {
	for _, withPassword := range []bool{false, true} {
		t.Run(fmt.Sprintf("Passwort=%v", withPassword), func(t *testing.T) {
			stub := installDeployStub(t, nil)
			rec := newConnectRecorder()
			if withPassword {
				rec.results["id-0"] = errAuthFailed
			}
			m := newListTestModel(t, "Server", "Zweit")
			m = editTestConfig(t, m, func(cfg *AppConfig) { cfg.Connections[0].AutoKeyOff = true })
			m.fmConnect = rec.connect
			m.termWidth, m.termHeight = 100, 30
			m = openSinglePick(t, m)
			m = pickSide(t, m, "Server")
			if withPassword {
				if m.state != ViewFMPassword {
					t.Fatalf("state = %v, erwartet Passwortabfrage", m.state)
				}
				delete(rec.results, "id-0")
				m = typeText(t, m, "geheim")
				m = pressRun(t, m, "enter")
			}
			if m.state != ViewFileManager {
				t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
			}
			if withPassword {
				if want := []string{"id-0"}; !reflect.DeepEqual(stub.ids(), want) {
					t.Errorf("Aufrufe = %v, erwartet %v", stub.ids(), want)
				}
			} else {
				if stub.count() != 0 {
					t.Errorf("Aufrufe = %v, erwartet keine", stub.ids())
				}
				if m.fm.message != "" || m.fm.keyNotice != "" {
					t.Errorf("Meldung ohne Vergabe: %q / %q", m.fm.message, m.fm.keyNotice)
				}
			}
		})
	}
}

// TestKeyDeployCmdRechecksSuspension: Zwischen Anmeldung und Vergabe wird die
// Vergabe ausgesetzt (z. B. der Nutzer entfernt den Schlüssel im anderen
// Fenster, oder eine parallele Vergabe scheitert zum zweiten Mal). Die
// erneute Prüfung unter fmDeployMu liest den frischen Eintrag: Ohne Passwort
// still übergangen (errFMKeyDeploySkipped, kein Stub-Aufruf), mit Passwort
// wird trotzdem vergeben.
//
// @date 2026-10-28 14:00
func TestKeyDeployCmdRechecksSuspension(t *testing.T) {
	cases := []struct {
		name   string
		change func(cfg *AppConfig)
	}{
		{"AutoKeyOff", func(cfg *AppConfig) { cfg.Connections[0].AutoKeyOff = true }},
		{"Fehlergrenze", func(cfg *AppConfig) { cfg.Connections[0].AutoKeyFailures = autoKeyMaxFailures }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := openSingleManager(t)
			stub := installDeployStub(t, nil)
			if err := updateConfig(m.configPath, func(cfg *AppConfig) bool { tc.change(cfg); return true }); err != nil {
				t.Fatal(err)
			}
			msg, ok := m.fmDeployKeyCmd(0, m.fm.panels[0].fs, false)().(fmKeyDeployedMsg)
			if !ok || !errors.Is(msg.err, errFMKeyDeploySkipped) || stub.count() != 0 {
				t.Fatalf("ohne Passwort: Ergebnis %+v, Aufrufe %v; erwartet übergangen und keine Aufrufe", msg, stub.ids())
			}
			msg, ok = m.fmDeployKeyCmd(0, m.fm.panels[0].fs, true)().(fmKeyDeployedMsg)
			if !ok || msg.err != nil || stub.count() != 1 {
				t.Errorf("mit Passwort: Ergebnis %+v, Aufrufe %v; erwartet Vergabe", msg, stub.ids())
			}
		})
	}
}

// --- Fable-Review Runde 2 ---

// installRealFMDeploy ersetzt fmDeployKey durch eine Weiterleitung an die
// ECHTE AutoDeployKey mit dem Client eines Testservers (die Fenster des Tests
// haben sonst keine SSH-Verbindung). Gezählt wird so wie in der echten
// Vergabe (updateConfig), der Fehler kommt vom Server. Die vorherige Funktion
// wird per t.Cleanup wiederhergestellt.
//
// @param t      - Testkontext
// @param client - Client des Testservers
// @return *int - Zeiger auf die Zahl der Aufrufe
// @date 2026-10-30 09:00
func installRealFMDeploy(t *testing.T, client *ssh.Client) *int {
	t.Helper()
	calls := new(int)
	previous := fmDeployKey
	t.Cleanup(func() { fmDeployKey = previous })
	fmDeployKey = func(conn Connection, fsys remoteFS, configPath string) (string, error) {
		*calls++
		return AutoDeployKey(conn, client, configPath)
	}
	return calls
}

// TestKeyDeployFMFailureReloadsConnections (H1, Datei-Manager): Nach einem
// Fehlversuch (echte AutoDeployKey zählt per updateConfig, Server liefert
// Exit 1) enthält m.connections sofort AutoKeyFailures 1, ohne Neuladen von
// Hand - sonst entscheidet die Oberfläche weiter nach dem veralteten Stand, und
// ein Speichern mit e schriebe die 0 zurück. Zweiter Versuch: 2. Dritter
// Öffnen: kein Aufruf mehr, keine Meldung. Rot heute: m.connections bleibt bei
// 0, weil der Fehlerzweig von handleFMKeyDeployed nicht neu lädt.
//
// @date 2026-10-30 09:00
func TestKeyDeployFMFailureReloadsConnections(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	calls := installRealFMDeploy(t, client)
	m := newListTestModel(t, "Server", "Zweit")
	m.fmConnect = newConnectRecorder().connect
	m.termWidth, m.termHeight = 100, 30

	for attempt := 1; attempt <= autoKeyMaxFailures; attempt++ {
		m = openSinglePick(t, m)
		m = pickSide(t, m, "Server")
		if m.state != ViewFileManager {
			t.Fatalf("Versuch %d: state = %v, erwartet ViewFileManager", attempt, m.state)
		}
		if *calls != attempt {
			t.Fatalf("Versuch %d: %d Aufrufe, erwartet %d", attempt, *calls, attempt)
		}
		if !strings.Contains(m.fm.message, m.lang.KeyDeployFailed) {
			t.Errorf("Versuch %d: fm.message = %q, erwartet die Fehlermeldung", attempt, m.fm.message)
		}
		if got := connByName(t, m, "Server").AutoKeyFailures; got != attempt {
			t.Fatalf("Versuch %d: m.connections hat AutoKeyFailures=%d, erwartet %d (Liste nicht neu geladen?)", attempt, got, attempt)
		}
		if got := testConnByID(t, loadTestConfig(t, m), "id-0").AutoKeyFailures; got != attempt {
			t.Fatalf("Versuch %d: Konfiguration hat AutoKeyFailures=%d, erwartet %d", attempt, got, attempt)
		}
		m = pressRun(t, m, "f10")
		if m.state != ViewList {
			t.Fatalf("Versuch %d: state = %v nach F10, erwartet ViewList", attempt, m.state)
		}
	}
	m = openSinglePick(t, m)
	m = pickSide(t, m, "Server")
	if *calls != autoKeyMaxFailures {
		t.Errorf("%d Aufrufe, erwartet %d (dritte Anmeldung darf nicht mehr vergeben)", *calls, autoKeyMaxFailures)
	}
	if m.fm.message != "" || m.fm.keyNotice != "" {
		t.Errorf("Meldung ohne Vergabe: %q / %q", m.fm.message, m.fm.keyNotice)
	}
}

// TestKeyDeployFMFailureReloadsAfterClose (H1, Randfall): Das Ergebnis des
// Fehlversuchs kommt erst nach dem Schließen des Datei-Managers. Die
// Konfiguration hat sich trotzdem geändert (Zähler), also muss m.connections
// auch dann neu geladen werden - die Liste und ein späteres e arbeiten sonst
// mit dem alten Zähler.
//
// @date 2026-10-30 09:00
func TestKeyDeployFMFailureReloadsAfterClose(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	m, _ := openSingleManager(t) // Standard-Naht: übergangen, noch kein Zähler
	installRealFMDeploy(t, client)
	cmd := m.fmDeployKeyCmd(0, m.fm.panels[0].fs, true)
	m = m.closeFileManager()
	msg, ok := cmd().(fmKeyDeployedMsg)
	if !ok || msg.err == nil {
		t.Fatalf("Ergebnis %#v, erwartet Fehler", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(AppModel)
	if got := connByName(t, m, "Server").AutoKeyFailures; got != 1 {
		t.Errorf("m.connections hat AutoKeyFailures=%d, erwartet 1", got)
	}
}
