// Paket main - Tests: Neu verbinden nach Verbindungsabriss (Build 90, Teil 2)
//
// Bis Build 89 blieb ein Dateifenster nach einem Verbindungsabriss tot ("Verbindung
// unterbrochen"); der Datei-Manager musste ganz geschlossen und neu geöffnet
// werden. Jetzt verbindet ein Fenster beim nächsten Einlesen neu:
//   - Auslöser: Enter in ein Verzeichnis (fmEnter), Strg+R bzw. Menü
//     "Neu einlesen" (fmRescan) und das Neuladen nach Kopieren, Löschen und
//     Verzeichnis anlegen (fmReloadPanel)
//   - ist die Verbindung schon vorher verloren (fsConnectionLost), wird zuerst
//     neu verbunden und dann der Pfad des Fensters gelesen; scheitert das
//     Lesen und ist die Verbindung danach verloren, wird einmal neu verbunden
//     und erneut gelesen
//   - höchstens EIN Verbindungsversuch pro Aktion, nichts wiederholt sich
//     von selbst
//   - Erfolg: altes Dateisystem geschlossen, neues im Fenster, Pfad bleibt,
//     Meldung txtFMReconnected
//   - scheitert die Anmeldung (weder Netzwerk- noch Host-Key- noch
//     SFTP-Fehler): Passwort-Dialog (fmDialogReconnect); das Passwort wird nur
//     für diesen einen Versuch benutzt und nirgends gespeichert
//   - Netzwerkfehler und geänderter Host-Key: nur eine Meldung, kein Dialog
//   - veraltete Ergebnisse (Datei-Manager inzwischen geschlossen) schließen die
//     neue Verbindung wieder
//   - der lokale PC verbindet nie neu
//
// Testaufbau: fmConnect wird durch reconnectConnector ersetzt (zählt Aufrufe
// und Passwörter, Verhalten per Skript), die Dateisysteme sind reconnFS (ein
// fakeRemoteFS mit connectionLost()).
//
// @author Kurt Ingwer
// @date   2026-11-15 15:14
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// reconnFS ist ein fakeRemoteFS mit schaltbarem Verbindungsverlust (wie
// sftpFS, nachdem watchConnection die Leitung als tot erkannt hat).
type reconnFS struct {
	*fakeRemoteFS
	// Verbindung gilt als verloren (connectionLost)
	lost atomic.Bool
	// Das nächste Lesen reißt die Verbindung ab: ReadDir setzt lost und scheitert
	dieOnRead atomic.Bool
	closes    atomic.Int32
	reads     atomic.Int32
}

func newReconnFS() *reconnFS { return &reconnFS{fakeRemoteFS: newFakeFS()} }

func (f *reconnFS) connectionLost() bool { return f.lost.Load() }

func (f *reconnFS) ReadDir(p string) ([]os.FileInfo, error) {
	f.reads.Add(1)
	if f.dieOnRead.Load() {
		f.lost.Store(true)
	}
	if f.lost.Load() {
		return nil, errConnectionLost
	}
	return f.fakeRemoteFS.ReadDir(p)
}

func (f *reconnFS) Close() error {
	f.closes.Add(1)
	return nil
}

// reconnectConnector ersetzt m.fmConnect: zählt die Aufrufe ("id/passwort")
// und liefert nach einem Skript Dateisystem oder Fehler. Ohne Skript (oder
// wenn es nil, nil liefert) entsteht ein frisches, gesundes reconnFS.
type reconnectConnector struct {
	mu     sync.Mutex
	calls  []string
	script func(n int, conn Connection, password string) (remoteFS, error)
	byID   map[string][]*reconnFS
}

func (c *reconnectConnector) connect(conn Connection, password string) (remoteFS, error) {
	c.mu.Lock()
	c.calls = append(c.calls, conn.ID+"/"+password)
	n := len(c.calls)
	script := c.script
	c.mu.Unlock()
	if script != nil {
		fsys, err := script(n, conn, password)
		if err != nil {
			return nil, err
		}
		if fsys != nil {
			if f, ok := fsys.(*reconnFS); ok {
				c.track(conn.ID, f)
			}
			return fsys, nil
		}
	}
	f := newReconnFS()
	c.track(conn.ID, f)
	return f, nil
}

func (c *reconnectConnector) track(id string, f *reconnFS) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID = map[string][]*reconnFS{}
	}
	c.byID[id] = append(c.byID[id], f)
}

func (c *reconnectConnector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *reconnectConnector) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return ""
	}
	return c.calls[len(c.calls)-1]
}

func (c *reconnectConnector) setScript(script func(n int, conn Connection, password string) (remoteFS, error)) {
	c.mu.Lock()
	c.script = script
	c.mu.Unlock()
}

// openReconnectManager öffnet den Datei-Manager mit zwei Verbindungen
// ("Links" = id-0, "Rechts" = id-1) über reconnectConnector. Es wurden
// danach genau zwei Verbindungen aufgebaut.
func openReconnectManager(t *testing.T) (AppModel, *reconnectConnector) {
	t.Helper()
	m := newListTestModel(t, "Links", "Rechts")
	c := &reconnectConnector{}
	m.fmConnect = c.connect
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v (%q)", m.state, m.fm.message)
	}
	if c.count() != 2 {
		t.Fatalf("%d Verbindungen beim Öffnen, erwartet 2", c.count())
	}
	return m, c
}

// panelRecon liefert das reconnFS eines Fensters.
func panelRecon(t *testing.T, m AppModel, side int) *reconnFS {
	t.Helper()
	f, ok := m.fm.panels[side].fs.(*reconnFS)
	if !ok {
		t.Fatalf("Fenster %d hat kein reconnFS (%T)", side, m.fm.panels[side].fs)
	}
	return f
}

// reconnectedText liefert die erwartete Erfolgsmeldung.
func reconnectedText(m AppModel, side int) string {
	return fmt.Sprintf(m.tr(txtFMReconnected), sanitizeFileName(m.fm.panels[side].conn.Name))
}

// authFailure ist ein Anmeldefehler wie vom SSH-Client: weder Netzwerk- noch
// Host-Key- noch SFTP-Fehler.
var authFailure = errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain")

// assertReconnected prüft den Erfolgsfall: ein Verbindungsversuch seit base,
// neues Dateisystem im Fenster (altes geschlossen), Pfad unverändert, Meldung,
// kein Dialog. wantPassword: "" = Auto-Auth.
func assertReconnected(t *testing.T, m AppModel, c *reconnectConnector, base, side int, old *reconnFS, wantPath, wantPassword string) *reconnFS {
	t.Helper()
	if n := c.count() - base; n != 1 {
		t.Fatalf("%d Verbindungsversuche, erwartet genau 1", n)
	}
	if want := fmt.Sprintf("id-%d/%s", side, wantPassword); c.last() != want {
		t.Errorf("Verbindung %q, erwartet %q", c.last(), want)
	}
	fresh := panelRecon(t, m, side)
	if fresh == old {
		t.Fatal("altes Dateisystem noch im Fenster")
	}
	if n := old.closes.Load(); n != 1 {
		t.Errorf("altes Dateisystem %d-mal geschlossen, erwartet 1", n)
	}
	if fresh.closes.Load() != 0 {
		t.Error("neues Dateisystem geschlossen")
	}
	if got := m.fm.panels[side].path; got != wantPath {
		t.Errorf("Pfad = %q, erwartet %q", got, wantPath)
	}
	if want := reconnectedText(m, side); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
		t.Errorf("Dialog %v / state %v", m.fm.dialog, m.state)
	}
	if m.fm.panels[side].loading {
		t.Error("Fenster noch im Zustand 'lädt'")
	}
	return fresh
}

// --- Auslöser ---

// TestReconnectOnRescanReplacesLostConnection - Strg+R auf einem Fenster mit
// bekannt verlorener Verbindung (links und rechts): genau ein Verbindungs-
// versuch ohne Passwort, danach steht das neue Dateisystem im Fenster, der
// Pfad bleibt, die Einträge sind frisch gelesen. Das andere Fenster bleibt
// unberührt.
func TestReconnectOnRescanReplacesLostConnection(t *testing.T) {
	for side := 0; side < 2; side++ {
		t.Run(fmt.Sprintf("Seite %d", side), func(t *testing.T) {
			m, c := openReconnectManager(t)
			m.fm.active = side
			old, other := panelRecon(t, m, side), panelRecon(t, m, 1-side)
			base := c.count()
			old.lost.Store(true)
			m = pressRun(t, m, "ctrl+r")
			fresh := assertReconnected(t, m, c, base, side, old, "/home/kurt", "")
			if names := entryNames(m.fm.panels[side]); len(names) < 3 || !strings.Contains(strings.Join(names, ","), "projekte") {
				t.Errorf("Einträge nicht frisch gelesen: %v", names)
			}
			if fresh.reads.Load() != 1 {
				t.Errorf("neues Dateisystem %d-mal gelesen, erwartet 1", fresh.reads.Load())
			}
			if panelRecon(t, m, 1-side) != other || other.closes.Load() != 0 {
				t.Error("anderes Fenster angefasst")
			}
		})
	}
}

// TestReconnectOnEnterOpensDirectory - Enter auf ein Verzeichnis bei
// verlorener Verbindung: erst neu verbinden, dann das Verzeichnis lesen. Das
// Fenster zeigt danach den neuen Ordner (nicht den alten Pfad).
func TestReconnectOnEnterOpensDirectory(t *testing.T) {
	m, c := openReconnectManager(t)
	old := panelRecon(t, m, 0)
	base := c.count()
	old.lost.Store(true)
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	assertReconnected(t, m, c, base, 0, old, "/home/kurt/projekte", "")
	if names := entryNames(m.fm.panels[0]); !strings.Contains(strings.Join(names, ","), "main.go") {
		t.Errorf("Inhalt von projekte nicht gelesen: %v", names)
	}
}

// TestReconnectOnReloadKeepsMarks - Das Neuladen nach Kopieren, Löschen oder
// Anlegen (fmReloadPanel) verbindet ebenfalls neu; Cursorziel und
// Markierungen bleiben erhalten.
func TestReconnectOnReloadKeepsMarks(t *testing.T) {
	m, c := openReconnectManager(t)
	old := panelRecon(t, m, 0)
	base := c.count()
	old.lost.Store(true)
	cmd := m.fmReloadPanel(0, "b.txt", 0, map[string]bool{"Notes.txt": true})
	m = runCmd(t, m, cmd)
	assertReconnected(t, m, c, base, 0, old, "/home/kurt", "")
	if got := markedNames(m.fm.panels[0]); len(got) != 1 || got[0] != "Notes.txt" {
		t.Errorf("Markierungen = %v, erwartet [Notes.txt]", got)
	}
	if entry, ok := m.fm.panels[0].current(); !ok || entry.name != "b.txt" {
		t.Errorf("Cursor auf %v, erwartet b.txt", entry.name)
	}
}

// TestReconnectKeepsDeepPath - Steht das Fenster tief in einem Verzeichnis,
// bleibt dieser Pfad nach dem Neu-Verbinden.
func TestReconnectKeepsDeepPath(t *testing.T) {
	m, c := openReconnectManager(t)
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	if m.fm.panels[0].path != "/home/kurt/projekte" {
		t.Fatalf("Pfad = %q", m.fm.panels[0].path)
	}
	old := panelRecon(t, m, 0)
	base := c.count()
	old.lost.Store(true)
	m = pressRun(t, m, "ctrl+r")
	assertReconnected(t, m, c, base, 0, old, "/home/kurt/projekte", "")
	if names := entryNames(m.fm.panels[0]); !strings.Contains(strings.Join(names, ","), "main.go") {
		t.Errorf("Inhalt nicht gelesen: %v", names)
	}
}

// TestReconnectWhenReadBreaksConnection - Die Verbindung ist noch nicht als
// verloren bekannt, reißt aber beim Lesen ab: Das Lesen scheitert, danach ist
// die Verbindung verloren, also genau EIN Verbindungsversuch und erneutes
// Lesen (Strg+R und Enter).
func TestReconnectWhenReadBreaksConnection(t *testing.T) {
	for _, key := range []string{"ctrl+r", "enter"} {
		t.Run(key, func(t *testing.T) {
			m, c := openReconnectManager(t)
			old := panelRecon(t, m, 0)
			base := c.count()
			readsBefore := old.reads.Load()
			old.dieOnRead.Store(true)
			wantPath := "/home/kurt"
			if key == "enter" {
				m = cursorTo(t, m, "projekte")
				wantPath = "/home/kurt/projekte"
			}
			m = pressRun(t, m, key)
			assertReconnected(t, m, c, base, 0, old, wantPath, "")
			if n := old.reads.Load() - readsBefore; n != 1 {
				t.Errorf("altes Dateisystem %d-mal gelesen, erwartet 1 (der gescheiterte Versuch)", n)
			}
		})
	}
}

// --- Wo nicht neu verbunden wird ---

// TestNoReconnectWhenConnectionIsHealthy - Gesunde Verbindung: Strg+R, Enter,
// Neuladen und ein Lesefehler aus anderem Grund (keine Rechte) rufen
// fmConnect nie auf; das Dateisystem bleibt dasselbe und ungeschlossen.
func TestNoReconnectWhenConnectionIsHealthy(t *testing.T) {
	m, c := openReconnectManager(t)
	fsys := panelRecon(t, m, 0)
	base := c.count()
	m = pressRun(t, m, "ctrl+r")
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	m = runCmd(t, m, m.fmReloadPanel(0, "", 0, nil))
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	m = cursorTo(t, m, "geheim")
	m = pressRun(t, m, "enter")
	if want := m.fmDirError("/home/kurt/geheim", os.ErrPermission); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	if c.count() != base {
		t.Errorf("%d Verbindungsversuche bei gesunder Verbindung", c.count()-base)
	}
	if panelRecon(t, m, 0) != fsys || fsys.closes.Load() != 0 {
		t.Error("Dateisystem ausgetauscht oder geschlossen")
	}
}

// TestNoReconnectWithoutConnectionWatcher - Ein Dateisystem ohne
// connectionLost() (fakeRemoteFS pur, wie der lokale PC) gilt nie als
// verloren: kein fmConnect-Aufruf, auch bei Lesefehlern.
func TestNoReconnectWithoutConnectionWatcher(t *testing.T) {
	m, c := openReconnectManager(t)
	base := c.count()
	plain := newFakeFS()
	m.fm.panels[0].fs = plain
	m = pressRun(t, m, "ctrl+r")
	m = cursorTo(t, m, "geheim")
	m = pressRun(t, m, "enter")
	if c.count() != base || m.fm.dialog != fmDialogNone {
		t.Errorf("%d Verbindungsversuche, Dialog %v", c.count()-base, m.fm.dialog)
	}
}

// TestLocalPCNeverReconnects - Der lokale PC hat keine Verbindung, die abreißen
// könnte: selbst wenn sein Dateisystem "verloren" meldet, wird weder fmConnect
// noch fmLocalConnect aufgerufen und kein Dialog geöffnet.
func TestLocalPCNeverReconnects(t *testing.T) {
	m, c := openReconnectManager(t)
	base := c.count()
	var localCalls atomic.Int32
	m.fmLocalConnect = func() (remoteFS, error) {
		localCalls.Add(1)
		return newReconnFS(), nil
	}
	m.fm.panels[0].conn = Connection{ID: fmLocalConnID, Name: "Lokaler PC"}
	lost := newReconnFS()
	lost.lost.Store(true)
	m.fm.panels[0].fs = lost
	m = pressRun(t, m, "ctrl+r")
	m = pressRun(t, m, "ctrl+r")
	if c.count() != base || localCalls.Load() != 0 {
		t.Errorf("fmConnect %d-mal, fmLocalConnect %d-mal aufgerufen", c.count()-base, localCalls.Load())
	}
	if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
		t.Errorf("Dialog %v / state %v", m.fm.dialog, m.state)
	}
	if lost.closes.Load() != 0 {
		t.Error("Dateisystem des lokalen PCs geschlossen")
	}
}

// --- Passwort-Dialog ---

// openReconnectDialog bringt den Datei-Manager in den Passwort-Dialog: Die
// Verbindung der Seite ist verloren und die Anmeldung scheitert (authFailure).
// Liefert Modell, Connector und die Zahl der Verbindungen davor.
func openReconnectDialog(t *testing.T, side int, script func(n int, conn Connection, password string) (remoteFS, error)) (AppModel, *reconnectConnector, int) {
	t.Helper()
	m, c := openReconnectManager(t)
	m.fm.active = side
	base := c.count()
	if script == nil {
		script = func(int, Connection, string) (remoteFS, error) { return nil, authFailure }
	}
	c.setScript(script)
	panelRecon(t, m, side).lost.Store(true)
	m = pressRun(t, m, "ctrl+r")
	if m.fm.dialog != fmDialogReconnect {
		t.Fatalf("Dialog = %v, erwartet fmDialogReconnect (Meldung %q)", m.fm.dialog, m.fm.message)
	}
	return m, c, base
}

// TestReconnectAskForPasswordWhenLoginFails - Scheitert die Anmeldung beim
// Neu-Verbinden (kein Netzwerk-, Host-Key- oder SFTP-Fehler), öffnet sich der
// Passwort-Dialog für die richtige Seite. Der Datei-Manager bleibt in der
// Fensteransicht, die alte Verbindung bleibt im Fenster (nicht geschlossen),
// der Dialog nennt Titel, Verbindung und user@host und zeigt das Passwort
// maskiert.
func TestReconnectAskForPasswordWhenLoginFails(t *testing.T) {
	for side := 0; side < 2; side++ {
		t.Run(fmt.Sprintf("Seite %d", side), func(t *testing.T) {
			m, c, base := openReconnectDialog(t, side, nil)
			if m.fm.reconnectSide != side {
				t.Errorf("reconnectSide = %d, erwartet %d", m.fm.reconnectSide, side)
			}
			if c.count()-base != 1 || c.last() != fmt.Sprintf("id-%d/", side) {
				t.Errorf("Verbindungen: %v", c.calls)
			}
			if m.state != ViewFileManager {
				t.Errorf("state = %v, erwartet ViewFileManager", m.state)
			}
			if old := panelRecon(t, m, side); old.closes.Load() != 0 {
				t.Error("alte Verbindung beim gescheiterten Versuch geschlossen")
			}
			conn := m.fm.panels[side].conn
			text := stripANSI(m.View())
			for _, want := range []string{m.tr(txtFMReconnectTitle), sanitizeFileName(conn.Name), conn.User + "@" + conn.Host} {
				if !strings.Contains(text, want) {
					t.Errorf("Dialog ohne %q:\n%s", want, text)
				}
			}
			m = typeText(t, m, "zq9-pass")
			if got := m.fm.passwordInput.Value(); got != "zq9-pass" {
				t.Errorf("Feldinhalt = %q", got)
			}
			if text := stripANSI(m.View()); strings.Contains(text, "zq9-pass") {
				t.Errorf("Passwort im Klartext sichtbar:\n%s", text)
			}
			assertScreenSize(t, m)
		})
	}
}

// TestReconnectPasswordSuccess - Enter mit Passwort: der Dialog schließt sofort,
// der Datei-Manager bleibt in der Fensteransicht, das Feld ist geleert (das
// Passwort steht nur noch im laufenden Kommando), fmConnect wird genau
// einmal mit diesem Passwort aufgerufen. Danach ist neu verbunden; das
// Passwort steht nirgends mehr (nicht im Feld, nicht in der Konfiguration).
func TestReconnectPasswordSuccess(t *testing.T) {
	const secret = "zq9-secret"
	m, c, base := openReconnectDialog(t, 0, func(n int, conn Connection, password string) (remoteFS, error) {
		if password == secret {
			return nil, nil
		}
		return nil, authFailure
	})
	old := panelRecon(t, m, 0)
	m = typeText(t, m, secret)
	updated, cmd := m.Update(fmKey("enter"))
	m = updated.(AppModel)
	if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
		t.Fatalf("nach Enter: Dialog %v, state %v", m.fm.dialog, m.state)
	}
	if got := m.fm.passwordInput.Value(); got != "" {
		t.Errorf("Passwortfeld nach Enter nicht geleert: %q", got)
	}
	if cmd == nil {
		t.Fatal("kein Kommando zum Verbinden")
	}
	m = runCmd(t, m, cmd)
	if n := c.count() - base; n != 2 {
		t.Fatalf("%d Verbindungsversuche seit dem Öffnen, erwartet 2 (Auto-Auth + Passwort)", n)
	}
	if c.last() != "id-0/"+secret {
		t.Errorf("letzter Aufruf %q, erwartet mit Passwort", c.last())
	}
	if n := strings.Count(strings.Join(c.calls, "\n"), secret); n != 1 {
		t.Errorf("Passwort in %d Aufrufen, erwartet 1", n)
	}
	fresh := panelRecon(t, m, 0)
	if fresh == old || old.closes.Load() != 1 {
		t.Errorf("Fenster nicht auf die neue Verbindung umgestellt (alt %d-mal geschlossen)", old.closes.Load())
	}
	if m.fm.message != reconnectedText(m, 0) || m.fm.dialog != fmDialogNone {
		t.Errorf("Meldung %q, Dialog %v", m.fm.message, m.fm.dialog)
	}
	if m.fm.passwordInput.Value() != "" {
		t.Error("Passwort nach dem Verbinden noch im Feld")
	}
	if cfg, err := os.ReadFile(m.configPath); err != nil || strings.Contains(string(cfg), secret) {
		t.Errorf("Passwort in der Konfiguration (Lesefehler: %v)", err)
	}
	if strings.Contains(stripANSI(m.View()), secret) {
		t.Error("Passwort auf dem Bildschirm")
	}
}

// TestReconnectWrongPasswordAsksAgain - Ein falsches Passwort führt zurück in
// den Dialog mit der Meldung "Anmeldung fehlgeschlagen"; das Feld ist leer,
// die Seite dieselbe, die alte Verbindung bleibt ungeschlossen. Danach
// klappt ein zweiter Versuch.
func TestReconnectWrongPasswordAsksAgain(t *testing.T) {
	m, c, base := openReconnectDialog(t, 1, func(n int, conn Connection, password string) (remoteFS, error) {
		if password == "richtig" {
			return nil, nil
		}
		return nil, authFailure
	})
	old := panelRecon(t, m, 1)
	m = typeText(t, m, "falsch")
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogReconnect || m.fm.reconnectSide != 1 {
		t.Fatalf("Dialog %v, Seite %d; erwartet erneut der Dialog für Seite 1", m.fm.dialog, m.fm.reconnectSide)
	}
	if m.fm.message != m.tr(txtFMWrongPassword) {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, m.tr(txtFMWrongPassword))
	}
	if m.fm.passwordInput.Value() != "" {
		t.Errorf("Feld nicht geleert: %q", m.fm.passwordInput.Value())
	}
	if old.closes.Load() != 0 || panelRecon(t, m, 1) != old {
		t.Error("alte Verbindung nach falschem Passwort ersetzt oder geschlossen")
	}
	if n := c.count() - base; n != 2 {
		t.Errorf("%d Versuche, erwartet 2", n)
	}
	assertScreenSize(t, m)
	m = typeText(t, m, "richtig")
	m = pressRun(t, m, "enter")
	assertReconnected(t, m, c, base+2, 1, old, "/home/kurt", "richtig")
}

// TestReconnectDialogEscAndEmptyEnter - Esc schließt den Dialog ohne
// Verbindungsversuch (Fenster und alte Verbindung unverändert, Feld
// geleert); Enter mit leerem Feld tut nichts (kein Versuch, Dialog bleibt).
func TestReconnectDialogEscAndEmptyEnter(t *testing.T) {
	m, c, base := openReconnectDialog(t, 0, nil)
	old := panelRecon(t, m, 0)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogReconnect || c.count()-base != 1 {
		t.Errorf("leeres Enter: Dialog %v, %d Versuche", m.fm.dialog, c.count()-base)
	}
	m = typeText(t, m, "abc")
	m = pressRun(t, m, "esc")
	if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
		t.Errorf("nach Esc: Dialog %v, state %v", m.fm.dialog, m.state)
	}
	if c.count()-base != 1 {
		t.Errorf("Esc löste einen Verbindungsversuch aus (%d)", c.count()-base)
	}
	if m.fm.passwordInput.Value() != "" {
		t.Errorf("Feld nach Esc nicht geleert: %q", m.fm.passwordInput.Value())
	}
	if panelRecon(t, m, 0) != old || old.closes.Load() != 0 {
		t.Error("Fenster nach Esc verändert")
	}
	// Normale Bedienung geht danach weiter (Strg+R versucht es erneut, genau einmal)
	m = pressRun(t, m, "ctrl+r")
	if c.count()-base != 2 || m.fm.dialog != fmDialogReconnect {
		t.Errorf("nach Esc und Strg+R: %d Versuche, Dialog %v", c.count()-base, m.fm.dialog)
	}
}

// TestReconnectDialogQIsPasswordCharacter - "q" beendet den Datei-Manager
// sonst immer, im Passwortfeld ist es ein normales Zeichen (auch in
// Großschreibung und mitten im Passwort).
func TestReconnectDialogQIsPasswordCharacter(t *testing.T) {
	m, _, _ := openReconnectDialog(t, 0, nil)
	m = typeText(t, m, "aqQb")
	if m.fm.dialog != fmDialogReconnect || m.state != ViewFileManager {
		t.Fatalf("Dialog %v / state %v nach \"q\"", m.fm.dialog, m.state)
	}
	if got := m.fm.passwordInput.Value(); got != "aqQb" {
		t.Errorf("Feldinhalt = %q, erwartet aqQb", got)
	}
}

// TestReconnectDialogKeepsFocusOnOtherKeys - Während der Dialog offen ist,
// wirken die Tasten der Fenster nicht (Tab wechselt das Fenster nicht, F10 und
// F8 öffnen nichts, der Datei-Manager bleibt offen).
func TestReconnectDialogKeepsFocusOnOtherKeys(t *testing.T) {
	m, _, _ := openReconnectDialog(t, 0, nil)
	for _, key := range []string{"tab", "f8", "f5", "f10"} {
		updated, _ := m.Update(fmKey(key))
		m = updated.(AppModel)
		if m.fm.dialog != fmDialogReconnect || m.state != ViewFileManager || m.fm.active != 0 {
			t.Fatalf("%s: Dialog %v, state %v, aktives Fenster %d", key, m.fm.dialog, m.state, m.fm.active)
		}
	}
}

// --- Fehler beim Neu-Verbinden ---

// TestReconnectNetworkErrorOnlyMessage - Ein Netzwerkfehler führt zu einer
// Meldung mit Namen und (bereinigtem) Fehlertext, ohne Dialog. Das alte
// Dateisystem bleibt im Fenster, ungeschlossen. Jedes weitere Strg+R versucht
// es erneut - genau einmal je Aktion, nichts von selbst. Der Fehlertext
// stammt vom Server und darf kein Steuerzeichen enthalten.
func TestReconnectNetworkErrorOnlyMessage(t *testing.T) {
	m, c := openReconnectManager(t)
	old := panelRecon(t, m, 0)
	base := c.count()
	failure := errors.New("dial tcp 10.0.0.1:22: connect: connection refused\x1b[2J")
	c.setScript(func(int, Connection, string) (remoteFS, error) { return nil, failure })
	old.lost.Store(true)
	for attempt := 1; attempt <= 3; attempt++ {
		m = pressRun(t, m, "ctrl+r")
		want := fmt.Sprintf(m.tr(txtFMReconnectFailed), "Links", sanitizeFileName(failure.Error()))
		if m.fm.message != want {
			t.Fatalf("Versuch %d: Meldung = %q, erwartet %q", attempt, m.fm.message, want)
		}
		if strings.ContainsRune(m.fm.message, 0x1b) {
			t.Errorf("Steuerzeichen in der Meldung: %q", m.fm.message)
		}
		if got := c.count() - base; got != attempt {
			t.Fatalf("Versuch %d: %d Verbindungsaufrufe, erwartet genau %d", attempt, got, attempt)
		}
		if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
			t.Errorf("Dialog %v / state %v bei Netzwerkfehler", m.fm.dialog, m.state)
		}
		if panelRecon(t, m, 0) != old || old.closes.Load() != 0 {
			t.Error("altes Dateisystem ersetzt oder geschlossen")
		}
	}
	// Das Netz ist wieder da: der nächste Versuch gelingt
	c.setScript(nil)
	m = pressRun(t, m, "ctrl+r")
	assertReconnected(t, m, c, base+3, 0, old, "/home/kurt", "")
}

// TestReconnectHostKeyChangedAndSFTPError - Geänderter Host-Key: Meldung wie
// beim Öffnen (txtFMErrHostKey mit dem Host), kein Dialog, keine Passwortfrage
// (ein Passwort würde hier nichts helfen). Ein SFTP-Fehler ebenso: nur Meldung.
func TestReconnectHostKeyChangedAndSFTPError(t *testing.T) {
	cases := map[string]error{
		"Host-Key": &HostKeyChangedError{Hostname: "10.0.0.1"},
		"SFTP":     &fmSFTPError{err: errors.New("subsystem request failed")},
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			m, c := openReconnectManager(t)
			old := panelRecon(t, m, 0)
			base := c.count()
			c.setScript(func(int, Connection, string) (remoteFS, error) { return nil, failure })
			old.lost.Store(true)
			m = pressRun(t, m, "ctrl+r")
			if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
				t.Fatalf("Dialog %v / state %v", m.fm.dialog, m.state)
			}
			if c.count()-base != 1 || m.fm.message == "" {
				t.Errorf("%d Versuche, Meldung %q", c.count()-base, m.fm.message)
			}
			if name == "Host-Key" {
				if want := fmt.Sprintf(m.tr(txtFMErrHostKey), "10.0.0.1"); m.fm.message != want {
					t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
				}
			}
			if panelRecon(t, m, 0) != old || old.closes.Load() != 0 {
				t.Error("altes Dateisystem ersetzt oder geschlossen")
			}
		})
	}
}

// TestReconnectAtMostOneAttemptPerAction - Auch wenn die neue Verbindung
// sofort wieder abreißt (das Lesen scheitert und sie ist verloren), gibt es
// keinen zweiten Verbindungsversuch in derselben Aktion und keine
// Endlosschleife; erst die nächste Aktion verbindet erneut.
func TestReconnectAtMostOneAttemptPerAction(t *testing.T) {
	for _, key := range []string{"ctrl+r", "enter"} {
		t.Run(key, func(t *testing.T) {
			m, c := openReconnectManager(t)
			old := panelRecon(t, m, 0)
			base := c.count()
			c.setScript(func(int, Connection, string) (remoteFS, error) {
				f := newReconnFS()
				f.dieOnRead.Store(true)
				return f, nil
			})
			old.lost.Store(true)
			if key == "enter" {
				m = cursorTo(t, m, "projekte")
			}
			m = pressRun(t, m, key)
			if n := c.count() - base; n != 1 {
				t.Fatalf("%d Verbindungsversuche in einer Aktion, erwartet 1", n)
			}
			if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
				t.Errorf("Dialog %v / state %v", m.fm.dialog, m.state)
			}
			m = pressRun(t, m, "ctrl+r")
			if n := c.count() - base; n != 2 {
				t.Errorf("%d Verbindungsversuche nach der zweiten Aktion, erwartet 2", n)
			}
		})
	}
}

// --- Veraltete Ergebnisse ---

// TestReconnectStaleResultClosesNewConnection - Kommt das Ergebnis eines
// Neu-Verbindens an, nachdem der Datei-Manager geschlossen oder neu geöffnet
// wurde (andere Sitzung), wird die neue Verbindung geschlossen statt zu
// verfallen (sonst bliebe ein offener SSH-Zugang). Das Fenster bleibt
// unverändert.
func TestReconnectStaleResultClosesNewConnection(t *testing.T) {
	t.Run("andere Sitzung", func(t *testing.T) {
		m, _ := openReconnectManager(t)
		old := panelRecon(t, m, 0)
		fresh := newReconnFS()
		updated, _ := m.Update(fmDirLoadedMsg{session: m.fm.session + 1, side: 0, path: "/home/kurt", newFS: fresh})
		m = updated.(AppModel)
		if fresh.closes.Load() != 1 {
			t.Errorf("neue Verbindung %d-mal geschlossen, erwartet 1", fresh.closes.Load())
		}
		if panelRecon(t, m, 0) != old || old.closes.Load() != 0 {
			t.Error("Fenster durch veraltetes Ergebnis verändert")
		}
	})
	t.Run("Datei-Manager geschlossen", func(t *testing.T) {
		m, _ := openReconnectManager(t)
		session := m.fm.session
		m = press(t, m, "q")
		if m.state == ViewFileManager {
			t.Fatalf("Datei-Manager nach q noch offen")
		}
		fresh := newReconnFS()
		updated, _ := m.Update(fmDirLoadedMsg{session: session, side: 0, path: "/home/kurt", newFS: fresh})
		m = updated.(AppModel)
		if fresh.closes.Load() != 1 {
			t.Errorf("neue Verbindung %d-mal geschlossen, erwartet 1", fresh.closes.Load())
		}
		if m.state == ViewFileManager {
			t.Error("veraltetes Ergebnis öffnete den Datei-Manager wieder")
		}
	})
}

// TestReconnectCompletedAfterCloseDoesNotShowDialog - Scheitert die Anmeldung,
// aber der Datei-Manager wurde inzwischen geschlossen, öffnet sich kein
// Passwort-Dialog (und nichts stürzt ab).
func TestReconnectCompletedAfterCloseDoesNotShowDialog(t *testing.T) {
	m, _ := openReconnectManager(t)
	session := m.fm.session
	m = press(t, m, "q")
	updated, _ := m.Update(fmDirLoadedMsg{session: session, side: 0, path: "/home/kurt", reconnectErr: authFailure})
	m = updated.(AppModel)
	if m.state == ViewFileManager || m.fm.dialog == fmDialogReconnect {
		t.Errorf("state %v, Dialog %v nach veraltetem Fehler", m.state, m.fm.dialog)
	}
}

// --- Anzeige ---

// TestReconnectDialogScreenSize - Der Passwort-Dialog füllt in allen
// Farbmodi, bei kleinen Terminals und in allen 41 Sprachen genau Höhe mal
// Breite (auch bei 40 Spalten in Chinesisch, Japanisch und Koreanisch: dort
// hat overlayAt früher ein halb verdecktes breites Zeichen übrig gelassen);
// Titel und Tastenhilfe sind in jeder Sprache vorhanden und enthalten keine
// Formatreste.
func TestReconnectDialogScreenSize(t *testing.T) {
	base, _, _ := openReconnectDialog(t, 0, nil)
	for _, opt := range AvailableLanguages {
		m := base
		m.language, m.lang = opt.Code, GetTranslations(opt.Code)
		for _, key := range []textKey{txtFMReconnectTitle, txtFMReconnectHelp, txtFMReconnected} {
			if text := m.tr(key); strings.TrimSpace(text) == "" || strings.Contains(text, "%!") {
				t.Errorf("%s: Text %q kaputt: %q", opt.Code, key, text)
			}
		}
		prompt := fmt.Sprintf(m.tr(txtFMReconnectPrompt), "Links", "root@10.0.0.1")
		if strings.Contains(prompt, "%!") || !strings.Contains(prompt, "Links") || !strings.Contains(prompt, "root@10.0.0.1") {
			t.Errorf("%s: Text txtFMReconnectPrompt ergibt %q", opt.Code, prompt)
		}
		for _, size := range [][2]int{{100, 30}, {40, 12}} {
			m.termWidth, m.termHeight = size[0], size[1]
			assertScreenSize(t, m)
		}
	}
	for theme := range fmThemes {
		for _, size := range [][2]int{{100, 30}, {80, 25}, {40, 12}, {20, 8}} {
			m := base
			m.fm.theme = theme
			m.termWidth, m.termHeight = size[0], size[1]
			assertScreenSize(t, m)
		}
	}
	m := base
	m.fm.panels[0].conn.Name = "Server\x1b[2J\u202e"
	m.fm.panels[0].conn.User = "ro\x07ot"
	if text := m.View(); strings.ContainsRune(text, 0x07) || strings.Contains(text, "\x1b[2J") {
		t.Error("Steuerzeichen aus Verbindungsname oder Benutzer im Dialog")
	}
}

// --- Verlust erst nach dem Fehler gemeldet (Build 90, Nachrunde) ---

// newWatchedSFTP baut einen sftpFS mit Überwachung von Hand (wie newSFTPFS:
// lost, lostCh und markLost, das genau einmal markiert). Der Client fehlt;
// lostAfter braucht ihn nicht. mark meldet den Verlust (wie die Goroutine
// nach sftpClient.Wait()).
func newWatchedSFTP() (fsys *sftpFS, mark func()) {
	lost := &atomic.Bool{}
	ch := make(chan struct{})
	var once sync.Once
	mark = func() {
		once.Do(func() {
			lost.Store(true)
			close(ch)
		})
	}
	return &sftpFS{lost: lost, lostCh: ch, markLost: mark}, mark
}

// TestSFTPLostAfterJudgesErrors - sftpFS.lostAfter bewertet den Fehler einer
// Anfrage, damit schon das ERSTE Strg+R neu verbindet (die Überwachung setzt
// das Verlust-Flag erst kurz NACH dem Fehler):
//   - sftp.ErrSSHFxConnectionLost (auch eingewickelt): sofort ja und als
//     verloren markiert
//   - Antwort des Servers (*sftp.StatusError, nicht vorhanden, keine Rechte):
//     die Verbindung lebt, nein - OHNE zu warten (sonst hakt jedes
//     Zugriff-verweigert eine Sekunde)
//   - EOF, unerwartetes EOF, Netzfehler: bis fmLostGrace auf die Überwachung
//     warten; kommt die Meldung, ja, sonst nein (nach etwa fmLostGrace)
//   - nil: nein; schon verloren: ja, egal welcher Fehler
//   - ohne Überwachung (kein lostCh): nein, ohne Warten und ohne Absturz
func TestSFTPLostAfterJudgesErrors(t *testing.T) {
	defer func(g time.Duration) { fmLostGrace = g }(fmLostGrace)
	fmLostGrace = 400 * time.Millisecond
	quick := fmLostGrace / 2
	status := &sftp.StatusError{Code: 3}
	for _, tc := range []struct {
		name    string
		err     error
		preLost bool          // schon vorher als verloren bekannt
		lateAt  time.Duration // Überwachung meldet den Verlust nach dieser Zeit (0 = nie)
		want    bool
		minWait time.Duration
		maxWait time.Duration
		flag    bool // connectionLost() danach
	}{
		{name: "ErrSSHFxConnectionLost", err: sftp.ErrSSHFxConnectionLost, want: true, maxWait: quick, flag: true},
		{name: "ErrSSHFxConnectionLost eingewickelt", err: fmt.Errorf("readdir /x: %w", sftp.ErrSSHFxConnectionLost), want: true, maxWait: quick, flag: true},
		{name: "StatusError", err: status, want: false, maxWait: quick},
		{name: "StatusError im PathError", err: &os.PathError{Op: "readdir", Path: "/x", Err: status}, want: false, maxWait: quick},
		{name: "nicht vorhanden", err: os.ErrNotExist, want: false, maxWait: quick},
		{name: "keine Rechte", err: os.ErrPermission, want: false, maxWait: quick},
		{name: "keine Rechte im PathError", err: &os.PathError{Op: "open", Path: "/x", Err: os.ErrPermission}, want: false, maxWait: quick},
		{name: "EOF, Verlust nach 20 ms", err: io.EOF, lateAt: 20 * time.Millisecond, want: true, minWait: 15 * time.Millisecond, maxWait: quick, flag: true},
		{name: "unerwartetes EOF, Verlust nach 20 ms", err: io.ErrUnexpectedEOF, lateAt: 20 * time.Millisecond, want: true, minWait: 15 * time.Millisecond, maxWait: quick, flag: true},
		{name: "Netzfehler, Verlust nach 20 ms", err: &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, lateAt: 20 * time.Millisecond, want: true, minWait: 15 * time.Millisecond, maxWait: quick, flag: true},
		{name: "EOF ohne Verlust", err: io.EOF, want: false, minWait: fmLostGrace * 8 / 10, maxWait: fmLostGrace * 3},
		{name: "Verlust erst nach der Wartezeit", err: io.EOF, lateAt: fmLostGrace * 2, want: false, minWait: fmLostGrace * 8 / 10, maxWait: fmLostGrace * 3 / 2},
		{name: "nil", err: nil, want: false, maxWait: quick},
		{name: "schon verloren, EOF", err: io.EOF, preLost: true, want: true, maxWait: quick, flag: true},
		{name: "schon verloren, StatusError", err: status, preLost: true, want: true, maxWait: quick, flag: true},
		{name: "schon verloren, nil", err: nil, preLost: true, want: true, maxWait: quick, flag: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys, mark := newWatchedSFTP()
			if tc.preLost {
				mark()
			}
			if tc.lateAt > 0 {
				timer := time.AfterFunc(tc.lateAt, mark)
				defer timer.Stop()
			}
			start := time.Now()
			got := fsys.lostAfter(tc.err)
			elapsed := time.Since(start)
			if got != tc.want {
				t.Errorf("lostAfter(%v) = %v, erwartet %v", tc.err, got, tc.want)
			}
			if elapsed < tc.minWait || elapsed > tc.maxWait {
				t.Errorf("wartete %v, erwartet zwischen %v und %v", elapsed, tc.minWait, tc.maxWait)
			}
			if fsys.connectionLost() != tc.flag {
				t.Errorf("connectionLost() = %v, erwartet %v", fsys.connectionLost(), tc.flag)
			}
		})
	}
	// Ohne Überwachung (z. B. ein von Hand gebauter sftpFS): nie warten, nie
	// verloren, kein Absturz - auch bei ErrSSHFxConnectionLost (markLost fehlt)
	for _, tc := range []struct {
		name string
		fsys *sftpFS
	}{
		{"ohne lostCh", &sftpFS{lost: &atomic.Bool{}}},
		{"ganz leer", &sftpFS{}},
	} {
		for _, err := range []error{io.EOF, sftp.ErrSSHFxConnectionLost, nil} {
			t.Run(fmt.Sprintf("%s/%v", tc.name, err), func(t *testing.T) {
				start := time.Now()
				if tc.fsys.lostAfter(err) {
					t.Error("lostAfter = true ohne Überwachung")
				}
				if elapsed := time.Since(start); elapsed > quick {
					t.Errorf("wartete %v ohne Überwachung", elapsed)
				}
			})
		}
	}
}

// judgeFS ist ein Dateisystem mit eigener Bewertung (connectionLossJudge).
type judgeFS struct {
	*fakeRemoteFS
	verdict bool
	calls   atomic.Int32
}

func (j *judgeFS) lostAfter(err error) bool {
	j.calls.Add(1)
	return j.verdict
}

// TestFsLostAfterDispatch - fsLostAfter fragt zuerst die Bewertung des
// Dateisystems (connectionLossJudge); ohne sie gilt das Flag
// (connectionLost); ein Dateisystem ohne beides ist nie verloren.
func TestFsLostAfterDispatch(t *testing.T) {
	plain := newFakeFS()
	if fsLostAfter(plain, io.EOF) {
		t.Error("Dateisystem ohne Überwachung gilt als verloren")
	}
	flagged := newReconnFS()
	if fsLostAfter(flagged, io.EOF) {
		t.Error("gesundes Dateisystem gilt als verloren")
	}
	flagged.lost.Store(true)
	if !fsLostAfter(flagged, io.EOF) {
		t.Error("verlorenes Dateisystem (nur Flag) gilt als gesund")
	}
	yes, no := &judgeFS{fakeRemoteFS: newFakeFS(), verdict: true}, &judgeFS{fakeRemoteFS: newFakeFS()}
	if !fsLostAfter(yes, io.EOF) || yes.calls.Load() != 1 {
		t.Errorf("Bewertung 'verloren' nicht übernommen (Aufrufe %d)", yes.calls.Load())
	}
	if fsLostAfter(no, io.EOF) || no.calls.Load() != 1 {
		t.Errorf("Bewertung 'lebt' nicht übernommen (Aufrufe %d)", no.calls.Load())
	}
	// Die echte Bewertung des sftpFS wird benutzt, nicht das Flag allein
	real, mark := newWatchedSFTP()
	timer := time.AfterFunc(20*time.Millisecond, mark)
	defer timer.Stop()
	if !fsLostAfter(real, io.EOF) {
		t.Error("sftpFS: späte Meldung des Verlusts nicht abgewartet")
	}
}

// lateLossFS ist ein Dateisystem, dessen Lesen mit EOF scheitert und das den
// Verlust erst SPÄTER meldet - wie sftpFS, dessen Überwachung das Flag nach
// sftpClient.Wait() setzt. Beim Fehler ist connectionLost() noch false;
// lostAfter wartet auf die Meldung (neverLost: wartet kurz und verneint).
type lateLossFS struct {
	*reconnFS
	delay     time.Duration
	neverLost bool
	breaking  atomic.Bool
	judged    atomic.Int32
	lostCh    chan struct{}
	once      sync.Once
}

func newLateLossFS(base *reconnFS, delay time.Duration) *lateLossFS {
	return &lateLossFS{reconnFS: base, delay: delay, lostCh: make(chan struct{})}
}

func (f *lateLossFS) markLost() {
	f.once.Do(func() {
		f.reconnFS.lost.Store(true)
		close(f.lostCh)
	})
}

func (f *lateLossFS) ReadDir(p string) ([]os.FileInfo, error) {
	if f.breaking.Load() {
		f.reconnFS.reads.Add(1)
		if !f.neverLost {
			time.AfterFunc(f.delay, f.markLost)
		}
		return nil, io.EOF
	}
	return f.reconnFS.ReadDir(p)
}

func (f *lateLossFS) lostAfter(err error) bool {
	f.judged.Add(1)
	select {
	case <-f.lostCh:
		return true
	case <-time.After(f.delay + 50*time.Millisecond):
		return f.connectionLost()
	}
}

// TestReconnectFirstCtrlRAfterLateLossReport - Das Lesen scheitert mit EOF, die
// Überwachung meldet den Verlust erst 30 ms SPÄTER (connectionLost() ist beim
// Fehler noch false). Schon das ERSTE Strg+R (und Enter) verbindet neu: genau
// ein fmConnect, Pfad bleibt, Erfolgsmeldung. Vor dem Fix (fsConnectionLost
// statt fsLostAfter) war erst das zweite Strg+R erfolgreich.
func TestReconnectFirstCtrlRAfterLateLossReport(t *testing.T) {
	for _, key := range []string{"ctrl+r", "enter"} {
		t.Run(key, func(t *testing.T) {
			m, c := openReconnectManager(t)
			old := panelRecon(t, m, 0)
			late := newLateLossFS(old, 30*time.Millisecond)
			m.fm.panels[0].fs = late
			base := c.count()
			late.breaking.Store(true)
			wantPath := "/home/kurt"
			if key == "enter" {
				m = cursorTo(t, m, "projekte")
				wantPath = "/home/kurt/projekte"
			}
			start := time.Now()
			m = pressRun(t, m, key)
			elapsed := time.Since(start)
			assertReconnected(t, m, c, base, 0, old, wantPath, "")
			if elapsed < 25*time.Millisecond {
				t.Errorf("nach %v neu verbunden, die Meldung kam aber erst nach 30 ms", elapsed)
			}
			if late.judged.Load() != 1 {
				t.Errorf("Bewertung %d-mal gefragt, erwartet 1", late.judged.Load())
			}
		})
	}
}

// TestReconnectReadErrorWithoutLossDoesNotReconnect - Gegenstück: Das Lesen
// scheitert mit EOF, aber die Überwachung meldet keinen Verlust (nur ein
// einzelner Fehler). Nach der Wartezeit kein Neu-Verbinden: nichts wird
// geschlossen oder ersetzt, die Fehlermeldung bleibt, fmConnect nie gerufen.
func TestReconnectReadErrorWithoutLossDoesNotReconnect(t *testing.T) {
	m, c := openReconnectManager(t)
	old := panelRecon(t, m, 0)
	late := newLateLossFS(old, 30*time.Millisecond)
	late.neverLost = true
	m.fm.panels[0].fs = late
	base := c.count()
	late.breaking.Store(true)
	m = pressRun(t, m, "ctrl+r")
	if c.count() != base {
		t.Errorf("%d Verbindungsversuche ohne Verlust", c.count()-base)
	}
	if old.closes.Load() != 0 || m.fm.panels[0].fs != remoteFS(late) {
		t.Error("Dateisystem trotz gesunder Verbindung ersetzt oder geschlossen")
	}
	if m.fm.message == "" || m.fm.message == reconnectedText(m, 0) {
		t.Errorf("Meldung = %q, erwartet einen Lesefehler", m.fm.message)
	}
	if m.fm.dialog != fmDialogNone {
		t.Errorf("Dialog %v ohne Verlust", m.fm.dialog)
	}
}

// TestReconnectFailedNeverOverOpenDialogEditorViewerOrMenu - Scheitert die
// Anmeldung beim Neu-Verbinden (Ergebnis kommt asynchron), während etwas
// anderes offen ist, öffnet sich KEIN Passwortdialog darüber: Dialog (auch
// ein anderer Passwortdialog), Editor, Betrachter oder Menü. Es erscheint nur
// die Meldung txtFMReconnectFailed (mit Namen und bereinigtem Fehlertext); der
// bestehende Zustand bleibt unverändert (Dialog, Eingabe, Seite, Dateisystem
// unberührt). Ohne etwas Offenes öffnet sich der Dialog (Kontrolle).
func TestReconnectFailedNeverOverOpenDialogEditorViewerOrMenu(t *testing.T) {
	failure := fmt.Errorf("%w\x1b[2J", authFailure)
	deliver := func(m AppModel) AppModel {
		updated, _ := m.Update(fmDirLoadedMsg{session: m.fm.session, side: 0, path: "/home/kurt", reconnectErr: failure})
		return updated.(AppModel)
	}
	t.Run("nichts offen: Dialog (Kontrolle)", func(t *testing.T) {
		m, _ := openReconnectManager(t)
		m = deliver(m)
		if m.fm.dialog != fmDialogReconnect || m.fm.reconnectSide != 0 {
			t.Errorf("Dialog %v, Seite %d, erwartet fmDialogReconnect für Seite 0", m.fm.dialog, m.fm.reconnectSide)
		}
	})
	for _, tc := range []struct {
		name string
		open func(t *testing.T, m AppModel) AppModel
		same func(t *testing.T, before, after AppModel)
	}{
		{"Verzeichnis anlegen (F7) mit Eingabe", func(t *testing.T, m AppModel) AppModel {
			m = pressRun(t, m, "f7")
			return typeText(t, m, "neu")
		}, func(t *testing.T, before, after AppModel) {
			if after.fm.dialog != fmDialogMkdir || after.fm.mkdirInput.Value() != "neu" {
				t.Errorf("Dialog %v, Eingabe %q, erwartet fmDialogMkdir mit \"neu\"", after.fm.dialog, after.fm.mkdirInput.Value())
			}
		}},
		{"Löschen bestätigen (F8)", func(t *testing.T, m AppModel) AppModel {
			m = cursorTo(t, m, "b.txt")
			return pressRun(t, m, "f8")
		}, func(t *testing.T, before, after AppModel) {
			if after.fm.dialog != fmDialogDeleteConfirm {
				t.Errorf("Dialog %v, erwartet fmDialogDeleteConfirm", after.fm.dialog)
			}
		}},
		{"Kopieren bestätigen (F5)", func(t *testing.T, m AppModel) AppModel {
			m = cursorTo(t, m, "b.txt")
			return pressRun(t, m, "f5")
		}, func(t *testing.T, before, after AppModel) {
			if after.fm.dialog != fmDialogCopyConfirm {
				t.Errorf("Dialog %v, erwartet fmDialogCopyConfirm", after.fm.dialog)
			}
		}},
		{"anderer Passwortdialog (rechts)", func(t *testing.T, m AppModel) AppModel {
			m.fm.dialog, m.fm.reconnectSide = fmDialogReconnect, 1
			m.fm.passwordInput = createPasswordInput()
			m.fm.passwordInput.Focus()
			m.fm.passwordInput.SetValue("tippt-gerade")
			return m
		}, func(t *testing.T, before, after AppModel) {
			if after.fm.dialog != fmDialogReconnect || after.fm.reconnectSide != 1 || after.fm.passwordInput.Value() != "tippt-gerade" {
				t.Errorf("Dialog %v, Seite %d, Eingabe %q: der laufende Dialog wurde gestört", after.fm.dialog, after.fm.reconnectSide, after.fm.passwordInput.Value())
			}
		}},
		{"Menü (F9)", func(t *testing.T, m AppModel) AppModel {
			m = pressRun(t, m, "f9")
			if !m.fm.menuOpen {
				t.Fatal("Menü nicht offen")
			}
			return m
		}, func(t *testing.T, before, after AppModel) {
			if !after.fm.menuOpen {
				t.Error("Menü geschlossen")
			}
		}},
		{"Editor", func(t *testing.T, m AppModel) AppModel {
			m.fm.editing = true
			return m
		}, func(t *testing.T, before, after AppModel) {
			if !after.fm.editing {
				t.Error("Editor geschlossen")
			}
		}},
		{"Betrachter", func(t *testing.T, m AppModel) AppModel {
			m.fm.viewing = true
			return m
		}, func(t *testing.T, before, after AppModel) {
			if !after.fm.viewing {
				t.Error("Betrachter geschlossen")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := openReconnectManager(t)
			old := panelRecon(t, m, 0)
			m = tc.open(t, m)
			before := m
			m = deliver(m)
			if m.fm.dialog == fmDialogReconnect && before.fm.dialog != fmDialogReconnect {
				t.Fatalf("Passwortdialog über %s geöffnet", tc.name)
			}
			if m.fm.dialog != before.fm.dialog || m.fm.editing != before.fm.editing || m.fm.viewing != before.fm.viewing || m.fm.menuOpen != before.fm.menuOpen {
				t.Errorf("Zustand verändert: Dialog %v->%v, Editor %v->%v, Betrachter %v->%v, Menü %v->%v",
					before.fm.dialog, m.fm.dialog, before.fm.editing, m.fm.editing, before.fm.viewing, m.fm.viewing, before.fm.menuOpen, m.fm.menuOpen)
			}
			tc.same(t, before, m)
			want := fmt.Sprintf(m.tr(txtFMReconnectFailed), "Links", sanitizeFileName(failure.Error()))
			if m.fm.message != want {
				t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
			}
			if strings.ContainsRune(m.fm.message, 0x1b) {
				t.Errorf("Steuerzeichen in der Meldung: %q", m.fm.message)
			}
			if panelRecon(t, m, 0) != old || old.closes.Load() != 0 || m.fm.panels[0].loading {
				t.Error("Fenster verändert (Dateisystem ersetzt, geschlossen oder noch am Laden)")
			}
		})
	}
}
