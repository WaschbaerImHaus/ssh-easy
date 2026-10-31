// Paket main - Tests für AutoDeployKey und RemoveDeployedKey gegen einen
// SSH-Server im Testprozess (Build 86)
//
// Hintergrund: Beide Abläufe laufen unter autoDeployMu. Bisher prüften die Tests
// nur die Entscheidungen ohne Verbindung (client=nil). Hier gibt es einen echten
// SSH-Server auf 127.0.0.1:0, der jeden exec-Befehl mit Exit 0 beantwortet und
// Befehl samt stdin mitschreibt. So sind auch die Server-Befehle prüfbar und
// die Zeitspanne zwischen "Konfiguration lesen" und "UpdateConnection" ist
// durch die Antwortverzögerung des Servers groß genug, dass fehlende Sperren
// sicher auffallen.
//   - Der Client nutzt ssh.FixedHostKey (nie InsecureIgnoreHostKey).
//   - HOME/USERPROFILE zeigen auf ein Temp-Verzeichnis (setTestHome).
//   - Die Tests laufen nie mit t.Parallel() (setzen Umgebungsvariablen).
//
// Build 86, Nachbesserung nach dem Fable-Review: Der Server hat einen zweiten
// Modus (startShellSSHServer). Er führt den empfangenen exec-Befehl wirklich
// mit /bin/sh -c aus (HOME = Temp-Verzeichnis des "Servers", stdin
// weitergereicht, Exit-Code zurück). Damit sind authorized_keys-Inhalt und
// -Rechte prüfbar (F4: keine Duplikate) - nur unter Unix, sonst t.Skip.
// F6: AutoDeployKey/RemoveDeployedKey ändern in der Konfiguration nur
// AuthType, KeyPath und UpdatedAt.
//
// @author Kurt Ingwer
// @date   2026-10-28 13:09
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"
)

// execRecord ist ein am Testserver eingegangener exec-Befehl samt stdin.
type execRecord struct {
	Command string
	Stdin   string
}

// testSSHServer ist ein SSH-Server im Testprozess. Er nimmt jede Passwort-
// Anmeldung an, beantwortet exec-Requests nach der optionalen Verzögerung mit
// Exit 0 und schreibt Befehle und stdin unter einem Mutex mit.
type testSSHServer struct {
	Addr    string
	HostKey ssh.PublicKey

	mu       sync.Mutex
	execs    []execRecord
	delay    time.Duration
	listener net.Listener
	conns    []net.Conn
	// shellHome ungleich "": exec-Befehle werden wirklich mit /bin/sh -c
	// ausgeführt, mit HOME = shellHome (siehe startShellSSHServer)
	shellHome string
	// pathPrefix ungleich "": Verzeichnis, das beim Ausführen vor PATH steht.
	// Damit lässt sich das "sh" im Befehl "sh -c '...'" austauschen (z. B.
	// durch bash), siehe startShellSSHServerWithSh
	pathPrefix string
	// failStatus ungleich 0: jeder exec-Befehl endet mit diesem Exit-Code,
	// ohne ausgeführt zu werden (siehe startFailingSSHServer)
	failStatus uint32
}

// startTestSSHServer startet den Server auf 127.0.0.1:0 und räumt per
// t.Cleanup auf (Listener und alle offenen Verbindungen schließen).
//
// @param t     - Testkontext
// @param delay - Wartezeit vor der exec-Antwort (0 = sofort); vergrößert die
//
//	Zeitspanne, in der nicht gesperrte Abläufe sich überlappen
//
// @return *testSSHServer - Laufender Server
// @date   2026-10-27 19:46
func startTestSSHServer(t *testing.T, delay time.Duration) *testSSHServer {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Host-Key erzeugen: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("Signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		// Jede Anmeldung wird angenommen: Geprüft wird nicht die Auth,
		// sondern was AutoDeployKey/RemoveDeployedKey danach tun.
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := &testSSHServer{Addr: ln.Addr().String(), HostKey: signer.PublicKey(), delay: delay, listener: ln}
	t.Cleanup(func() {
		_ = ln.Close()
		srv.mu.Lock()
		for _, c := range srv.conns {
			_ = c.Close()
		}
		srv.mu.Unlock()
	})

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			srv.mu.Lock()
			srv.conns = append(srv.conns, nc)
			srv.mu.Unlock()
			go srv.serve(nc, cfg)
		}
	}()
	return srv
}

// serve bedient eine Verbindung: nur Session-Kanäle, nur exec.
//
// @param nc  - Angenommene TCP-Verbindung
// @param cfg - Server-Konfiguration (Host-Key, Anmeldung)
// @date   2026-10-28 13:09
func (s *testSSHServer) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "nur session")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, chReqs)
	}
}

// handleSession beantwortet exec: stdin bis EOF lesen, Befehl mitschreiben,
// nach der Verzögerung Exit-Status senden und den Kanal schließen. Ohne
// shellHome ist der Status immer 0; mit shellHome kommt der Exit-Code der
// echten Shell (runShellCommand).
//
// @param ch   - Session-Kanal
// @param reqs - Anfragen des Kanals
// @date   2026-10-28 13:09
func (s *testSSHServer) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		go func(command string) {
			stdin, _ := io.ReadAll(ch)
			s.mu.Lock()
			s.execs = append(s.execs, execRecord{Command: command, Stdin: string(stdin)})
			shellHome := s.shellHome
			pathPrefix := s.pathPrefix
			failStatus := s.failStatus
			s.mu.Unlock()
			if s.delay > 0 {
				time.Sleep(s.delay)
			}
			status := failStatus
			if shellHome != "" {
				status = runShellCommand(command, shellHome, pathPrefix, stdin)
			}
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			_ = ch.Close()
		}(payload.Command)
	}
}

// runShellCommand führt einen exec-Befehl mit /bin/sh -c aus: HOME und
// Arbeitsverzeichnis sind home, stdin kommt aus dem Argument, sonst erbt die
// Shell nur PATH. Die Ausgabe wird verworfen.
//
// @param command    - Befehlstext wie vom SSH-Client gesendet
// @param home       - HOME des "Servers" (Temp-Verzeichnis)
// @param pathPrefix - optionales Verzeichnis vor PATH ("" = keins)
// @param stdin      - Eingabe für den Befehl
// @return uint32 - Exit-Code (127, wenn die Shell nicht startete)
// @date   2026-10-28 13:09
func runShellCommand(command, home, pathPrefix string, stdin []byte) uint32 {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = home
	path := os.Getenv("PATH")
	if pathPrefix != "" {
		path = pathPrefix + string(os.PathListSeparator) + path
	}
	cmd.Env = []string{"HOME=" + home, "PATH=" + path}
	cmd.Stdin = bytes.NewReader(stdin)
	_, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return uint32(exitErr.ExitCode())
	}
	return 127
}

// startShellSSHServer startet den Testserver im Shell-Modus: exec-Befehle
// laufen wirklich in /bin/sh, mit HOME = einem frischen Temp-Verzeichnis (dem
// "Home auf dem Server", getrennt vom lokalen Test-Home). Unter Windows oder
// ohne sh wird der Test übersprungen.
//
// @param t - Testkontext
// @return *testSSHServer - Laufender Server
// @return string - HOME des "Servers" (dort liegt .ssh/authorized_keys)
// @date   2026-10-28 13:09
func startShellSSHServer(t *testing.T) (*testSSHServer, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Shell-Modus des Testservers nur unter Unix")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh nicht gefunden")
	}
	srv := startTestSSHServer(t, 0)
	remoteHome := t.TempDir()
	srv.mu.Lock()
	srv.shellHome = remoteHome
	srv.mu.Unlock()
	return srv, remoteHome
}

// startShellSSHServerWithSh startet den Shell-Server so, dass das "sh" INNERHALB
// des Befehls ("sh -c '...'", wie es deployKeyScript und removeKeyScript
// senden) die gewünschte Shell ist. Der äußere Aufruf bleibt /bin/sh. Auf
// diesem Rechner ist sh dash; viele Server haben bash als sh, und beide
// verhalten sich bei einer fehlgeschlagenen Umleitung verschieden (dash: Exit
// 2, bash: Exit 1). Ohne Namen (shell == "") gilt das sh des Systems. Fehlt die
// Shell, wird der Test übersprungen.
//
// @param t     - Testkontext
// @param shell - Programmname im PATH (z. B. "bash") oder ""
// @return *testSSHServer - Laufender Server
// @return string - HOME des "Servers"
// @date   2026-10-30 09:00
func startShellSSHServerWithSh(t *testing.T, shell string) (*testSSHServer, string) {
	t.Helper()
	srv, remoteHome := startShellSSHServer(t)
	if shell == "" {
		return srv, remoteHome
	}
	target, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s nicht gefunden", shell)
	}
	shimDir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(shimDir, "sh")); err != nil {
		t.Skipf("Symlink nicht möglich: %v", err)
	}
	srv.mu.Lock()
	srv.pathPrefix = shimDir
	srv.mu.Unlock()
	return srv, remoteHome
}

// startFailingSSHServer startet den Testserver im Fehlermodus: Jeder
// exec-Befehl wird mitgeschrieben, aber nicht ausgeführt, und endet mit dem
// Exit-Code status (z. B. 1 = "authorized_keys nicht beschreibbar"). Damit
// scheitert das Hinterlegen des Schlüssels, während die SSH-Verbindung selbst
// steht.
//
// @param t      - Testkontext
// @param status - Exit-Code der Befehle (ungleich 0)
// @return *testSSHServer - Laufender Server
// @date   2026-10-28 14:00
func startFailingSSHServer(t *testing.T, status uint32) *testSSHServer {
	t.Helper()
	srv := startTestSSHServer(t, 0)
	srv.mu.Lock()
	srv.failStatus = status
	srv.mu.Unlock()
	return srv
}

// Execs liefert eine Kopie aller bisher eingegangenen Befehle.
//
// @return []execRecord - Befehle in der Reihenfolge ihres Eingangs
// @date   2026-10-28 13:09
func (s *testSSHServer) Execs() []execRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]execRecord(nil), s.execs...)
}

// Reset vergisst alle bisher eingegangenen Befehle.
//
// @date   2026-10-28 13:09
func (s *testSSHServer) Reset() {
	s.mu.Lock()
	s.execs = nil
	s.mu.Unlock()
}

// dialTestSSHServer verbindet sich mit dem Testserver. Der Host-Key wird
// fest vorgegeben (ssh.FixedHostKey), nie InsecureIgnoreHostKey.
//
// @param t   - Testkontext
// @param srv - Testserver
// @return *ssh.Client - Verbundener Client (per t.Cleanup geschlossen)
// @date   2026-10-27 19:46
func dialTestSSHServer(t *testing.T, srv *testSSHServer) *ssh.Client {
	t.Helper()
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.Password("egal")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKey),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Verbindung zum Testserver: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// passwordConn erstellt eine Verbindung mit Passwort-Auth und fester ID.
//
// @param id   - Feste Verbindungs-ID
// @param host - Hostname
// @param user - Benutzername
// @return Connection - Verbindung mit AuthPassword, Port 22
// @date   2026-10-28 13:09
func passwordConn(id, host, user string) Connection {
	conn := NewConnection("Verbindung "+id, host, 22, user, AuthPassword)
	conn.ID = id
	return conn
}

// readFileString liest eine Datei als String (bricht den Test bei Fehler ab).
//
// @param t    - Testkontext
// @param path - Dateipfad
// @return string - Dateiinhalt
// @date   2026-10-28 13:09
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Datei %s nicht lesbar: %v", path, err)
	}
	return string(data)
}

// TestAutoDeployKeyConcurrentUniqueNames prüft die gleichzeitige Vergabe:
// 8 Verbindungen mit gleichem Host und Benutzer rufen gleichzeitig
// AutoDeployKey auf. Erwartet: acht verschiedene Pfade (Grundname, _2 … _8,
// lückenlos), jede Datei samt .pub existiert, jeder Config-Eintrag hat AuthKey
// und genau seinen KeyPath, und der Server bekam acht Hinterlegungen mit
// jeweils dem eigenen Public Key. Ohne autoDeployMu wählen alle denselben
// Namen (Server verzögert 30 ms, damit sich die Läufe sicher überlappen).
// Zusätzlich mit -race zu prüfen.
//
// @date 2026-10-27 19:46
func TestAutoDeployKeyConcurrentUniqueNames(t *testing.T) {
	const n = 8
	home := setTestHome(t)
	srv := startTestSSHServer(t, 30*time.Millisecond)
	client := dialTestSSHServer(t, srv)

	conns := make([]Connection, n)
	for i := range conns {
		conns[i] = passwordConn(fmt.Sprintf("id-%d", i), "srv.example.org", "admin")
	}
	cfgPath := writeKeyConfig(t, conns...)

	paths := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // alle gleichzeitig loslassen
			paths[i], errs[i] = AutoDeployKey(conns[i], client, cfgPath)
		}(i)
	}
	close(start)
	wg.Wait()

	base := "~/.ssh/id_ed25519_srv.example.org_admin"
	want := []string{base}
	for i := 2; i <= n; i++ {
		want = append(want, fmt.Sprintf("%s_%d", base, i))
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("AutoDeployKey #%d: %v", i, err)
		}
	}
	got := append([]string(nil), paths...)
	sort.Strings(got)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	if strings.Join(got, "\n") != strings.Join(sortedWant, "\n") {
		t.Fatalf("Pfade nicht eindeutig/lückenlos:\n got  %v\n want %v", got, sortedWant)
	}

	// Jede Verbindung: AuthKey und der zurückgegebene Pfad in der Konfiguration;
	// Dateien auf der Platte; .pub gehört zum Server-Befehl.
	execs := srv.Execs()
	if len(execs) != n {
		t.Errorf("Server bekam %d Befehle, erwartet %d", len(execs), n)
	}
	stdins := map[string]bool{}
	for _, e := range execs {
		stdins[e.Stdin] = true
	}
	for i, c := range conns {
		saved := findConn(t, cfgPath, c.ID)
		if saved.AuthType != AuthKey || saved.KeyPath != paths[i] {
			t.Errorf("%s: AuthType=%q KeyPath=%q, erwartet AuthKey und %q", c.ID, saved.AuthType, saved.KeyPath, paths[i])
		}
		abs := filepath.Join(home, paths[i][2:])
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("%s: Schlüsseldatei fehlt: %v", c.ID, err)
		}
		if pub := readFileString(t, abs+".pub"); !stdins[pub] {
			t.Errorf("%s: Public Key %q wurde nie an den Server geschickt", c.ID, pub)
		}
	}
	if len(stdins) != n {
		t.Errorf("Server bekam %d verschiedene Public Keys, erwartet %d", len(stdins), n)
	}
}

// TestAutoDeployKeyAgainReusesFile prüft die erneute Vergabe derselben
// Verbindung (z. B. nach erneuter Passwort-Anmeldung): gleicher Name, die
// vorhandene Datei wird wiederverwendet, Inhalt von Datei und .pub vorher und
// nachher gleich (kein neuer Schlüssel, kein "Datei existiert bereits"-Fehler).
// Der Server bekommt beide Male denselben Public Key.
//
// @date 2026-10-27 19:46
func TestAutoDeployKeyAgainReusesFile(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)

	first, err := AutoDeployKey(conn, client, cfgPath)
	if err != nil {
		t.Fatalf("erste Vergabe: %v", err)
	}
	abs := filepath.Join(home, first[2:])
	privBefore, pubBefore := readFileString(t, abs), readFileString(t, abs+".pub")

	// Wieder mit dem ursprünglichen Eintrag (Passwort-Anmeldung), nicht dem gespeicherten.
	second, err := AutoDeployKey(conn, client, cfgPath)
	if err != nil {
		t.Fatalf("zweite Vergabe: %v", err)
	}
	if second != first {
		t.Errorf("Name geändert: %q statt %q", second, first)
	}
	if got := readFileString(t, abs); got != privBefore {
		t.Error("privater Schlüssel wurde neu erzeugt")
	}
	if got := readFileString(t, abs+".pub"); got != pubBefore {
		t.Error(".pub wurde neu erzeugt")
	}
	execs := srv.Execs()
	if len(execs) != 2 || execs[0].Stdin != pubBefore || execs[1].Stdin != pubBefore {
		t.Errorf("Server-Befehle: %+v, erwartet zweimal den vorhandenen Public Key", execs)
	}
	if saved := findConn(t, cfgPath, "me"); saved.KeyPath != first || saved.AuthType != AuthKey {
		t.Errorf("Konfiguration: %+v", saved)
	}
	// Keine zusätzliche Schlüsseldatei (_2) angelegt.
	entries, _ := os.ReadDir(filepath.Join(home, ".ssh"))
	if len(entries) != 2 {
		t.Errorf("%d Dateien in ~/.ssh, erwartet 2 (Schlüssel + .pub)", len(entries))
	}
}

// TestRemoveDeployedKeyConcurrentShared prüft das gleichzeitige Entfernen
// durch Quelle und Kopie mit geteiltem Schlüssel über mehrere Runden. Ohne
// Sperre sähe jede Seite die andere noch als Nutzer und niemand löschte die
// Dateien. Erwartet: beide Dateien gelöscht, beide Verbindungen auf Passwort
// mit leerem KeyPath. Zwei Varianten: anderer Host (jede Seite schickt einen
// Server-Befehl, die Verzögerung öffnet das Fenster weit) und gleicher Host
// (Server-Eintrag bleibt zunächst stehen; der letzte Nutzer entfernt ihn).
//
// @date 2026-10-27 19:46
func TestRemoveDeployedKeyConcurrentShared(t *testing.T) {
	home := setTestHome(t)
	variants := []struct {
		label     string
		copyHost  string
		wantExecs int // Server-Befehle pro Runde
	}{
		{"anderer Host", "other.example.org", 2},
		{"gleicher Host", "srv.example.org", 1},
	}
	for _, v := range variants {
		srv := startTestSSHServer(t, 30*time.Millisecond)
		client := dialTestSSHServer(t, srv)
		for round := 0; round < 5; round++ {
			priv, pub := writeTestKeyPair(t, home)
			src := keyConn("src-id", "srv.example.org", "admin", priv)
			cpy := keyConn("copy-id", v.copyHost, "admin", "~/.ssh/id_test")
			cfgPath := writeKeyConfig(t, src, cpy)
			srv.Reset()

			errs := make([]error, 2)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i, c := range []Connection{src, cpy} {
				wg.Add(1)
				go func(i int, c Connection) {
					defer wg.Done()
					<-start
					errs[i] = RemoveDeployedKey(c, client, cfgPath)
				}(i, c)
			}
			close(start)
			wg.Wait()

			for i, err := range errs {
				if err != nil {
					t.Fatalf("%s, Runde %d: RemoveDeployedKey #%d: %v", v.label, round, i, err)
				}
			}
			for _, p := range []string{priv, pub} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s, Runde %d: %s müsste gelöscht sein (Fehler: %v)", v.label, round, filepath.Base(p), err)
				}
			}
			for _, id := range []string{"src-id", "copy-id"} {
				got := findConn(t, cfgPath, id)
				if got.KeyPath != "" || got.AuthType != AuthPassword {
					t.Errorf("%s, Runde %d: %s nicht zurückgesetzt: KeyPath=%q AuthType=%q", v.label, round, id, got.KeyPath, got.AuthType)
				}
			}
			// Gleicher Host: Der erste Entferner behält den Server-Eintrag, der zweite
			// (letzte Nutzer) entfernt ihn. Anderer Host: beide entfernen je ihren.
			if n := len(srv.Execs()); n != v.wantExecs {
				t.Errorf("%s, Runde %d: %d Server-Befehle, erwartet %d", v.label, round, n, v.wantExecs)
			}
		}
	}
}

// TestAutoDeployKeyBrokenConfigAborts prüft den Abbruch bei defekter
// Konfiguration: Fehler, keine Datei unter ~/.ssh (auch kein Verzeichnis-
// inhalt), kein exec am Server und die defekte Datei bleibt unverändert.
// Ohne lesbare Liste wäre unbekannt, ob der Name schon vergeben ist; ein
// Rückfall auf den Grundnamen würde Schlüssel doppelt vergeben.
//
// @date 2026-10-27 19:46
func TestAutoDeployKeyBrokenConfigAborts(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	cfgPath := filepath.Join(t.TempDir(), "connections.json")
	broken := []byte("{ das ist kein JSON ")
	if err := os.WriteFile(cfgPath, broken, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, err := AutoDeployKey(passwordConn("me", "srv.example.org", "admin"), client, cfgPath)
	if err == nil {
		t.Fatalf("kein Fehler bei defekter Konfiguration (Pfad %q)", path)
	}
	if path != "" {
		t.Errorf("Pfad %q trotz Fehler", path)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".ssh")); len(entries) != 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("Dateien unter ~/.ssh trotz Abbruch: %v", names)
	}
	if execs := srv.Execs(); len(execs) != 0 {
		t.Errorf("Server bekam Befehle trotz Abbruch: %+v", execs)
	}
	if got := readFileString(t, cfgPath); got != string(broken) {
		t.Errorf("defekte Konfiguration wurde verändert: %q", got)
	}
}

// TestRemoveDeployedKeyKeepsServerEntry prüft, wann der authorized_keys-Eintrag
// auf dem Server bleibt. Geteilter Schlüssel mit gleichem Host (andere
// Schreibweise, anderer Port) und gleichem Benutzer: KEIN grep -vF-Befehl, die
// lokalen Dateien bleiben. Anderer Host oder anderer Benutzer: der Befehl
// kommt genau einmal und trägt den Base64-Teil des Keys als stdin (sonst
// bliebe ein Schlüssel auf einem Server zurück, den keine Verbindung mehr nutzt).
//
// @date 2026-10-27 19:46
func TestRemoveDeployedKeyKeepsServerEntry(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	const keyData = "AAAAC3NzaC1lZDI1NTE5AAAAItestkey"

	cases := []struct {
		label     string
		otherHost string
		otherUser string
		otherPort int
		wantExecs int
	}{
		{"gleicher Host, andere Schreibweise, anderer Port", "SRV.Example.ORG", "admin", 2222, 0},
		{"gleicher Host, gleicher Port", "srv.example.org", "admin", 22, 0},
		{"anderer Host", "other.example.org", "admin", 22, 1},
		{"gleicher Host, anderer Benutzer", "srv.example.org", "root", 22, 1},
	}
	for _, c := range cases {
		priv, pub := writeTestKeyPair(t, home)
		mine := keyConn("me", "srv.example.org", "admin", "~/.ssh/id_test")
		other := keyConn("other", c.otherHost, c.otherUser, priv)
		other.Port = c.otherPort
		cfgPath := writeKeyConfig(t, mine, other)
		srv.Reset()

		if err := RemoveDeployedKey(mine, client, cfgPath); err != nil {
			t.Fatalf("%s: RemoveDeployedKey: %v", c.label, err)
		}
		execs := srv.Execs()
		if len(execs) != c.wantExecs {
			t.Errorf("%s: %d Server-Befehle, erwartet %d: %+v", c.label, len(execs), c.wantExecs, execs)
		}
		for _, e := range execs {
			if !strings.Contains(e.Command, "grep -vF") || e.Stdin != keyData {
				t.Errorf("%s: unerwarteter Befehl %+v (erwartet grep -vF mit stdin %q)", c.label, e, keyData)
			}
		}
		// Geteilter Schlüssel: lokale Dateien bleiben in jedem Fall.
		for _, p := range []string{priv, pub} {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s: %s fehlt: %v", c.label, filepath.Base(p), err)
			}
		}
		if got := findConn(t, cfgPath, "me"); got.KeyPath != "" || got.AuthType != AuthPassword {
			t.Errorf("%s: Verbindung nicht zurückgesetzt: %+v", c.label, got)
		}
	}
}

// TestAutoDeployKeySendsPublicKeyViaStdin prüft das Hinterlegen: Der Befehl
// hängt an ~/.ssh/authorized_keys an, und der Public Key kommt über stdin
// (kein Shell-Escaping, kein Key im Befehlstext). Der empfangene stdin
// entspricht der .pub-Datei, nur mit genau einem Zeilenende.
//
// @date 2026-10-27 19:46
func TestAutoDeployKeySendsPublicKeyViaStdin(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)

	keyPath, err := AutoDeployKey(conn, client, cfgPath)
	if err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	pub := readFileString(t, filepath.Join(home, keyPath[2:])+".pub")
	execs := srv.Execs()
	if len(execs) != 1 {
		t.Fatalf("%d Server-Befehle, erwartet 1: %+v", len(execs), execs)
	}
	e := execs[0]
	if !strings.Contains(e.Command, "cat >> ~/.ssh/authorized_keys") {
		t.Errorf("Befehl hängt nicht an authorized_keys an: %q", e.Command)
	}
	keyBody := strings.Fields(pub)[1]
	if strings.Contains(e.Command, keyBody) {
		t.Errorf("Key steht im Befehlstext statt in stdin: %q", e.Command)
	}
	if want := strings.TrimSpace(pub) + "\n"; e.Stdin != want {
		t.Errorf("stdin = %q, erwartet %q", e.Stdin, want)
	}
	if !strings.HasPrefix(e.Stdin, "ssh-ed25519 ") || strings.Count(e.Stdin, "\n") != 1 || !strings.HasSuffix(e.Stdin, "\n") {
		t.Errorf("stdin ist keine einzelne Schlüsselzeile: %q", e.Stdin)
	}
}

// --- F4: authorized_keys ohne Duplikate (Server führt Befehle wirklich aus) ---

// remoteAuthKeysPath liefert den Pfad der authorized_keys im "Home" des
// Shell-Servers.
//
// @param remoteHome - HOME des Servers (aus startShellSSHServer)
// @return string - <remoteHome>/.ssh/authorized_keys
// @date   2026-10-28 13:09
func remoteAuthKeysPath(remoteHome string) string {
	return filepath.Join(remoteHome, ".ssh", "authorized_keys")
}

// writeRemoteAuthKeys legt auf dem "Server" ~/.ssh (0700) und eine
// authorized_keys mit genau dem übergebenen Inhalt an (0600). Der Inhalt wird
// nicht verändert, auch ein fehlendes Zeilenende am Schluss bleibt fehlen.
//
// @param t          - Testkontext
// @param remoteHome - HOME des Servers
// @param content    - Dateiinhalt
// @date   2026-10-28 13:09
func writeRemoteAuthKeys(t *testing.T, remoteHome, content string) {
	t.Helper()
	dir := filepath.Join(remoteHome, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(remoteAuthKeysPath(remoteHome), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// remoteLines liefert die Zeilen der authorized_keys auf dem "Server" (ohne
// das Zeilenende am Schluss; leere Datei: keine Zeilen).
//
// @param t          - Testkontext
// @param remoteHome - HOME des Servers
// @return []string - Zeilen in Dateireihenfolge
// @date   2026-10-28 13:09
func remoteLines(t *testing.T, remoteHome string) []string {
	t.Helper()
	content := readFileString(t, remoteAuthKeysPath(remoteHome))
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// countLinesWith zählt die Zeilen, die text enthalten.
//
// @param lines - Zeilen
// @param text  - Gesuchter Text (hier: Base64-Teil eines Schlüssels)
// @return int - Anzahl der Zeilen mit text
// @date   2026-10-28 13:09
func countLinesWith(lines []string, text string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, text) {
			n++
		}
	}
	return n
}

// precreateAutoKey legt das Schlüsselpaar vorab unter dem Namen an, den
// AutoDeployKey für Host und Benutzer wählt (id_ed25519_<host>_<user>). So
// ist der Base64-Teil vor der Vergabe bekannt; AutoDeployKey verwendet die
// vorhandene Datei wieder (siehe TestAutoDeployKeyAgainReusesFile).
//
// @param t    - Testkontext (setTestHome muss schon gerufen sein)
// @param host - Hostname der Verbindung
// @param user - Benutzer der Verbindung
// @return string - Base64-Teil des öffentlichen Schlüssels
// @return string - Komplette Zeile der .pub-Datei (ohne Zeilenende)
// @date   2026-10-28 13:09
func precreateAutoKey(t *testing.T, host, user string) (string, string) {
	t.Helper()
	pub, err := GenerateSSHKey("~/.ssh/id_ed25519_"+host+"_"+user, "")
	if err != nil {
		t.Fatalf("GenerateSSHKey: %v", err)
	}
	pub = strings.TrimSpace(pub)
	return strings.Fields(pub)[1], pub
}

// TestAutoDeployKeyTwiceStoresKeyOnce (F4): Zweimal AutoDeployKey für dieselbe
// Verbindung (z. B. erneute Passwort-Anmeldung) gegen einen Server, der den
// Befehl wirklich ausführt. Erwartet: Der Base64-Teil steht genau EINMAL in
// authorized_keys und die Datei hat genau eine Zeile. Rot heute: `cat >>`
// hängt den Schlüssel bei jeder Vergabe erneut an (2 Zeilen).
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyTwiceStoresKeyOnce(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)

	for i := 1; i <= 2; i++ {
		if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
			t.Fatalf("Vergabe %d: %v", i, err)
		}
	}
	saved := findConn(t, cfgPath, "me")
	pub := readFileString(t, expandTilde(saved.KeyPath)+".pub")
	b64 := strings.Fields(pub)[1]
	lines := remoteLines(t, remoteHome)
	if n := countLinesWith(lines, b64); n != 1 {
		t.Errorf("Schlüssel steht %d-mal in authorized_keys, erwartet genau 1:\n%s", n, strings.Join(lines, "\n"))
	}
	if len(lines) != 1 {
		t.Errorf("authorized_keys hat %d Zeilen, erwartet 1: %q", len(lines), lines)
	}
}

// TestAutoDeployKeyKeepsForeignLines (F4): Vorhandene fremde Zeilen (auch ein
// Kommentar und eine Zeile mit Optionen) bleiben bei der Vergabe unverändert
// und in derselben Reihenfolge; der eigene Schlüssel kommt genau einmal ans
// Ende. Nach einer zweiten Vergabe ändert sich nichts mehr. Rot heute: Die
// zweite Vergabe hängt eine zweite Kopie an.
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyKeepsForeignLines(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	b64, ownLine := precreateAutoKey(t, "srv.example.org", "admin")
	foreign := []string{
		"# Schlüssel der Buchhaltung",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIforeignAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA kollege@pc",
		`command="/bin/true",no-pty ssh-rsa AAAAB3NzaC1yc2EAAAADAQABforeignRSA admin@alt`,
	}
	writeRemoteAuthKeys(t, remoteHome, strings.Join(foreign, "\n")+"\n")

	for i := 1; i <= 2; i++ {
		if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
			t.Fatalf("Vergabe %d: %v", i, err)
		}
		lines := remoteLines(t, remoteHome)
		if len(lines) != len(foreign)+1 {
			t.Fatalf("nach Vergabe %d: %d Zeilen, erwartet %d:\n%s", i, len(lines), len(foreign)+1, strings.Join(lines, "\n"))
		}
		if !reflect.DeepEqual(lines[:len(foreign)], foreign) {
			t.Errorf("nach Vergabe %d: fremde Zeilen verändert:\n got  %q\n want %q", i, lines[:len(foreign)], foreign)
		}
		if lines[len(foreign)] != ownLine {
			t.Errorf("nach Vergabe %d: letzte Zeile %q, erwartet %q", i, lines[len(foreign)], ownLine)
		}
		if n := countLinesWith(lines, b64); n != 1 {
			t.Errorf("nach Vergabe %d: Schlüssel %d-mal, erwartet 1", i, n)
		}
	}
}

// TestAutoDeployKeyForeignFileWithoutFinalNewline (F4, Randfall): Die
// vorhandene authorized_keys endet ohne Zeilenende (von Hand bearbeitet).
// Der eigene Schlüssel muss in einer EIGENEN Zeile stehen; die letzte fremde
// Zeile darf nicht mit ihm verklebt werden (sie wäre sonst ungültig und der
// fremde Schlüssel unbrauchbar). Rot heute: `cat >>` klebt die Zeilen
// zusammen.
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyForeignFileWithoutFinalNewline(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	_, ownLine := precreateAutoKey(t, "srv.example.org", "admin")
	foreign := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIeins eins@pc",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIzwei zwei@pc",
	}
	writeRemoteAuthKeys(t, remoteHome, strings.Join(foreign, "\n")) // ohne Zeilenende

	if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	want := append(append([]string(nil), foreign...), ownLine)
	if got := remoteLines(t, remoteHome); !reflect.DeepEqual(got, want) {
		t.Errorf("authorized_keys:\n got  %q\n want %q", got, want)
	}
}

// TestAutoDeployKeyCreatesAuthorizedKeys (F4): Gibt es weder ~/.ssh noch
// authorized_keys, legt die Vergabe beides an: Verzeichnis 0700, Datei 0600,
// Inhalt genau die Schlüsselzeile mit einem Zeilenende. Ein Verzeichnis
// ~/.ssh mit 0755 und eine Datei mit 0666 werden bei der ersten Vergabe auf
// 0700 bzw. 0600 gesetzt. Läuft heute grün (Schutz gegen Rückschritt bei der
// Umstellung auf den Duplikat-Test).
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyCreatesAuthorizedKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Dateirechte nur unter Unix prüfbar")
	}
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)

	if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	pub := strings.TrimSpace(readFileString(t, expandTilde(findConn(t, cfgPath, "me").KeyPath)+".pub"))
	dirInfo, err := os.Stat(filepath.Join(remoteHome, ".ssh"))
	if err != nil {
		t.Fatalf("~/.ssh nicht angelegt: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("~/.ssh hat Rechte %o, erwartet 700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(remoteAuthKeysPath(remoteHome))
	if err != nil {
		t.Fatalf("authorized_keys nicht angelegt: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys hat Rechte %o, erwartet 600", fileInfo.Mode().Perm())
	}
	if got := readFileString(t, remoteAuthKeysPath(remoteHome)); got != pub+"\n" {
		t.Errorf("authorized_keys = %q, erwartet %q", got, pub+"\n")
	}

	// Zu offene Rechte einer vorhandenen Installation werden bei der Vergabe
	// korrigiert (wie bisher durch chmod 700/600).
	other := passwordConn("zweit", "zweit.example.org", "admin")
	cfgPath2 := writeKeyConfig(t, other)
	if err := os.Chmod(filepath.Join(remoteHome, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(remoteAuthKeysPath(remoteHome), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := AutoDeployKey(other, client, cfgPath2); err != nil {
		t.Fatalf("zweite Vergabe: %v", err)
	}
	dirInfo, _ = os.Stat(filepath.Join(remoteHome, ".ssh"))
	fileInfo, _ = os.Stat(remoteAuthKeysPath(remoteHome))
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("Rechte nach zweiter Vergabe: .ssh %o, authorized_keys %o; erwartet 700/600",
			dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
	if lines := remoteLines(t, remoteHome); len(lines) != 2 {
		t.Errorf("authorized_keys hat %d Zeilen, erwartet 2 (zwei verschiedene Schlüssel): %q", len(lines), lines)
	}
}

// TestAutoDeployKeyAlreadyPresentOtherComment (F4): Der Schlüssel steht schon
// in authorized_keys, aber mit einem ANDEREN Kommentar (z. B. vom Server-
// Betreiber umbenannt). Verglichen wird nur der Base64-Teil: Es kommt keine
// zweite Zeile, die vorhandene Zeile bleibt mit ihrem Kommentar unverändert.
// Rot heute: Es wird eine zweite Zeile mit dem Originalkommentar angehängt.
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyAlreadyPresentOtherComment(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	b64, ownLine := precreateAutoKey(t, "srv.example.org", "admin")
	renamed := "ssh-ed25519 " + b64 + " vom-betreiber-umbenannt"
	if renamed == ownLine {
		t.Fatal("Testaufbau: Kommentar muss abweichen")
	}
	foreign := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIfremd fremd@pc"
	writeRemoteAuthKeys(t, remoteHome, foreign+"\n"+renamed+"\n")

	if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	want := []string{foreign, renamed}
	if got := remoteLines(t, remoteHome); !reflect.DeepEqual(got, want) {
		t.Errorf("authorized_keys:\n got  %q\n want %q", got, want)
	}
}

// TestAutoDeployKeyComparesAsFixedString (F4, Gegenprobe gegen eine
// Regex-Lösung): Base64 enthält '+', '/' und '='. Eine fremde Zeile, die den
// Schlüssel nur als Muster träfe ("C3+bc" träfe "C3bc"), ist KEIN Treffer:
// Die erste Vergabe hängt den Schlüssel an, die zweite erkennt ihn und lässt
// die Datei in Ruhe. Der Schlüssel ist von Hand gebaut (AutoDeployKey nimmt
// die vorhandene Datei und prüft das Format nicht). Rot heute: zweite Vergabe
// hängt erneut an (3 statt 2 Zeilen).
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyComparesAsFixedString(t *testing.T) {
	home := setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(dir, "id_ed25519_srv.example.org_admin")
	ownLine := "ssh-ed25519 AAAAC3+bc/zz+/== muster"
	if err := os.WriteFile(priv, []byte("PRIVATE-TEST-KEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(priv+".pub", []byte(ownLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lookalike := "ssh-ed25519 AAAAC3bc/zz+/== wuerde-als-regex-passen"
	writeRemoteAuthKeys(t, remoteHome, lookalike+"\n")

	for i := 1; i <= 2; i++ {
		if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
			t.Fatalf("Vergabe %d: %v", i, err)
		}
		want := []string{lookalike, ownLine}
		if got := remoteLines(t, remoteHome); !reflect.DeepEqual(got, want) {
			t.Fatalf("nach Vergabe %d:\n got  %q\n want %q", i, got, want)
		}
	}
}

// TestRemoveDeployedKeyRemovesOnlyOwnLine (F4): Über den ausführenden Server
// entfernt RemoveDeployedKey genau die Zeile des eigenen Schlüssels; fremde
// Zeilen davor und danach bleiben unverändert und in derselben Reihenfolge.
// Die Rechte der Datei bleiben 0600. Läuft heute grün (Schutz gegen
// Rückschritt).
//
// @date 2026-10-28 13:09
func TestRemoveDeployedKeyRemovesOnlyOwnLine(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	before := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIvorher vorher@pc"
	after := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAInachher nachher@pc"
	writeRemoteAuthKeys(t, remoteHome, before+"\n")
	if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	// Eine fremde Zeile NACH dem eigenen Schlüssel
	f, err := os.OpenFile(remoteAuthKeysPath(remoteHome), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(after + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := RemoveDeployedKey(findConn(t, cfgPath, "me"), client, cfgPath); err != nil {
		t.Fatalf("RemoveDeployedKey: %v", err)
	}
	want := []string{before, after}
	if got := remoteLines(t, remoteHome); !reflect.DeepEqual(got, want) {
		t.Errorf("authorized_keys:\n got  %q\n want %q", got, want)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(remoteAuthKeysPath(remoteHome)); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("authorized_keys nach dem Entfernen: %v / %v, erwartet Rechte 600", info, err)
		}
	}
}

// TestRemoveDeployedKeyOnlyEntry (F4, Randfall, zusätzlicher Fund): Ist der
// eigene Schlüssel die EINZIGE Zeile, liefert `grep -vF` keine Zeile und
// Exit-Code 1; die Kette "grep … > tmp && mv …" bricht dann ab. Erwartet:
// kein Fehler, der Schlüssel ist aus authorized_keys entfernt, und es bleibt
// keine Temp-Datei (.authorized_keys_tmp) auf dem Server liegen. Rot heute,
// wenn die Kette tatsächlich abbricht (Fehler "Key konnte nicht vom Server
// entfernt werden", Schlüssel steht weiter drin).
//
// @date 2026-10-28 13:09
func TestRemoveDeployedKeyOnlyEntry(t *testing.T) {
	setTestHome(t)
	srv, remoteHome := startShellSSHServer(t)
	client := dialTestSSHServer(t, srv)
	conn := passwordConn("me", "srv.example.org", "admin")
	cfgPath := writeKeyConfig(t, conn)
	if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}
	saved := findConn(t, cfgPath, "me")
	b64 := strings.Fields(readFileString(t, expandTilde(saved.KeyPath)+".pub"))[1]
	if lines := remoteLines(t, remoteHome); len(lines) != 1 {
		t.Fatalf("Testaufbau: %d Zeilen, erwartet 1", len(lines))
	}

	if err := RemoveDeployedKey(saved, client, cfgPath); err != nil {
		t.Errorf("RemoveDeployedKey bei einziger Zeile: %v", err)
	}
	if lines := remoteLines(t, remoteHome); countLinesWith(lines, b64) != 0 {
		t.Errorf("Schlüssel steht weiter in authorized_keys: %q", lines)
	}
	if _, err := os.Stat(filepath.Join(remoteHome, ".ssh", ".authorized_keys_tmp")); !os.IsNotExist(err) {
		t.Errorf("Temp-Datei .authorized_keys_tmp liegt noch auf dem Server (Fehler: %v)", err)
	}
}

// --- F6: Vergabe schreibt nur ihre Felder ---

// staleTunnels ist der Tunnel, den die Konfiguration schon hat, die
// übergebene (veraltete) Verbindung aber nicht.
var staleTunnels = []TunnelConfig{{LocalPort: 8080, RemotePort: 80, Enabled: true}}

// TestAutoDeployKeyWritesOnlyOwnFields (F6): Die übergebene Verbindung ist
// veraltet (Name "alt", keine Tunnel), in der Konfiguration steht inzwischen
// Name "neu" mit einem Tunnel (z. B. per e gespeichert). Erwartet: Name und
// Tunnel bleiben, AuthType ist key, KeyPath der zurückgegebene Pfad,
// UpdatedAt neu; alle anderen Verbindungen und Felder unverändert. Rot heute:
// UpdateConnection speichert die ganze veraltete Verbindung (Name "alt",
// Tunnel weg).
//
// @date 2026-10-28 13:09
func TestAutoDeployKeyWritesOnlyOwnFields(t *testing.T) {
	setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	current := passwordConn("me", "srv.example.org", "admin")
	current.Name = "neu"
	current.Tunnels = append([]TunnelConfig(nil), staleTunnels...)
	current.UpdatedAt = "2020-01-01T00:00:00Z"
	bystander := passwordConn("andere", "andere.example.org", "root")
	cfgPath := writeKeyConfig(t, current, bystander)
	bystanderBefore := findConn(t, cfgPath, "andere")

	stale := passwordConn("me", "srv.example.org", "admin")
	stale.Name = "alt"
	stale.Tunnels = nil
	keyPath, err := AutoDeployKey(stale, client, cfgPath)
	if err != nil {
		t.Fatalf("AutoDeployKey: %v", err)
	}

	got := findConn(t, cfgPath, "me")
	if got.Name != "neu" {
		t.Errorf("Name = %q, erwartet \"neu\" (veralteter Stand überschrieben)", got.Name)
	}
	if !reflect.DeepEqual(got.Tunnels, staleTunnels) {
		t.Errorf("Tunnel = %+v, erwartet %+v", got.Tunnels, staleTunnels)
	}
	if got.AuthType != AuthKey || got.KeyPath != keyPath || keyPath == "" {
		t.Errorf("AuthType=%q KeyPath=%q (Rückgabe %q), erwartet key und derselbe Pfad", got.AuthType, got.KeyPath, keyPath)
	}
	if got.Host != "srv.example.org" || got.User != "admin" || got.Port != 22 {
		t.Errorf("Host/User/Port verändert: %+v", got)
	}
	if ts, err := time.Parse(time.RFC3339, got.UpdatedAt); err != nil || got.UpdatedAt == "2020-01-01T00:00:00Z" || time.Since(ts) > time.Minute {
		t.Errorf("UpdatedAt = %q, erwartet ein frischer RFC3339-Zeitstempel (Fehler %v)", got.UpdatedAt, err)
	}
	if after := findConn(t, cfgPath, "andere"); !reflect.DeepEqual(after, bystanderBefore) {
		t.Errorf("fremde Verbindung verändert:\n vorher  %+v\n nachher %+v", bystanderBefore, after)
	}
}

// TestRemoveDeployedKeyWritesOnlyOwnFields (F6): Wie oben für das Entfernen.
// In der Konfiguration: Name "neu", Tunnel, AuthKey mit Schlüssel; übergeben
// wird der alte Stand (Name "alt", keine Tunnel, gleicher KeyPath). Erwartet:
// Name und Tunnel bleiben, AuthType password, KeyPath leer, UpdatedAt neu.
// Rot heute: Name "alt" und Tunnel weg.
//
// @date 2026-10-28 13:09
func TestRemoveDeployedKeyWritesOnlyOwnFields(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	priv, _ := writeTestKeyPair(t, home)
	current := keyConn("me", "srv.example.org", "admin", "~/.ssh/id_test")
	current.Name = "neu"
	current.Tunnels = append([]TunnelConfig(nil), staleTunnels...)
	current.UpdatedAt = "2020-01-01T00:00:00Z"
	bystander := passwordConn("andere", "andere.example.org", "root")
	cfgPath := writeKeyConfig(t, current, bystander)
	bystanderBefore := findConn(t, cfgPath, "andere")

	stale := keyConn("me", "srv.example.org", "admin", "~/.ssh/id_test")
	stale.Name = "alt"
	stale.Tunnels = nil
	if err := RemoveDeployedKey(stale, client, cfgPath); err != nil {
		t.Fatalf("RemoveDeployedKey: %v", err)
	}

	got := findConn(t, cfgPath, "me")
	if got.Name != "neu" {
		t.Errorf("Name = %q, erwartet \"neu\" (veralteter Stand überschrieben)", got.Name)
	}
	if !reflect.DeepEqual(got.Tunnels, staleTunnels) {
		t.Errorf("Tunnel = %+v, erwartet %+v", got.Tunnels, staleTunnels)
	}
	if got.AuthType != AuthPassword || got.KeyPath != "" {
		t.Errorf("AuthType=%q KeyPath=%q, erwartet password und leer", got.AuthType, got.KeyPath)
	}
	if ts, err := time.Parse(time.RFC3339, got.UpdatedAt); err != nil || got.UpdatedAt == "2020-01-01T00:00:00Z" || time.Since(ts) > time.Minute {
		t.Errorf("UpdatedAt = %q, erwartet ein frischer RFC3339-Zeitstempel (Fehler %v)", got.UpdatedAt, err)
	}
	if _, err := os.Stat(priv); !os.IsNotExist(err) {
		t.Errorf("privater Schlüssel müsste gelöscht sein (Fehler %v)", err)
	}
	if after := findConn(t, cfgPath, "andere"); !reflect.DeepEqual(after, bystanderBefore) {
		t.Errorf("fremde Verbindung verändert:\n vorher  %+v\n nachher %+v", bystanderBefore, after)
	}
}

// TestDeployKeyUnknownIDFailsWithoutChangingConfig (F6, Randfall): Gibt es die
// ID nicht (mehr) in der Konfiguration (inzwischen gelöscht), liefern beide
// Abläufe einen Fehler und die Konfigurationsdatei bleibt Byte für Byte
// gleich (kein Wiederanlegen der gelöschten Verbindung, keine fremde
// Verbindung verändert). Läuft heute grün, weil UpdateConnection die fehlende
// ID meldet; sichert die Umstellung auf updateConfig ab.
//
// @date 2026-10-28 13:09
func TestDeployKeyUnknownIDFailsWithoutChangingConfig(t *testing.T) {
	home := setTestHome(t)
	srv := startTestSSHServer(t, 0)
	client := dialTestSSHServer(t, srv)
	writeTestKeyPair(t, home)
	cfgPath := writeKeyConfig(t, passwordConn("andere", "andere.example.org", "root"))
	before := readFileString(t, cfgPath)

	if _, err := AutoDeployKey(passwordConn("weg", "srv.example.org", "admin"), client, cfgPath); err == nil {
		t.Error("AutoDeployKey: kein Fehler bei unbekannter ID")
	}
	if got := readFileString(t, cfgPath); got != before {
		t.Errorf("AutoDeployKey hat die Konfiguration verändert:\n%s", got)
	}

	gone := keyConn("weg", "srv.example.org", "admin", "~/.ssh/id_test")
	if err := RemoveDeployedKey(gone, client, cfgPath); err == nil {
		t.Error("RemoveDeployedKey: kein Fehler bei unbekannter ID")
	}
	if got := readFileString(t, cfgPath); got != before {
		t.Errorf("RemoveDeployedKey hat die Konfiguration verändert:\n%s", got)
	}
}

// --- Merken und aussetzen: AutoKeyOff / AutoKeyFailures (Nutzerentscheidung Build 86) ---

// autoKeyServerConn legt eine Passwort-Verbindung "me" mit festem Stand in
// eine Konfiguration: Name "neu", ein Tunnel und die gewünschten Werte für
// AutoKeyOff und AutoKeyFailures. Die Verbindung wird so gespeichert, wie sie
// später von AutoDeployKey übergeben wird.
//
// @param t        - Testkontext
// @param off      - Startwert AutoKeyOff
// @param failures - Startwert AutoKeyFailures
// @return Connection - die gespeicherte Verbindung (Stand der Datei)
// @return string - Pfad der Konfiguration
// @date   2026-10-28 14:00
func autoKeyServerConn(t *testing.T, off bool, failures int) (Connection, string) {
	t.Helper()
	conn := passwordConn("me", "srv.example.org", "admin")
	conn.Name = "neu"
	conn.Tunnels = append([]TunnelConfig(nil), staleTunnels...)
	conn.UpdatedAt = "2020-01-01T00:00:00Z"
	conn.AutoKeyOff = off
	conn.AutoKeyFailures = failures
	cfgPath := writeKeyConfig(t, conn, passwordConn("andere", "andere.example.org", "root"))
	return findConn(t, cfgPath, "me"), cfgPath
}

// TestAutoDeployKeyFailureCountsOnlyItsField: Scheitert das Hinterlegen auf
// dem Server (Exit 1), zählt AutoDeployKey AutoKeyFailures um 1 hoch - bei
// jedem Versuch (1, dann 2, dann 3), und zwar nur dieses Feld: Name, Tunnel,
// AuthType (bleibt password), KeyPath (bleibt leer), UpdatedAt, AutoKeyOff und
// die andere Verbindung bleiben, auch wenn die übergebene Verbindung veraltet
// ist (Name "alt", keine Tunnel). Der Fehler wird weiter zurückgegeben. Rot
// heute: Kompilierfehler (AutoKeyFailures fehlt); danach würde nichts gezählt.
//
// @date 2026-10-28 14:00
func TestAutoDeployKeyFailureCountsOnlyItsField(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	before, cfgPath := autoKeyServerConn(t, false, 0)
	bystander := findConn(t, cfgPath, "andere")
	stale := passwordConn("me", "srv.example.org", "admin")
	stale.Name = "alt"

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := AutoDeployKey(stale, client, cfgPath); err == nil {
			t.Fatalf("Versuch %d: kein Fehler, obwohl der Server Exit 1 liefert", attempt)
		}
		want := before
		want.AutoKeyFailures = attempt
		if got := findConn(t, cfgPath, "me"); !reflect.DeepEqual(got, want) {
			t.Fatalf("Versuch %d:\n got  %+v\n want %+v", attempt, got, want)
		}
		if got := findConn(t, cfgPath, "andere"); !reflect.DeepEqual(got, bystander) {
			t.Errorf("Versuch %d: fremde Verbindung verändert: %+v", attempt, got)
		}
	}
}

// TestAutoDeployKeyFailureKeepsAutoKeyOff: Ein Fehlversuch bei schon
// gesetztem AutoKeyOff zählt trotzdem (Passwort-Anmeldung versucht es ja),
// lässt AutoKeyOff aber auf true: Nur ein ERFOLG schaltet die Vergabe wieder
// ein.
//
// @date 2026-10-28 14:00
func TestAutoDeployKeyFailureKeepsAutoKeyOff(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	before, cfgPath := autoKeyServerConn(t, true, 0)
	if _, err := AutoDeployKey(before, client, cfgPath); err == nil {
		t.Fatal("kein Fehler bei Exit 1")
	}
	got := findConn(t, cfgPath, "me")
	if !got.AutoKeyOff || got.AutoKeyFailures != 1 {
		t.Errorf("AutoKeyOff=%v AutoKeyFailures=%d, erwartet true und 1", got.AutoKeyOff, got.AutoKeyFailures)
	}
}

// connectedWithClient schickt eine sshConnectedMsg mit LEERER msg.conn (wie
// der Auto-Login) und einem echten SSH-Client im Status, damit die echte
// AutoDeployKey gegen den Testserver laufen kann.
//
// @param m           - Modell
// @param client      - Client des Testservers
// @param wasPassword - Login per Passwort?
// @return AppModel - Modell nach der Meldung
// @return tea.Cmd - Folgekommando (Vergabe oder nil)
// @date   2026-10-30 09:00
func connectedWithClient(m AppModel, client *ssh.Client, wasPassword bool) (AppModel, tea.Cmd) {
	updated, cmd := m.Update(sshConnectedMsg{id: "id-0", status: &ConnectionStatus{SSHClient: client}, wasPassword: wasPassword})
	return updated.(AppModel), cmd
}

// installCountingAutoDeploy ersetzt autoDeployKeyFunc durch einen Zähler, der
// an die ECHTE AutoDeployKey weitergibt (Zählen, Schlüssel, Server). Die
// Originalfunktion wird per t.Cleanup wiederhergestellt.
//
// @param t - Testkontext
// @return *int - Zeiger auf die Zahl der Aufrufe
// @date   2026-10-30 09:00
func installCountingAutoDeploy(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	orig := autoDeployKeyFunc
	t.Cleanup(func() { autoDeployKeyFunc = orig })
	autoDeployKeyFunc = func(conn Connection, client *ssh.Client, configPath string) (string, error) {
		*calls++
		return orig(conn, client, configPath)
	}
	return calls
}

// runAutoDeployMsg führt das Vergabe-Kommando aus und speist die Antwort
// (sshKeyDeployedMsg) ins Modell - ohne Invalidate/reloadConfig von Hand.
//
// @param t   - Testkontext
// @param m   - Modell
// @param cmd - Kommando aus sshConnectedMsg (nicht nil)
// @return AppModel - Modell nach der Antwort
// @date   2026-10-30 09:00
func runAutoDeployMsg(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("kein Vergabe-Kommando")
	}
	msg, ok := cmd().(sshKeyDeployedMsg)
	if !ok {
		t.Fatal("Kommando liefert keine sshKeyDeployedMsg")
	}
	updated, _ := m.Update(msg)
	return updated.(AppModel)
}

// TestAutoDeployKeyTwoFailuresSuspendAutoLogin: Der ganze Weg der
// Nutzerentscheidung, NUR über Update (kein Invalidate/reloadConfig von Hand;
// die frühere Fassung lud vor jedem Schritt selbst neu und verdeckte so, dass
// der Fehlerzweig von sshKeyDeployedMsg die Liste nicht neu lädt, H1). Echte
// AutoDeployKey gegen einen Server, der Exit 1 liefert. Erwartet:
//   - Auto-Login 1 und 2 vergeben (Aufruf, Fehler); nach jedem Fehler steht
//     AutoKeyFailures in m.connections (1, dann 2).
//   - Auto-Login 3: kein Aufruf, kein Kommando.
//   - Passwort-Login: wieder ein Aufruf.
//
// Rot heute: m.connections bleibt bei 0, der dritte Auto-Login vergibt erneut.
//
// @date 2026-10-30 09:00
func TestAutoDeployKeyTwoFailuresSuspendAutoLogin(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	calls := installCountingAutoDeploy(t)
	m := newListTestModel(t, "Server")

	for attempt := 1; attempt <= autoKeyMaxFailures; attempt++ {
		var cmd tea.Cmd
		m, cmd = connectedWithClient(m, client, false)
		m = runAutoDeployMsg(t, m, cmd)
		if *calls != attempt {
			t.Fatalf("nach Auto-Login %d: %d Aufrufe, erwartet %d", attempt, *calls, attempt)
		}
		if m.errorMsg == "" {
			t.Errorf("Auto-Login %d: keine Fehlermeldung", attempt)
		}
		if got := m.connections[0].AutoKeyFailures; got != attempt {
			t.Fatalf("nach Fehlversuch %d: m.connections hat AutoKeyFailures=%d, erwartet %d (Liste nicht neu geladen?)", attempt, got, attempt)
		}
	}

	m, cmd := connectedWithClient(m, client, false)
	if cmd != nil {
		t.Errorf("Auto-Login %d startet trotz %d Fehlversuchen eine Vergabe", autoKeyMaxFailures+1, autoKeyMaxFailures)
	}
	if *calls != autoKeyMaxFailures {
		t.Errorf("%d Aufrufe, erwartet %d", *calls, autoKeyMaxFailures)
	}
	m, cmd = connectedWithClient(m, client, true)
	if cmd == nil {
		t.Fatal("Passwort-Anmeldung startet keine Vergabe")
	}
	m = runAutoDeployMsg(t, m, cmd)
	if *calls != autoKeyMaxFailures+1 {
		t.Errorf("nach Passwort-Anmeldung %d Aufrufe, erwartet %d", *calls, autoKeyMaxFailures+1)
	}
}

// TestFailedDeployThenEditKeepsFailureCount (H1): Ein Fehlversuch (echte
// AutoDeployKey, Server Exit 1, nur über Update), danach e und unverändert
// speichern. keepStoredConnectionFields übernimmt den Stand aus m.connections;
// war der nach dem Fehler nicht neu geladen, schreibt das Speichern die
// veraltete 0 zurück und löscht den Zähler. Erwartet: in der Konfiguration
// steht danach AutoKeyFailures 1, nach dem zweiten Fehlversuch und Speichern 2;
// danach vergibt der Auto-Login nicht mehr. Rot heute: 0 statt 1.
//
// @date 2026-10-30 09:00
func TestFailedDeployThenEditKeepsFailureCount(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	calls := installCountingAutoDeploy(t)
	m := newListTestModel(t, "Server")

	for attempt := 1; attempt <= autoKeyMaxFailures; attempt++ {
		var cmd tea.Cmd
		m, cmd = connectedWithClient(m, client, false)
		m = runAutoDeployMsg(t, m, cmd)
		m = press(t, m, "q") // Statusansicht -> Liste
		if m.state != ViewList {
			t.Fatalf("state = %v, erwartet ViewList", m.state)
		}
		m.selectConnectionByID("id-0")
		m = press(t, m, "e")
		if m.state != ViewEdit {
			t.Fatalf("state = %v, erwartet ViewEdit", m.state)
		}
		m = saveFormNow(t, m)
		saved := testConnByID(t, loadTestConfig(t, m), "id-0")
		if saved.AutoKeyFailures != attempt {
			t.Fatalf("nach Fehlversuch %d und Speichern: AutoKeyFailures=%d in der Konfiguration, erwartet %d", attempt, saved.AutoKeyFailures, attempt)
		}
	}
	if _, cmd := connectedWithClient(m, client, false); cmd != nil {
		t.Error("Auto-Login vergibt trotz erreichter Grenze")
	}
	if *calls != autoKeyMaxFailures {
		t.Errorf("%d Aufrufe, erwartet %d", *calls, autoKeyMaxFailures)
	}
}

// TestFailedDeployMessageHasPrefixOnce (N5): Die Fehlermeldung nach einem
// Fehlversuch enthält "fehlgeschlagen" nur einmal, auch beim ersten Versuch mit
// NEU erzeugtem Schlüssel (dort hängt AutoDeployKey sein eigenes "Key-
// Deployment fehlgeschlagen: " vor den Fehler, und die Oberfläche setzt
// KeyDeployFailed davor - gleicher Text, also doppelt). Geprüft: (a) der
// Fehlertext von AutoDeployKey enthält das Präfix höchstens einmal, (b) die
// Meldung im Hauptablauf (m.errorMsg) und (c) im Datei-Manager (fm.message)
// enthalten "fehlgeschlagen" höchstens einmal - beim ersten (neuer Schlüssel)
// und beim zweiten Versuch (vorhandener Schlüssel). Rot heute (b) und (c) beim
// ersten Versuch: "Key-Deployment fehlgeschlagen: Key-Deployment
// fehlgeschlagen: ...".
//
// @date 2026-10-30 09:00
func TestFailedDeployMessageHasPrefixOnce(t *testing.T) {
	const prefix = "Key-Deployment fehlgeschlagen"
	t.Run("Fehlertext von AutoDeployKey", func(t *testing.T) {
		setTestHome(t)
		srv := startFailingSSHServer(t, 1)
		client := dialTestSSHServer(t, srv)
		conn := passwordConn("me", "srv.example.org", "admin")
		cfgPath := writeKeyConfig(t, conn)
		for attempt := 1; attempt <= 2; attempt++ {
			_, err := AutoDeployKey(conn, client, cfgPath)
			if err == nil {
				t.Fatalf("Versuch %d: kein Fehler", attempt)
			}
			if n := strings.Count(err.Error(), prefix); n > 1 {
				t.Errorf("Versuch %d: Präfix %d-mal in %q", attempt, n, err)
			}
		}
	})
	t.Run("Hauptablauf", func(t *testing.T) {
		setTestHome(t)
		srv := startFailingSSHServer(t, 1)
		client := dialTestSSHServer(t, srv)
		installCountingAutoDeploy(t)
		m := newListTestModel(t, "Server")
		for attempt := 1; attempt <= 2; attempt++ {
			var cmd tea.Cmd
			m, cmd = connectedWithClient(m, client, false)
			m = runAutoDeployMsg(t, m, cmd)
			if n := strings.Count(m.errorMsg, "fehlgeschlagen"); n != 1 {
				t.Errorf("Versuch %d: \"fehlgeschlagen\" %d-mal in %q, erwartet genau 1", attempt, n, m.errorMsg)
			}
		}
	})
	t.Run("Datei-Manager", func(t *testing.T) {
		setTestHome(t)
		srv := startFailingSSHServer(t, 1)
		client := dialTestSSHServer(t, srv)
		installRealFMDeploy(t, client)
		m := newListTestModel(t, "Server", "Zweit")
		m.fmConnect = newConnectRecorder().connect
		m.termWidth, m.termHeight = 100, 30
		for attempt := 1; attempt <= 2; attempt++ {
			m = openPick(t, m)
			m = pickSingle(t, m, "Server")
			if n := strings.Count(m.fm.message, "fehlgeschlagen"); n != 1 {
				t.Errorf("Versuch %d: \"fehlgeschlagen\" %d-mal in %q, erwartet genau 1", attempt, n, m.fm.message)
			}
			m = pressRun(t, m, "f10")
		}
	})
}

// TestAutoDeployKeySuccessResetsAutoKeyState: Ein Erfolg setzt AutoKeyOff auf
// false und AutoKeyFailures auf 0 und stellt AuthType key und KeyPath ein -
// die Passwort-Anmeldung schaltet die Vergabe damit wieder ein. Bei jedem
// Startzustand (aus, 1 Fehler, Grenze erreicht, beides). Name, Tunnel und
// andere Verbindungen bleiben wie bei F6.
//
// @date 2026-10-28 14:00
func TestAutoDeployKeySuccessResetsAutoKeyState(t *testing.T) {
	cases := []struct {
		label    string
		off      bool
		failures int
	}{
		{"aus", true, 0},
		{"ein Fehler", false, 1},
		{"Grenze erreicht", false, autoKeyMaxFailures},
		{"aus und Grenze", true, autoKeyMaxFailures + 1},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			setTestHome(t)
			srv := startTestSSHServer(t, 0)
			client := dialTestSSHServer(t, srv)
			before, cfgPath := autoKeyServerConn(t, c.off, c.failures)
			bystander := findConn(t, cfgPath, "andere")
			stale := passwordConn("me", "srv.example.org", "admin")
			stale.Name = "alt"

			keyPath, err := AutoDeployKey(stale, client, cfgPath)
			if err != nil {
				t.Fatalf("AutoDeployKey: %v", err)
			}
			got := findConn(t, cfgPath, "me")
			if got.AutoKeyOff || got.AutoKeyFailures != 0 {
				t.Errorf("AutoKeyOff=%v AutoKeyFailures=%d, erwartet false und 0", got.AutoKeyOff, got.AutoKeyFailures)
			}
			if got.AuthType != AuthKey || got.KeyPath != keyPath || keyPath == "" {
				t.Errorf("AuthType=%q KeyPath=%q (Rückgabe %q), erwartet key und derselbe Pfad", got.AuthType, got.KeyPath, keyPath)
			}
			if got.Name != "neu" || !reflect.DeepEqual(got.Tunnels, before.Tunnels) {
				t.Errorf("Name=%q Tunnel=%+v, erwartet \"neu\" und %+v", got.Name, got.Tunnels, before.Tunnels)
			}
			if after := findConn(t, cfgPath, "andere"); !reflect.DeepEqual(after, bystander) {
				t.Errorf("fremde Verbindung verändert: %+v", after)
			}
		})
	}
}

// TestAutoDeployKeyLocalFailuresCount: Auch die lokalen Fehlerstellen zählen,
// nicht nur der Server: (a) Die Schlüsseldatei existiert, aber die .pub fehlt
// - der Server bekommt gar keinen Befehl; (b) die Schlüsselerzeugung scheitert
// (~/.ssh ist eine Datei statt eines Verzeichnisses). In beiden Fällen Fehler,
// AutoKeyFailures 1, sonst nichts geändert.
//
// @date 2026-10-28 14:00
func TestAutoDeployKeyLocalFailuresCount(t *testing.T) {
	cases := []struct {
		label string
		setup func(t *testing.T, home string)
	}{
		{"Lesen der .pub scheitert", func(t *testing.T, home string) {
			if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
				t.Fatal(err)
			}
			priv := filepath.Join(home, ".ssh", "id_ed25519_srv.example.org_admin")
			if err := os.WriteFile(priv, []byte("PRIVATE-TEST-KEY\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"Schlüsselerzeugung scheitert", func(t *testing.T, home string) {
			if err := os.WriteFile(filepath.Join(home, ".ssh"), []byte("keine Verzeichnis\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			home := setTestHome(t)
			srv := startTestSSHServer(t, 0)
			client := dialTestSSHServer(t, srv)
			c.setup(t, home)
			before, cfgPath := autoKeyServerConn(t, false, 0)

			if _, err := AutoDeployKey(before, client, cfgPath); err == nil {
				t.Fatal("kein Fehler")
			}
			want := before
			want.AutoKeyFailures = 1
			if got := findConn(t, cfgPath, "me"); !reflect.DeepEqual(got, want) {
				t.Errorf("\n got  %+v\n want %+v", got, want)
			}
			if execs := srv.Execs(); len(execs) != 0 {
				t.Errorf("Server bekam Befehle trotz lokalem Fehler: %+v", execs)
			}
		})
	}
}

// TestAutoDeployKeyUnknownIDFailureDoesNotCount (Randfall): Scheitert die
// Vergabe und die ID gibt es nicht (mehr) in der Konfiguration, wird nichts
// gezählt, nichts angelegt und nichts abgestürzt: Die Konfigurationsdatei
// bleibt Byte für Byte gleich.
//
// @date 2026-10-28 14:00
func TestAutoDeployKeyUnknownIDFailureDoesNotCount(t *testing.T) {
	setTestHome(t)
	srv := startFailingSSHServer(t, 1)
	client := dialTestSSHServer(t, srv)
	cfgPath := writeKeyConfig(t, passwordConn("andere", "andere.example.org", "root"))
	before := readFileString(t, cfgPath)
	if _, err := AutoDeployKey(passwordConn("weg", "srv.example.org", "admin"), client, cfgPath); err == nil {
		t.Error("kein Fehler")
	}
	if got := readFileString(t, cfgPath); got != before {
		t.Errorf("Konfiguration verändert:\n%s", got)
	}
}

// TestRemoveDeployedKeySetsAutoKeyOff: RemoveDeployedKey merkt sich zusätzlich
// AutoKeyOff=true - sonst vergäbe der nächste Auto-Login sofort wieder einen
// Schlüssel, den der Nutzer eben mit r entfernt hat. Geprüft für einen
// eigenen und für einen geteilten Schlüssel (lokale Dateien bleiben), mit
// veralteter übergebener Verbindung: Name und Tunnel der Konfiguration bleiben,
// AuthType password, KeyPath leer, AutoKeyFailures unverändert, fremde
// Verbindungen unberührt.
//
// @date 2026-10-28 14:00
func TestRemoveDeployedKeySetsAutoKeyOff(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("geteilt=%v", shared), func(t *testing.T) {
			home := setTestHome(t)
			srv := startTestSSHServer(t, 0)
			client := dialTestSSHServer(t, srv)
			priv, _ := writeTestKeyPair(t, home)
			current := keyConn("me", "srv.example.org", "admin", "~/.ssh/id_test")
			current.Name = "neu"
			current.Tunnels = append([]TunnelConfig(nil), staleTunnels...)
			current.AutoKeyFailures = 1
			conns := []Connection{current}
			if shared {
				conns = append(conns, keyConn("zweit", "andere.example.org", "root", priv))
			}
			cfgPath := writeKeyConfig(t, conns...)
			stale := keyConn("me", "srv.example.org", "admin", "~/.ssh/id_test")
			stale.Name = "alt"

			if err := RemoveDeployedKey(stale, client, cfgPath); err != nil {
				t.Fatalf("RemoveDeployedKey: %v", err)
			}
			got := findConn(t, cfgPath, "me")
			if !got.AutoKeyOff {
				t.Error("AutoKeyOff = false, erwartet true")
			}
			if got.AutoKeyFailures != 1 {
				t.Errorf("AutoKeyFailures = %d, erwartet unverändert 1", got.AutoKeyFailures)
			}
			if got.Name != "neu" || !reflect.DeepEqual(got.Tunnels, staleTunnels) {
				t.Errorf("Name=%q Tunnel=%+v, erwartet \"neu\" und %+v", got.Name, got.Tunnels, staleTunnels)
			}
			if got.AuthType != AuthPassword || got.KeyPath != "" {
				t.Errorf("AuthType=%q KeyPath=%q, erwartet password und leer", got.AuthType, got.KeyPath)
			}
			if shared {
				if other := findConn(t, cfgPath, "zweit"); other.AutoKeyOff || other.AuthType != AuthKey || other.KeyPath != priv {
					t.Errorf("Verbindung mit dem geteilten Schlüssel verändert: %+v", other)
				}
			}
		})
	}
}

// --- Fable-Review Runde 2: N1 und N2 (Skripte auf dem Server) ---

// TestAutoDeployKeyIgnoresCommentedOutKey (N1): Eine auskommentierte Kopie
// des Schlüssels in authorized_keys ("# ssh-ed25519 AAAA… kommentar", auch mit
// Leerzeichen oder Tab vor dem #, auch ohne Leerzeichen nach dem #) gilt NICHT
// als "schon vorhanden": sshd ignoriert die Zeile, der Schlüssel wäre
// wirkungslos. Erwartet: Die Vergabe hängt eine echte, aktive Zeile an, die
// Kommentarzeile und die fremde Zeile bleiben unverändert; eine zweite
// Vergabe erkennt die aktive Zeile und ändert nichts mehr. Rot heute: grep -qF
// trifft die Kommentarzeile, es wird nichts angehängt.
//
// @date 2026-10-30 09:00
func TestAutoDeployKeyIgnoresCommentedOutKey(t *testing.T) {
	variants := []struct{ label, format string }{
		{"Kommentar", "# ssh-ed25519 %s kommentar"},
		{"Leerzeichen vor dem #", "   # ssh-ed25519 %s kommentar"},
		{"Tab vor dem #", "\t# ssh-ed25519 %s kommentar"},
		{"ohne Leerzeichen nach dem #", "#ssh-ed25519 %s"},
	}
	for _, v := range variants {
		t.Run(v.label, func(t *testing.T) {
			setTestHome(t)
			srv, remoteHome := startShellSSHServer(t)
			client := dialTestSSHServer(t, srv)
			conn := passwordConn("me", "srv.example.org", "admin")
			cfgPath := writeKeyConfig(t, conn)
			b64, ownLine := precreateAutoKey(t, "srv.example.org", "admin")
			foreign := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIfremd fremd@pc"
			commented := fmt.Sprintf(v.format, b64)
			writeRemoteAuthKeys(t, remoteHome, foreign+"\n"+commented+"\n")

			for i := 1; i <= 2; i++ {
				if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
					t.Fatalf("Vergabe %d: %v", i, err)
				}
				want := []string{foreign, commented, ownLine}
				if got := remoteLines(t, remoteHome); !reflect.DeepEqual(got, want) {
					t.Fatalf("nach Vergabe %d:\n got  %q\n want %q", i, got, want)
				}
			}
		})
	}
}

// TestRemoveDeployedKeyStaleTempNeverReplacesAuthorizedKeys (N2): Liegt eine
// alte, nicht beschreibbare ~/.ssh/.authorized_keys_tmp herum (Datei 0400 mit
// fremdem Inhalt bzw. ein Verzeichnis dieses Namens), darf authorized_keys nie
// den alten Temp-Inhalt bekommen. Zwei Ausgänge sind erlaubt: (1) Fehler und
// authorized_keys unverändert, (2) die Altlast wird vorher beseitigt und das
// Entfernen klappt richtig (fremde Zeilen bleiben, der eigene Schlüssel ist
// weg). Geprüft mit dem sh des Systems (hier dash: Umleitungsfehler = Exit 2)
// UND mit bash als sh (Umleitungsfehler = Exit 1). Mit bash als sh bricht das
// Skript heute NICHT ab, sondern verschiebt die Altlast über authorized_keys.
// Rot heute: bash als sh, Datei 0400 (unter root übersprungen, weil root 0400
// ignoriert).
//
// @date 2026-10-30 09:00
func TestRemoveDeployedKeyStaleTempNeverReplacesAuthorizedKeys(t *testing.T) {
	const marker = "ALT-INHALT-DER-TEMP-DATEI"
	staleTemps := []struct {
		label string
		make  func(t *testing.T, tmp string)
	}{
		{"Datei 0400", func(t *testing.T, tmp string) {
			if os.Geteuid() == 0 {
				t.Skip("root ignoriert 0400")
			}
			if err := os.WriteFile(tmp, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI"+marker+" alt@pc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(tmp, 0o400); err != nil {
				t.Fatal(err)
			}
		}},
		{"Verzeichnis", func(t *testing.T, tmp string) {
			if err := os.MkdirAll(filepath.Join(tmp, "drin"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, "drin", "datei"), []byte(marker+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, shell := range []string{"", "bash"} {
		for _, st := range staleTemps {
			label := "sh des Systems"
			if shell != "" {
				label = shell + " als sh"
			}
			t.Run(label+", "+st.label, func(t *testing.T) {
				setTestHome(t)
				srv, remoteHome := startShellSSHServerWithSh(t, shell)
				client := dialTestSSHServer(t, srv)
				conn := passwordConn("me", "srv.example.org", "admin")
				cfgPath := writeKeyConfig(t, conn)
				before := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIvorher vorher@pc"
				after := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAInachher nachher@pc"
				writeRemoteAuthKeys(t, remoteHome, before+"\n")
				if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
					t.Fatalf("AutoDeployKey: %v", err)
				}
				f, err := os.OpenFile(remoteAuthKeysPath(remoteHome), os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString(after + "\n"); err != nil {
					t.Fatal(err)
				}
				f.Close()
				original := readFileString(t, remoteAuthKeysPath(remoteHome))
				saved := findConn(t, cfgPath, "me")
				b64 := strings.Fields(readFileString(t, expandTilde(saved.KeyPath)+".pub"))[1]
				st.make(t, filepath.Join(remoteHome, ".ssh", ".authorized_keys_tmp"))

				err = RemoveDeployedKey(saved, client, cfgPath)
				got := readFileString(t, remoteAuthKeysPath(remoteHome))
				if strings.Contains(got, marker) {
					t.Fatalf("authorized_keys hat den alten Temp-Inhalt bekommen (Fehler: %v):\n%s", err, got)
				}
				if err != nil {
					if got != original {
						t.Errorf("Fehler (%v), aber authorized_keys verändert:\n%s", err, got)
					}
					return
				}
				want := before + "\n" + after + "\n"
				if got != want {
					t.Errorf("Entfernen ohne Fehler, aber authorized_keys falsch:\n got  %q\n want %q (Schlüssel %s)", got, want, b64)
				}
			})
		}
	}
}

// --- Fable-Review Runde 3: M1 (grep meldet einen Schreibfehler nicht) ---

// startShellSSHServerWithPrefix startet den Shell-Server mit einem Verzeichnis
// vor PATH (pathPrefix). So lässt sich ein einzelnes Programm wie grep
// austauschen, ohne das sh des Systems zu ändern.
//
// @param t      - Testkontext
// @param prefix - Verzeichnis, das vor PATH steht
// @return *testSSHServer - Laufender Server
// @return string - HOME des "Servers"
// @date   2026-10-30 16:25
func startShellSSHServerWithPrefix(t *testing.T, prefix string) (*testSSHServer, string) {
	t.Helper()
	srv, remoteHome := startShellSSHServer(t)
	srv.mu.Lock()
	srv.pathPrefix = prefix
	srv.mu.Unlock()
	return srv, remoteHome
}

// writeLyingGrep legt ein Verzeichnis mit einem Skript "grep" an, das die
// Eigenheit von BusyBox-grep nachstellt: Bei einem Aufruf, der Zeilen AUSGIBT
// und dabei -v und -F nutzt (Entfernen eines Schlüssels), geht das Schreiben
// "schief", ohne dass grep es meldet - Exit 0. Mode "nichts" schreibt gar
// nichts (volle Platte vor dem ersten Byte), Mode "abgeschnitten" nur die erste
// Zeile. Alle anderen Aufrufe (ohne -v oder ohne -F, oder mit -q/-c/-l/-L, also
// die Prüfungen in deployKeyScript und removeKeyScript) gehen unverändert an
// das echte grep, damit die Prüfungen davor und danach normal laufen. Mode
// "echt" reicht alles durch (Gegenprobe). Flags werden bis zum ersten "--"
// ausgewertet; der Schlüssel dahinter wird nie als Flag gelesen.
//
// @param t    - Testkontext
// @param mode - "nichts", "abgeschnitten" oder "echt"
// @return string - Verzeichnis mit dem Skript (für pathPrefix)
// @date   2026-10-30 16:25
func writeLyingGrep(t *testing.T, mode string) string {
	t.Helper()
	real, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("grep nicht gefunden")
	}
	var lie string
	switch mode {
	case "nichts":
		lie = "exit 0"
	case "abgeschnitten":
		lie = `'` + real + `' "$@" | head -n 1; exit 0`
	case "echt":
		lie = `exec '` + real + `' "$@"`
	default:
		t.Fatalf("unbekannter Modus %q", mode)
	}
	script := `#!/bin/sh
hasv=0; hasF=0; quiet=0
for a in "$@"; do
	case "$a" in
		--) break ;;
		-*)
			case "$a" in *v*) hasv=1 ;; esac
			case "$a" in *F*) hasF=1 ;; esac
			case "$a" in *q*|*c*|*l*|*L*) quiet=1 ;; esac
			;;
	esac
done
if [ "$hasv" = 1 ] && [ "$hasF" = 1 ] && [ "$quiet" = 0 ]; then
	` + lie + `
fi
exec '` + real + `' "$@"
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grep"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRemoveDeployedKeyBrokenGrepNeverEmptiesAuthorizedKeys (M1): BusyBox-grep
// meldet einen Schreibfehler weder mit Exit-Code noch mit Text
// (`busybox grep -vF zzz datei >> /dev/full` endet mit Exit 0). Bei voller
// Platte schöbe mv dann eine leere oder abgeschnittene Temp-Datei über
// authorized_keys, und alle Schlüssel-Anmeldungen wären weg. Der Test schiebt
// per PATH ein grep unter, das beim Entfernen (-vF mit Ausgabe) nichts bzw.
// nur die erste Zeile schreibt und Exit 0 meldet; die Prüf-Aufrufe (-q usw.)
// gehen an das echte grep. Erwartet in beiden Fällen:
//   - RemoveDeployedKey liefert einen Fehler,
//   - authorized_keys ist Byte für Byte unverändert,
//   - in ~/.ssh liegt außer authorized_keys nichts (keine Temp-Datei),
//   - die lokale Seite bleibt, wie sie war (Schlüsseldateien da, Config key).
//
// Der Fall "abgeschnitten" ist strenger als "nichts": Er verlangt, dass die
// Prüfung nicht nur eine LEERE, sondern jede unvollständige Temp-Datei erkennt
// (etwa über den Zeilenvergleich). Gegenprobe "echtes grep": Das Entfernen
// klappt weiter, fremde Zeilen bleiben, keine Temp-Datei. Rot heute (nichts):
// authorized_keys wird geleert, der Fehler bleibt aus.
//
// @date 2026-10-30 16:25
func TestRemoveDeployedKeyBrokenGrepNeverEmptiesAuthorizedKeys(t *testing.T) {
	const before = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIvorher vorher@pc"
	const after = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAInachher nachher@pc"
	for _, mode := range []string{"nichts", "abgeschnitten", "echt"} {
		t.Run(mode, func(t *testing.T) {
			setTestHome(t)
			srv, remoteHome := startShellSSHServerWithPrefix(t, writeLyingGrep(t, mode))
			client := dialTestSSHServer(t, srv)
			conn := passwordConn("me", "srv.example.org", "admin")
			cfgPath := writeKeyConfig(t, conn)
			writeRemoteAuthKeys(t, remoteHome, before+"\n")
			if _, err := AutoDeployKey(conn, client, cfgPath); err != nil {
				t.Fatalf("AutoDeployKey: %v", err)
			}
			f, err := os.OpenFile(remoteAuthKeysPath(remoteHome), os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(after + "\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			original := readFileString(t, remoteAuthKeysPath(remoteHome))
			saved := findConn(t, cfgPath, "me")
			b64 := strings.Fields(readFileString(t, expandTilde(saved.KeyPath)+".pub"))[1]
			if !strings.Contains(original, b64) || strings.Count(original, "\n") != 3 {
				t.Fatalf("Testaufbau: authorized_keys = %q", original)
			}

			err = RemoveDeployedKey(saved, client, cfgPath)
			got := readFileString(t, remoteAuthKeysPath(remoteHome))
			entries, _ := os.ReadDir(filepath.Join(remoteHome, ".ssh"))
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}

			if mode == "echt" {
				if err != nil {
					t.Fatalf("RemoveDeployedKey mit echtem grep: %v", err)
				}
				if want := before + "\n" + after + "\n"; got != want {
					t.Errorf("authorized_keys:\n got  %q\n want %q", got, want)
				}
				if !reflect.DeepEqual(names, []string{"authorized_keys"}) {
					t.Errorf("Dateien in ~/.ssh: %v, erwartet nur authorized_keys", names)
				}
				return
			}

			if err == nil {
				t.Error("RemoveDeployedKey meldet keinen Fehler, obwohl grep nichts bzw. zu wenig schrieb")
			}
			if got != original {
				t.Errorf("authorized_keys verändert:\n got  %q\n want %q", got, original)
			}
			if !reflect.DeepEqual(names, []string{"authorized_keys"}) {
				t.Errorf("Dateien in ~/.ssh: %v, erwartet nur authorized_keys (Temp-Datei übrig?)", names)
			}
			if again := findConn(t, cfgPath, "me"); again.AuthType != AuthKey || again.KeyPath != saved.KeyPath {
				t.Errorf("Konfiguration trotz Fehler verändert: AuthType=%q KeyPath=%q", again.AuthType, again.KeyPath)
			}
			if _, statErr := os.Stat(expandTilde(saved.KeyPath)); statErr != nil {
				t.Errorf("lokale Schlüsseldatei trotz Fehler weg: %v", statErr)
			}
		})
	}
}
