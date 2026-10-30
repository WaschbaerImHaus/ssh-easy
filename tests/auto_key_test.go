// Paket main - Tests für den automatischen Schlüssel bei Passwort-Verbindungen, Build 86
//
// Nutzerwunsch: "Wenn du eine Verbindung hast, die nur ein Passwort besitzt,
// versuche ungefragt einen Key zu vergeben." Bisher löste nur wasPassword=true
// (Login per Passwort) AutoDeployKey aus. Der Auto-Login beim Enter (Agent oder
// irgendein Schlüssel aus ~/.ssh) meldet wasPassword=false und lässt die
// Verbindung dauerhaft auf "password" stehen.
//   - connectionNeedsOwnKey(): reine Entscheidungsfunktion.
//   - autoDeployKeyFunc: Naht, damit die Tests kein echtes AutoDeployKey brauchen.
//   - sshConnectedMsg: holt die Verbindung aus m.connections (msg.conn bleibt
//     beim Auto-Login leer) und startet den Schlüssel-Einsatz nur bei Bedarf.
//
// Build 86, Nachbesserung nach dem Fable-Review:
//   - F5a: autoAuthOrder() - reine Funktion für die Reihenfolge der
//     Anmeldearten in connectAutoDiscover (konfigurierter Schlüssel zuerst).
//   - F5b: Nach erfolgreicher Vergabe (sshKeyDeployedMsg) wird der Auth-Cache
//     der Verbindung geleert; bei einem Fehler bleibt er stehen.
//
// @author Kurt Ingwer
// @date   2026-10-28 13:09
package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"
)

// autoDeployCall merkt sich einen Aufruf des Fakes.
type autoDeployCall struct {
	conn       Connection
	client     *ssh.Client
	configPath string
}

// installFakeAutoDeploy ersetzt autoDeployKeyFunc durch einen Fake, der alle
// Aufrufe aufzeichnet und keyPath/err zurückgibt. Die Originalfunktion wird per
// t.Cleanup wiederhergestellt. onCall (optional) läuft im Fake, z. B. um die
// Konfiguration wie das echte AutoDeployKey zu aktualisieren.
//
// @param t - Testkontext
// @param keyPath - Rückgabewert des Fakes
// @param onCall - optionaler Zusatzcode im Fake
// @return *[]autoDeployCall - Zeiger auf die Aufrufliste
// @date 2026-10-27 18:30
func installFakeAutoDeploy(t *testing.T, keyPath string, onCall func(Connection, string)) *[]autoDeployCall {
	t.Helper()
	calls := &[]autoDeployCall{}
	orig := autoDeployKeyFunc
	t.Cleanup(func() { autoDeployKeyFunc = orig })
	autoDeployKeyFunc = func(conn Connection, client *ssh.Client, configPath string) (string, error) {
		*calls = append(*calls, autoDeployCall{conn: conn, client: client, configPath: configPath})
		if onCall != nil {
			onCall(conn, configPath)
		}
		return keyPath, nil
	}
	return calls
}

// autoKeyModel baut ein Modell mit einer Verbindung "id-0" (10.0.0.1, root,
// Port 22) und setzt AuthType und KeyPath direkt in m.connections.
//
// @param t - Testkontext
// @param auth - Authentifizierungsart
// @param keyPath - Schlüsselpfad (leer = keiner)
// @return AppModel - Modell
// @date 2026-10-27 18:30
func autoKeyModel(t *testing.T, auth AuthMethod, keyPath string) AppModel {
	t.Helper()
	m := newListTestModel(t, "Server")
	m.connections[0].AuthType = auth
	m.connections[0].KeyPath = keyPath
	return m
}

// sendConnected schickt eine sshConnectedMsg mit LEERER msg.conn (wie der
// Auto-Login in startAutoConnect) und liefert Modell und Cmd.
//
// @param m - Modell
// @param wasPassword - Login per Passwort?
// @return AppModel - Modell nach der Meldung
// @return tea.Cmd - Folgekommando (Schlüsselvergabe oder nil)
// @date 2026-10-28 13:09
func sendConnected(m AppModel, wasPassword bool) (AppModel, tea.Cmd) {
	updated, cmd := m.Update(sshConnectedMsg{id: "id-0", status: &ConnectionStatus{}, wasPassword: wasPassword})
	return updated.(AppModel), cmd
}

// TestConnectionNeedsOwnKey prüft die Entscheidung als Tabelle. Nur "password
// ohne Schlüssel" braucht einen eigenen Schlüssel. Randfälle: password MIT
// KeyPath (Altbestand, der Schlüssel existiert schon), agent ohne Schlüssel
// (bewusste Wahl des Nutzers, kein Eingriff) und leerer AuthType aus alten
// Konfigurationen - dort wird bewusst false erwartet: Unklar, was der Nutzer
// wollte, und ungefragt Schlüssel zu erzeugen wäre der riskantere Fehler.
//
// @date 2026-10-27 18:30
func TestConnectionNeedsOwnKey(t *testing.T) {
	cases := []struct {
		label   string
		auth    AuthMethod
		keyPath string
		want    bool
	}{
		{"password ohne Schlüssel", AuthPassword, "", true},
		{"key mit Schlüssel", AuthKey, "~/.ssh/id_x", false},
		{"password mit KeyPath (Altbestand)", AuthPassword, "~/.ssh/id_x", false},
		{"agent ohne Schlüssel", AuthAgent, "", false},
		{"leerer AuthType ohne Schlüssel (alte Config)", AuthMethod(""), "", false},
		{"key ohne KeyPath (ungültig)", AuthKey, "", false},
	}
	for _, c := range cases {
		conn := NewConnection("n", "h", 22, "u", c.auth)
		conn.KeyPath = c.keyPath
		if got := connectionNeedsOwnKey(conn); got != c.want {
			t.Errorf("%s: connectionNeedsOwnKey = %v, erwartet %v", c.label, got, c.want)
		}
	}
}

// TestAutoLoginPasswordConnectionDeploysKey prüft den Kern des Wunsches:
// Auto-Login (wasPassword=false, msg.conn leer) einer password-Verbindung ohne
// Schlüssel startet den Schlüssel-Einsatz. Der Fake bekommt die Verbindung aus
// m.connections (ID, Host, User, Port), NICHT die leere msg.conn, und genau
// einmal. Das Ergebnis ist sshKeyDeployedMsg mit dem Pfad des Fakes. Die
// Ansicht wechselt wie bisher zur Statusseite.
//
// @date 2026-10-27 18:30
func TestAutoLoginPasswordConnectionDeploysKey(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := autoKeyModel(t, AuthPassword, "")

	m, cmd := sendConnected(m, false)
	if cmd == nil {
		t.Fatal("Cmd ist nil: Passwort-Verbindung ohne Schlüssel bekommt keinen Schlüssel")
	}
	if m.state != ViewStatus || m.activeID != "id-0" {
		t.Errorf("state=%v activeID=%q, erwartet ViewStatus und id-0", m.state, m.activeID)
	}
	if len(*calls) != 0 {
		t.Fatalf("Fake läuft schon vor Ausführung des Cmd (%d Aufrufe)", len(*calls))
	}
	msg := cmd()
	if len(*calls) != 1 {
		t.Fatalf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 1", len(*calls))
	}
	got := (*calls)[0]
	if got.conn.ID != "id-0" || got.conn.Host != "10.0.0.1" || got.conn.User != "root" || got.conn.Port != 22 {
		t.Errorf("Fake bekam unvollständige Verbindung: %+v", got.conn)
	}
	if got.configPath != m.configPath {
		t.Errorf("configPath = %q, erwartet %q", got.configPath, m.configPath)
	}
	dep, ok := msg.(sshKeyDeployedMsg)
	if !ok {
		t.Fatalf("Cmd liefert %T, erwartet sshKeyDeployedMsg", msg)
	}
	if dep.connID != "id-0" || dep.keyPath != "~/.ssh/id_fake" || dep.err != nil {
		t.Errorf("sshKeyDeployedMsg = %+v", dep)
	}
}

// TestAutoLoginKeyConnectionDoesNotDeploy prüft: Hat die Verbindung schon
// einen eigenen Schlüssel (AuthKey + KeyPath), gibt es bei wasPassword=false
// weder Fake-Aufruf noch Cmd (sonst bekäme jede Verbindung bei jedem Enter
// einen neuen Schlüssel).
//
// @date 2026-10-27 18:30
func TestAutoLoginKeyConnectionDoesNotDeploy(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := autoKeyModel(t, AuthKey, "~/.ssh/id_own")

	m, cmd := sendConnected(m, false)
	if cmd != nil {
		t.Errorf("Cmd müsste nil sein, ist %T-Ergebnis %v", cmd, cmd())
	}
	if len(*calls) != 0 {
		t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
	}
	if m.state != ViewStatus {
		t.Errorf("state = %v, erwartet ViewStatus", m.state)
	}
}

// TestAutoLoginAgentConnectionDoesNotDeploy prüft: AuthAgent ohne KeyPath ist
// eine bewusste Wahl (Schlüssel liegt im Agent). Kein Aufruf, kein Cmd.
//
// @date 2026-10-27 18:30
func TestAutoLoginAgentConnectionDoesNotDeploy(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := autoKeyModel(t, AuthAgent, "")

	_, cmd := sendConnected(m, false)
	if cmd != nil {
		t.Errorf("Cmd müsste nil sein, Ergebnis %v", cmd())
	}
	if len(*calls) != 0 {
		t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
	}
}

// TestPasswordLoginDeploysWithFullConnectionFromList prüft das bisherige
// Verhalten mit der neuen Quelle: wasPassword=true löst auch bei einer
// Key-Verbindung einen Aufruf aus, und die Verbindung stammt aus
// m.connections mit ALLEN Feldern (KeyPath, AuthType, Name, Host, User, Port),
// nicht aus der leeren msg.conn.
//
// @date 2026-10-27 18:30
func TestPasswordLoginDeploysWithFullConnectionFromList(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := autoKeyModel(t, AuthKey, "~/.ssh/id_own")
	m.connections[0].Port = 2222

	_, cmd := sendConnected(m, true)
	if cmd == nil {
		t.Fatal("Cmd ist nil, erwartet Schlüssel-Einsatz bei wasPassword=true")
	}
	cmd()
	if len(*calls) != 1 {
		t.Fatalf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 1", len(*calls))
	}
	c := (*calls)[0].conn
	if c.ID != "id-0" || c.Name != "Server" || c.Host != "10.0.0.1" || c.User != "root" ||
		c.Port != 2222 || c.AuthType != AuthKey || c.KeyPath != "~/.ssh/id_own" {
		t.Errorf("Verbindung nicht vollständig aus m.connections: %+v", c)
	}
}

// TestAutoLoginUnknownConnectionIsIgnored prüft den Randfall: Die ID der
// Meldung ist nicht (mehr) in der Liste (z. B. zwischenzeitlich gelöscht).
// Bei wasPassword=false darf nichts abstürzen und nichts erzeugt werden.
//
// @date 2026-10-27 18:30
func TestAutoLoginUnknownConnectionIsIgnored(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := autoKeyModel(t, AuthPassword, "")

	updated, cmd := m.Update(sshConnectedMsg{id: "gibt-es-nicht", status: &ConnectionStatus{}})
	_ = updated
	if cmd != nil {
		t.Errorf("Cmd müsste nil sein, Ergebnis %v", cmd())
	}
	if len(*calls) != 0 {
		t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
	}
}

// TestAutoKeyFullFlowSwitchesToKey prüft den Ablauf ganz: Auto-Login einer
// password-Verbindung, der Fake schreibt wie das echte AutoDeployKey AuthKey
// und KeyPath in die Konfiguration, danach sshKeyDeployedMsg. Die Liste ist
// neu geladen (key + KeyPath), die Statusansicht zeigt "key" statt "password".
// Ein zweiter Auto-Login löst dann keinen weiteren Einsatz mehr aus (kein
// neuer Schlüssel bei jedem Enter).
//
// @date 2026-10-27 18:30
func TestAutoKeyFullFlowSwitchesToKey(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", func(conn Connection, configPath string) {
		conn.AuthType = AuthKey
		conn.KeyPath = "~/.ssh/id_fake"
		if err := UpdateConnection(configPath, conn); err != nil {
			t.Errorf("UpdateConnection im Fake: %v", err)
		}
	})
	m := newListTestModel(t, "Server")
	m.activeID = "id-0"
	// Zeile "Auth:    <art>" der Statusansicht (Label aus der Sprachtabelle)
	authLine := func(mm AppModel, auth string) bool {
		return strings.Contains(stripANSI(mm.View()), mm.lang.LabelAuth+auth)
	}

	m, cmd := sendConnected(m, false)
	if !authLine(m, "password") {
		t.Fatalf("Vorher müsste die Statusansicht \"password\" zeigen:\n%s", stripANSI(m.View()))
	}
	if cmd == nil {
		t.Fatal("Cmd ist nil, erwartet Schlüssel-Einsatz")
	}
	updated, _ := m.Update(cmd())
	m = updated.(AppModel)

	if len(*calls) != 1 {
		t.Fatalf("Fake %d-mal aufgerufen, erwartet 1", len(*calls))
	}
	conn := m.connections[0]
	if conn.AuthType != AuthKey || conn.KeyPath != "~/.ssh/id_fake" {
		t.Errorf("Nach dem Neuladen: AuthType=%q KeyPath=%q, erwartet key und ~/.ssh/id_fake", conn.AuthType, conn.KeyPath)
	}
	view := stripANSI(m.View())
	if !authLine(m, "key") || strings.Contains(view, "password") {
		t.Errorf("Statusansicht zeigt nicht \"key\" statt \"password\":\n%s", view)
	}

	// Zweiter Auto-Login: Schlüssel vorhanden, also kein weiterer Einsatz
	m.state = ViewList
	_, cmd = sendConnected(m, false)
	if cmd != nil {
		t.Errorf("zweiter Auto-Login startet erneut einen Einsatz: %v", cmd())
	}
	if len(*calls) != 1 {
		t.Errorf("Fake nach zweitem Login %d-mal aufgerufen, erwartet weiter 1", len(*calls))
	}
}

// --- F5a: Reihenfolge der Anmeldearten ---

// TestAutoAuthOrder (F5a): Es gab keine Naht für die Reihenfolge in
// connectAutoDiscover (Agent, dann KeyPath, dann ~/.ssh stehen fest im
// Ablauf, der ein echtes Netz braucht). Deshalb die reine Funktion
// autoAuthOrder(conn, agentAvailable, keyFiles) []string. Sie liefert die
// Cache-Schlüssel in Probierreihenfolge: "key:<abs>" und "agent". Neu: Der
// konfigurierte KeyPath kommt VOR dem Agent - sonst gewinnt bei einer Verbindung
// mit frisch vergebenem Schlüssel weiter der Agent, und der Schlüssel wird nie
// benutzt. Randfälle: KeyPath nicht doppelt, wenn er unter den keyFiles steht
// (Tilde, absolut und unsauber "./" gelten als gleich); ohne KeyPath Agent
// zuerst wie bisher; ohne Agent kein "agent"; nichts vorhanden = leer.
// Die Eingabeliste darf nicht verändert werden.
//
// @date 2026-10-28 13:09
func TestAutoAuthOrder(t *testing.T) {
	home := setTestHome(t)
	abs := func(name string) string { return filepath.Join(home, ".ssh", name) }
	key := func(name string) string { return "key:" + abs(name) }
	cases := []struct {
		label   string
		keyPath string
		agent   bool
		files   []string
		want    []string
	}{
		{"KeyPath zuerst, dann Agent, dann der Rest", "~/.ssh/id_neu", true,
			[]string{abs("id_a"), abs("id_b")}, []string{key("id_neu"), "agent", key("id_a"), key("id_b")}},
		{"KeyPath nicht doppelt (Tilde, absolut in keyFiles)", "~/.ssh/id_neu", true,
			[]string{abs("id_a"), abs("id_neu"), abs("id_b")}, []string{key("id_neu"), "agent", key("id_a"), key("id_b")}},
		{"KeyPath absolut, steht auch in keyFiles", abs("id_neu"), true,
			[]string{abs("id_neu"), abs("id_a")}, []string{key("id_neu"), "agent", key("id_a")}},
		{"Tilde in keyFiles gilt als gleich", "~/.ssh/id_neu", true,
			[]string{"~/.ssh/id_neu", abs("id_a")}, []string{key("id_neu"), "agent", key("id_a")}},
		{"unsauberer KeyPath (./) gilt als gleich", "~/.ssh/./id_neu", true,
			[]string{abs("id_neu"), abs("id_a")}, []string{key("id_neu"), "agent", key("id_a")}},
		{"ohne KeyPath: Agent zuerst wie bisher", "", true,
			[]string{abs("id_a"), abs("id_b")}, []string{"agent", key("id_a"), key("id_b")}},
		{"ohne Agent, mit KeyPath", "~/.ssh/id_neu", false,
			[]string{abs("id_a")}, []string{key("id_neu"), key("id_a")}},
		{"ohne Agent, ohne KeyPath: Reihenfolge der Dateien bleibt", "", false,
			[]string{abs("id_b"), abs("id_a")}, []string{key("id_b"), key("id_a")}},
		{"nur Agent", "", true, nil, []string{"agent"}},
		{"nur KeyPath", "~/.ssh/id_neu", false, nil, []string{key("id_neu")}},
		{"KeyPath und Agent, keine weiteren Dateien", "~/.ssh/id_neu", true, nil, []string{key("id_neu"), "agent"}},
		{"nichts vorhanden", "", false, nil, nil},
	}
	for _, c := range cases {
		conn := NewConnection("n", "h", 22, "u", AuthKey)
		conn.KeyPath = c.keyPath
		input := append([]string(nil), c.files...)
		got := autoAuthOrder(conn, c.agent, c.files)
		if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%s:\n got  %q\n want %q", c.label, got, c.want)
		}
		if !reflect.DeepEqual(c.files, input) && !(len(c.files) == 0 && len(input) == 0) {
			t.Errorf("%s: keyFiles wurde verändert: %q statt %q", c.label, c.files, input)
		}
	}
}

// --- F5b: Auth-Cache nach der Vergabe leeren (Hauptablauf) ---

// newAuthCacheModel baut ein Modell mit Verbindung "id-0" und trägt im
// Auth-Cache für "id-0" und eine fremde ID je den Eintrag "agent" ein. HOME
// zeigt vorher auf ein Temp-Verzeichnis, damit der Cache nie die echte
// ~/.ssh-easy/auth-cache.json beschreibt.
//
// @param t - Testkontext
// @return AppModel - Modell mit gefülltem Auth-Cache
// @date 2026-10-28 13:09
func newAuthCacheModel(t *testing.T) AppModel {
	t.Helper()
	setTestHome(t)
	m := autoKeyModel(t, AuthPassword, "")
	m.sshManager.authCache.RecordSuccess("id-0", authCacheMethodAgent)
	m.sshManager.authCache.RecordSuccess("id-fremd", authCacheMethodAgent)
	return m
}

// TestKeyDeployedMsgClearsAuthCache (F5b, Hauptablauf): Nach erfolgreicher
// Vergabe ist der Cache-Eintrag der Verbindung weg, damit die nächste
// Anmeldung den neuen Schlüssel probiert statt des gemerkten Agents. Der
// Eintrag einer ANDEREN Verbindung bleibt (es zählt msg.connID). Rot heute:
// der Eintrag bleibt stehen.
//
// @date 2026-10-28 13:09
func TestKeyDeployedMsgClearsAuthCache(t *testing.T) {
	m := newAuthCacheModel(t)
	updated, _ := m.Update(sshKeyDeployedMsg{connID: "id-0", keyPath: "~/.ssh/id_neu"})
	m = updated.(AppModel)
	if entry := m.sshManager.authCache.Get("id-0"); entry != nil {
		t.Errorf("Auth-Cache von id-0 nach Erfolg = %+v, erwartet leer", entry)
	}
	if entry := m.sshManager.authCache.Get("id-fremd"); entry == nil || entry.Method != authCacheMethodAgent {
		t.Errorf("Eintrag einer anderen Verbindung verändert: %+v", entry)
	}
	if m.successMsg == "" || m.errorMsg != "" {
		t.Errorf("successMsg=%q errorMsg=%q, erwartet Erfolgsmeldung", m.successMsg, m.errorMsg)
	}
}

// TestKeyDeployedMsgErrorKeepsAuthCache (F5b): Scheitert die Vergabe, steht
// weder ein neuer Schlüssel noch ein geänderter Zustand fest - der Cache
// bleibt unverändert (Methode "agent", Fehlerzähler 0).
//
// @date 2026-10-28 13:09
func TestKeyDeployedMsgErrorKeepsAuthCache(t *testing.T) {
	m := newAuthCacheModel(t)
	updated, _ := m.Update(sshKeyDeployedMsg{connID: "id-0", err: errors.New("serverfehler")})
	m = updated.(AppModel)
	for _, id := range []string{"id-0", "id-fremd"} {
		entry := m.sshManager.authCache.Get(id)
		if entry == nil || entry.Method != authCacheMethodAgent || entry.FailureCount != 0 {
			t.Errorf("Auth-Cache von %s nach Fehler = %+v, erwartet unverändert", id, entry)
		}
	}
	if m.errorMsg == "" {
		t.Error("errorMsg leer, erwartet Fehlermeldung")
	}
}

// TestKeyDeployedMsgUnknownConnectionClearsOnlyItsEntry (F5b, Randfall): Die
// ID der Meldung gehört nicht (mehr) zu einer Verbindung der Liste (gelöscht,
// während die Vergabe lief). Das darf nichts zerstören: kein Absturz, Einträge
// anderer Verbindungen bleiben, und ein Eintrag unter genau dieser ID ist weg
// (die Vergabe war ja erfolgreich).
//
// @date 2026-10-28 13:09
func TestKeyDeployedMsgUnknownConnectionClearsOnlyItsEntry(t *testing.T) {
	m := newAuthCacheModel(t)
	m.sshManager.authCache.RecordSuccess("id-weg", authCacheMethodAgent)
	updated, _ := m.Update(sshKeyDeployedMsg{connID: "id-weg", keyPath: "~/.ssh/id_neu"})
	m = updated.(AppModel)
	if entry := m.sshManager.authCache.Get("id-weg"); entry != nil {
		t.Errorf("Eintrag von id-weg = %+v, erwartet leer", entry)
	}
	for _, id := range []string{"id-0", "id-fremd"} {
		if entry := m.sshManager.authCache.Get(id); entry == nil {
			t.Errorf("Eintrag von %s ist verschwunden", id)
		}
	}
}

// --- Merken und aussetzen: AutoKeyOff / AutoKeyFailures (Nutzerentscheidung Build 86) ---
//
// Entscheidung des Nutzers: Die automatische Vergabe bleibt "immer", auch nach
// einem Agent-Login. Nach r (Schlüssel entfernen) und nach autoKeyMaxFailures
// gescheiterten Versuchen gibt es für die Verbindung keine automatische
// Vergabe mehr. Eine Passwort-Anmeldung vergibt wie bisher immer und schaltet
// sie damit wieder ein.

// TestAutoKeyMaxFailuresIsTwo: Die Grenze ist festgelegt auf 2 Fehlversuche.
//
// @date 2026-10-28 14:00
func TestAutoKeyMaxFailuresIsTwo(t *testing.T) {
	if autoKeyMaxFailures != 2 {
		t.Errorf("autoKeyMaxFailures = %d, erwartet 2", autoKeyMaxFailures)
	}
}

// TestConnectionNeedsOwnKeyAutoKeyFields: connectionNeedsOwnKey als Tabelle mit
// den neuen Feldern. Grundregel: KeyPath leer, AuthType password, AutoKeyOff
// aus und weniger als 2 Fehlversuche. Randfälle: Off wirkt auch ohne Fehler und
// zusammen mit hohen Zählern; Zähler 1 vergibt noch, 2 und 3 nicht; ein
// negativer Zähler (von Hand kaputt bearbeitete Konfiguration) gilt als
// "unter der Grenze"; die Felder machen eine Verbindung mit Schlüssel oder
// Agent nie vergabefähig.
//
// @date 2026-10-28 14:00
func TestConnectionNeedsOwnKeyAutoKeyFields(t *testing.T) {
	cases := []struct {
		label    string
		auth     AuthMethod
		keyPath  string
		off      bool
		failures int
		want     bool
	}{
		{"Grundfall", AuthPassword, "", false, 0, true},
		{"AutoKeyOff", AuthPassword, "", true, 0, false},
		{"AutoKeyOff mit Fehlern", AuthPassword, "", true, 5, false},
		{"1 Fehler", AuthPassword, "", false, 1, true},
		{"2 Fehler (Grenze)", AuthPassword, "", false, 2, false},
		{"3 Fehler", AuthPassword, "", false, 3, false},
		{"negativer Zähler", AuthPassword, "", false, -1, true},
		{"Schlüssel, kein Off", AuthKey, "~/.ssh/id_x", false, 0, false},
		{"Agent, kein Off", AuthAgent, "", false, 0, false},
		{"Passwort mit KeyPath, kein Off", AuthPassword, "~/.ssh/id_x", false, 0, false},
	}
	for _, c := range cases {
		conn := NewConnection("n", "h", 22, "u", c.auth)
		conn.KeyPath = c.keyPath
		conn.AutoKeyOff = c.off
		conn.AutoKeyFailures = c.failures
		if got := connectionNeedsOwnKey(conn); got != c.want {
			t.Errorf("%s: connectionNeedsOwnKey = %v, erwartet %v", c.label, got, c.want)
		}
	}
}

// TestAutoLoginSuspendedKeepsPasswordLoginDeploying (Hauptablauf): Bei
// AutoKeyOff und bei erreichter Fehlergrenze startet der Auto-Login
// (wasPassword=false) keine Vergabe - kein Cmd, kein Aufruf. Der Login mit
// Passwort (wasPassword=true) vergibt aber wie bisher, und der Fake bekommt
// die ganze Verbindung aus der Liste (mit den neuen Feldern). Mit 1 Fehler
// vergibt auch der Auto-Login noch.
//
// @date 2026-10-28 14:00
func TestAutoLoginSuspendedKeepsPasswordLoginDeploying(t *testing.T) {
	cases := []struct {
		label     string
		off       bool
		failures  int
		autoLogin bool // erwartet: Auto-Login vergibt
	}{
		{"AutoKeyOff", true, 0, false},
		{"Fehlergrenze erreicht", false, autoKeyMaxFailures, false},
		{"über der Grenze", false, autoKeyMaxFailures + 3, false},
		{"AutoKeyOff und Fehler", true, 1, false},
		{"ein Fehler", false, 1, true},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
			m := autoKeyModel(t, AuthPassword, "")
			m.connections[0].AutoKeyOff = c.off
			m.connections[0].AutoKeyFailures = c.failures

			_, cmd := sendConnected(m, false)
			if c.autoLogin {
				if cmd == nil {
					t.Fatal("Auto-Login vergibt nicht, erwartet Vergabe")
				}
				cmd()
				if len(*calls) != 1 {
					t.Errorf("Auto-Login: %d Aufrufe, erwartet 1", len(*calls))
				}
			} else {
				if cmd != nil {
					t.Error("Auto-Login startet trotz Aussetzen eine Vergabe")
				}
				if len(*calls) != 0 {
					t.Errorf("Auto-Login: %d Aufrufe, erwartet 0", len(*calls))
				}
			}

			*calls = nil
			_, cmd = sendConnected(m, true)
			if cmd == nil {
				t.Fatal("Passwort-Anmeldung vergibt nicht (muss immer vergeben)")
			}
			cmd()
			if len(*calls) != 1 {
				t.Fatalf("Passwort-Anmeldung: %d Aufrufe, erwartet 1", len(*calls))
			}
			got := (*calls)[0].conn
			if got.AutoKeyOff != c.off || got.AutoKeyFailures != c.failures {
				t.Errorf("Fake bekam AutoKeyOff=%v AutoKeyFailures=%d, erwartet %v und %d", got.AutoKeyOff, got.AutoKeyFailures, c.off, c.failures)
			}
		})
	}
}

// TestStatusKeyRemoveSuspendsAutoKey: Der Weg über die Statusansicht. Eine
// Verbindung mit Schlüssel: r, j entfernt ihn (Dateien, Konfiguration), danach
// steht AutoKeyOff in der gespeicherten und in der neu geladenen Liste. Ein
// anschließender Auto-Login vergibt nicht, ein Passwort-Login schon. Läuft
// ohne Server (die Verbindung ist in der Statusansicht nicht aktiv).
//
// @date 2026-10-28 14:00
func TestStatusKeyRemoveSuspendsAutoKey(t *testing.T) {
	home := setTestHome(t)
	writeTestKeyPair(t, home)
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := newListTestModel(t, "Server")
	m = editTestConfig(t, m, func(cfg *AppConfig) {
		setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_test")
	})
	m.activeID = "id-0"

	m = pressKeyRemove(t, m)

	saved := loadTestConfig(t, m).Connections[0]
	if !saved.AutoKeyOff || saved.AuthType != AuthPassword || saved.KeyPath != "" {
		t.Fatalf("gespeichert: AutoKeyOff=%v AuthType=%q KeyPath=%q, erwartet true, password, leer", saved.AutoKeyOff, saved.AuthType, saved.KeyPath)
	}
	if !m.connections[0].AutoKeyOff {
		t.Error("Liste nicht neu geladen: AutoKeyOff fehlt in m.connections")
	}
	if _, autoCmd := sendConnected(m, false); autoCmd != nil {
		t.Error("Auto-Login vergibt nach r sofort wieder einen Schlüssel")
	}
	_, cmd := sendConnected(m, true)
	if cmd == nil {
		t.Fatal("Passwort-Login vergibt nicht")
	}
	cmd()
	if len(*calls) != 1 {
		t.Errorf("%d Aufrufe, erwartet 1 (nur der Passwort-Login)", len(*calls))
	}
}

// pressKeyRemove führt in der Statusansicht r und die Bestätigung j aus und
// speist die Antwort des Hintergrundkommandos (sshKeyRemovedMsg) ins Modell.
//
// @param t - Testkontext
// @param m - Modell in ViewStatus mit Schlüssel-Verbindung
// @return AppModel - Modell nach der Antwort
// @date 2026-10-28 14:00
func pressKeyRemove(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m.state = ViewStatus
	updated, _ := m.Update(keyMsgFor("r"))
	m = updated.(AppModel)
	if !m.confirmKeyRemove {
		t.Fatalf("keine Rückfrage nach r (Fehler %q)", m.errorMsg)
	}
	updated, cmd := m.Update(keyMsgFor("j"))
	m = updated.(AppModel)
	if cmd == nil {
		t.Fatal("kein Kommando nach der Bestätigung")
	}
	msg, ok := cmd().(sshKeyRemovedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("Antwort %#v, erwartet sshKeyRemovedMsg ohne Fehler", msg)
	}
	updated, _ = m.Update(msg)
	return updated.(AppModel)
}

// TestStatusKeyRemoveThenEditKeepsAutoKeyOff (H1, Schutztest): r, j in der
// Statusansicht (nur über Update, kein Neuladen von Hand), danach e und
// unverändert speichern. sshKeyRemovedMsg lädt die Liste schon neu (Invalidate
// + reloadConfig), also steht AutoKeyOff in m.connections und das Speichern
// schreibt es nicht als false zurück. Erwartet: in der Konfiguration bleibt
// AutoKeyOff=true, AuthType password, KeyPath leer. Läuft heute grün; schützt
// vor einem Rückschritt.
//
// @date 2026-10-30 09:00
func TestStatusKeyRemoveThenEditKeepsAutoKeyOff(t *testing.T) {
	home := setTestHome(t)
	writeTestKeyPair(t, home)
	m := newListTestModel(t, "Server")
	m = editTestConfig(t, m, func(cfg *AppConfig) {
		setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_test")
	})
	m.activeID = "id-0"
	m = pressKeyRemove(t, m)
	if m.state != ViewList {
		t.Fatalf("state = %v, erwartet ViewList", m.state)
	}
	if !m.connections[0].AutoKeyOff {
		t.Fatal("m.connections hat AutoKeyOff=false nach r, j (Liste nicht neu geladen?)")
	}
	m.selectConnectionByID("id-0")
	m = press(t, m, "e")
	if m.state != ViewEdit {
		t.Fatalf("state = %v, erwartet ViewEdit", m.state)
	}
	m = saveFormNow(t, m)
	saved := testConnByID(t, loadTestConfig(t, m), "id-0")
	if !saved.AutoKeyOff || saved.AuthType != AuthPassword || saved.KeyPath != "" {
		t.Errorf("nach e und Speichern: AutoKeyOff=%v AuthType=%q KeyPath=%q, erwartet true, password, leer", saved.AutoKeyOff, saved.AuthType, saved.KeyPath)
	}
}

// --- Frische Prüfung im Vergabe-Kommando (autoDeployKeyCmd, Build 86) ---
//
// m.connections ist nur der Stand beim Anmelden. Das Kommando liest unter
// fmDeployMu den Eintrag frisch aus der Datei und prüft nach einem Auto-Login
// connectionNeedsOwnKey dort erneut. Gilt die Regel nicht mehr, kommt
// sshKeyDeployedMsg{skipped: true}: kein Aufruf, keine Meldung, Liste neu
// geladen. Nach einem Passwort-Login wird immer vergeben. Die Tests lassen den
// Speicher absichtlich beim alten Stand ("password" ohne Schlüssel) und ändern
// nur die Datei.

// newStaleMemoryModel baut ein Modell, dessen Verbindung "id-0" im Speicher
// (m.connections) auf "password" ohne Schlüssel steht, während die Datei durch
// edit geändert wird - so, als hätte inzwischen ein anderer Ablauf (zweite
// Anmeldung, Datei-Manager, e) gespeichert.
//
// @param t    - Testkontext
// @param edit - Änderung der Konfigurationsdatei (nicht des Modells)
// @return AppModel - Modell mit veraltetem Speicher
// @date 2026-10-30 16:40
func newStaleMemoryModel(t *testing.T, edit func(cfg *AppConfig)) AppModel {
	t.Helper()
	setTestHome(t)
	m := autoKeyModel(t, AuthPassword, "")
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		edit(cfg)
		return true
	})
	if err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
	return m
}

// runSkippableDeploy meldet eine Anmeldung, führt das Vergabe-Kommando aus
// und liefert dessen Ergebnis. Vorher werden die Meldungen der Anmeldung
// gelöscht, damit nach der Antwort geprüft werden kann, ob die Vergabe selbst
// eine Meldung gesetzt hat.
//
// @param t           - Testkontext
// @param m           - Modell
// @param wasPassword - Anmeldung per Passwort?
// @return AppModel - Modell mit gelöschten Meldungen (vor der Antwort)
// @return sshKeyDeployedMsg - Ergebnis des Kommandos
// @date 2026-10-30 16:40
func runSkippableDeploy(t *testing.T, m AppModel, wasPassword bool) (AppModel, sshKeyDeployedMsg) {
	t.Helper()
	m, cmd := sendConnected(m, wasPassword)
	if cmd == nil {
		t.Fatal("Cmd ist nil: Der Speicher steht auf \"password\" ohne Schlüssel, also startet die Vergabe")
	}
	m.successMsg, m.errorMsg = "", ""
	raw := cmd()
	msg, ok := raw.(sshKeyDeployedMsg)
	if !ok {
		t.Fatalf("Cmd liefert %T, erwartet sshKeyDeployedMsg", raw)
	}
	return m, msg
}

// TestAutoLoginSkipsWhenFileAlreadyHasKey (1): Im Speicher "password" ohne
// Schlüssel, in der Datei schon "key" mit KeyPath (eine parallele Vergabe war
// schneller). Auto-Login: Das Kommando übergeht (skipped=true, kein Fehler,
// kein Pfad), der Fake wird nicht aufgerufen. Nach Update keine Meldung (weder
// successMsg noch errorMsg), die Liste ist neu geladen und zeigt "key". Rot
// ohne frische Prüfung: Der Fake wird trotzdem aufgerufen, und es entsteht eine
// Erfolgsmeldung für eine zweite Vergabe.
//
// @date 2026-10-30 16:40
func TestAutoLoginSkipsWhenFileAlreadyHasKey(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := newStaleMemoryModel(t, func(cfg *AppConfig) { setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_schon") })
	if m.connections[0].AuthType != AuthPassword {
		t.Fatal("Testaufbau: Speicher müsste noch auf password stehen")
	}

	m, msg := runSkippableDeploy(t, m, false)
	if len(*calls) != 0 {
		t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
	}
	if !msg.skipped || msg.err != nil || msg.keyPath != "" || msg.connID != "id-0" {
		t.Errorf("Ergebnis %+v, erwartet skipped ohne Fehler und Pfad für id-0", msg)
	}
	updated, follow := m.Update(msg)
	m = updated.(AppModel)
	if follow != nil {
		t.Error("Folgekommando nach übergangener Vergabe")
	}
	if m.successMsg != "" || m.errorMsg != "" {
		t.Errorf("Meldung trotz Übergehen: success=%q error=%q", m.successMsg, m.errorMsg)
	}
	if got := m.connections[0]; got.AuthType != AuthKey || got.KeyPath != "~/.ssh/id_schon" {
		t.Errorf("Liste nicht neu geladen: AuthType=%q KeyPath=%q, erwartet key und ~/.ssh/id_schon", got.AuthType, got.KeyPath)
	}
}

// TestPasswordLoginDeploysEvenWhenFileHasKey (2): Derselbe Stand wie oben
// (Speicher "password", Datei "key"), aber Anmeldung per Passwort: Es wird wie
// bisher immer vergeben - genau ein Aufruf, nicht übergangen. (Der Passwort-
// Login ist der Weg, einen kaputten Schlüssel zu ersetzen.)
//
// @date 2026-10-30 16:40
func TestPasswordLoginDeploysEvenWhenFileHasKey(t *testing.T) {
	calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
	m := newStaleMemoryModel(t, func(cfg *AppConfig) { setTestAuth(t, cfg, "id-0", AuthKey, "~/.ssh/id_schon") })

	_, msg := runSkippableDeploy(t, m, true)
	if len(*calls) != 1 {
		t.Fatalf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 1", len(*calls))
	}
	if msg.skipped || msg.err != nil || msg.keyPath != "~/.ssh/id_fake" {
		t.Errorf("Ergebnis %+v, erwartet Erfolg mit ~/.ssh/id_fake", msg)
	}
}

// TestAutoLoginSkipsWhenConnectionDeletedFromFile (3): Die Verbindung wurde
// inzwischen aus der Datei gelöscht. Auto-Login: kein Aufruf, übergangen, keine
// Meldung, kein Absturz; die Liste wird neu geladen und enthält die Verbindung
// nicht mehr. Ebenso bei unlesbarer Datei: kein Aufruf, übergangen, keine
// Meldung zur Schlüsselvergabe (die Fehlermeldung des Ladens, falls es eine
// gibt, ist davon unabhängig). Rot ohne frische Prüfung: Der Fake wird
// aufgerufen.
//
// @date 2026-10-30 16:40
func TestAutoLoginSkipsWhenConnectionDeletedFromFile(t *testing.T) {
	t.Run("gelöscht", func(t *testing.T) {
		calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
		m := newStaleMemoryModel(t, func(cfg *AppConfig) { cfg.Connections = nil })

		m, msg := runSkippableDeploy(t, m, false)
		if len(*calls) != 0 {
			t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
		}
		if !msg.skipped || msg.err != nil {
			t.Errorf("Ergebnis %+v, erwartet skipped ohne Fehler", msg)
		}
		updated, follow := m.Update(msg)
		m = updated.(AppModel)
		if follow != nil {
			t.Error("Folgekommando nach übergangener Vergabe")
		}
		if m.successMsg != "" || m.errorMsg != "" {
			t.Errorf("Meldung trotz Übergehen: success=%q error=%q", m.successMsg, m.errorMsg)
		}
		if len(m.connections) != 0 {
			t.Errorf("Liste nicht neu geladen: %d Verbindungen, erwartet 0", len(m.connections))
		}
	})
	t.Run("Datei unlesbar", func(t *testing.T) {
		calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
		m := newStaleMemoryModel(t, func(cfg *AppConfig) {})
		if err := os.WriteFile(m.configPath, []byte("{kaputt"), 0o600); err != nil {
			t.Fatal(err)
		}

		m, msg := runSkippableDeploy(t, m, false)
		if len(*calls) != 0 {
			t.Errorf("autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
		}
		if !msg.skipped || msg.err != nil {
			t.Errorf("Ergebnis %+v, erwartet skipped ohne Fehler", msg)
		}
		updated, _ := m.Update(msg)
		m = updated.(AppModel)
		if strings.Contains(m.successMsg, "id_fake") || strings.Contains(m.errorMsg, m.lang.KeyDeployFailed) {
			t.Errorf("Meldung zur Schlüsselvergabe trotz Übergehen: success=%q error=%q", m.successMsg, m.errorMsg)
		}
	})
}

// TestAutoLoginSkipsWhenFileSuspendsDeploy (4): In der Datei steht inzwischen
// AutoKeyOff=true (der Nutzer hat den Schlüssel in der Statusansicht entfernt)
// bzw. die Fehlergrenze ist erreicht (ein anderer Ablauf hat zweimal
// gezählt). Der Speicher kennt das noch nicht. Auto-Login: kein Aufruf,
// übergangen; die Liste zeigt danach den neuen Stand. Ein Passwort-Login mit
// demselben Stand vergibt (genau ein Aufruf).
//
// @date 2026-10-30 16:40
func TestAutoLoginSkipsWhenFileSuspendsDeploy(t *testing.T) {
	cases := []struct {
		label string
		edit  func(cfg *AppConfig)
		check func(t *testing.T, c Connection)
	}{
		{"AutoKeyOff", func(cfg *AppConfig) { cfg.Connections[0].AutoKeyOff = true }, func(t *testing.T, c Connection) {
			if !c.AutoKeyOff {
				t.Error("Liste nicht neu geladen: AutoKeyOff fehlt")
			}
		}},
		{"Fehlergrenze", func(cfg *AppConfig) { cfg.Connections[0].AutoKeyFailures = autoKeyMaxFailures }, func(t *testing.T, c Connection) {
			if c.AutoKeyFailures != autoKeyMaxFailures {
				t.Errorf("Liste nicht neu geladen: AutoKeyFailures=%d", c.AutoKeyFailures)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			calls := installFakeAutoDeploy(t, "~/.ssh/id_fake", nil)
			m := newStaleMemoryModel(t, c.edit)
			if m.connections[0].AutoKeyOff || m.connections[0].AutoKeyFailures != 0 {
				t.Fatal("Testaufbau: Speicher müsste noch ohne Aussetzen stehen")
			}

			auto, msg := runSkippableDeploy(t, m, false)
			if len(*calls) != 0 {
				t.Errorf("Auto-Login: autoDeployKeyFunc wurde %d-mal aufgerufen, erwartet 0", len(*calls))
			}
			if !msg.skipped || msg.err != nil {
				t.Errorf("Ergebnis %+v, erwartet skipped ohne Fehler", msg)
			}
			updated, _ := auto.Update(msg)
			auto = updated.(AppModel)
			if auto.successMsg != "" || auto.errorMsg != "" {
				t.Errorf("Meldung trotz Übergehen: success=%q error=%q", auto.successMsg, auto.errorMsg)
			}
			c.check(t, auto.connections[0])

			before := len(*calls)
			_, pwMsg := runSkippableDeploy(t, m, true)
			if got := len(*calls) - before; got != 1 || pwMsg.skipped {
				t.Errorf("Passwort-Login: %d Aufrufe, skipped=%v; erwartet 1 und nicht übergangen", got, pwMsg.skipped)
			}
		})
	}
}
