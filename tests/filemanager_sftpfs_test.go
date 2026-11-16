// Paket main - Tests: newSFTPFS, Überwachung der Verbindung und find-Kanal gegen
// einen SSH/SFTP-Server im Testprozess (Build 90)
//
// Bisher rief kein Test newSFTPFS auf; Überwachung (watchConnection), Lebens-
// zeichen (activityReader) und der find-Kanal (ListTree/AbortTreeList) liefen
// nur gegen Fakes. Hier steht ein kleiner Server auf 127.0.0.1:0
// (golang.org/x/crypto/ssh), der sich steuern lässt:
//   - Subsystem "sftp": pkg/sftp-Server auf einem Temp-Verzeichnis
//   - Befehl "exec": echtes sh -c (find), tröpfelnde Ausgabe, Schweigen oder
//     "angenommen, aber nie beantwortet" (der Kanal bleibt offen, ein Close des
//     Clients bestätigt die Go-Bibliothek selbst; der Server zählt die Kanäle)
//   - Kanal-Öffnen nie beantworten: neue Session-Kanäle werden weder
//     angenommen noch abgelehnt (der SFTP-Kanal läuft weiter)
//   - Keepalive "keepalive@ssh-easy": beantworten oder verschweigen
//   - einfrieren: die Verbindung des Servers liest und schreibt nichts mehr
//     (wie eine tote Leitung oder ein Server, der kein Close bestätigt - die
//     Go-Bibliothek würde ein Close sonst von selbst bestätigen)
//
// @date   2026-11-16 12:22
// InsecureIgnoreHostKey). Zeitgrenzen sind großzügig gegen Flattern unter -race.
//
// @author Kurt Ingwer
// @date   2026-11-16 12:05
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpExecMode wählt, wie der Testserver exec-Befehle beantwortet.
type sftpExecMode int32

const (
	// sftpExecReal führt den Befehl wirklich mit /bin/sh -c aus (find)
	sftpExecReal sftpExecMode = iota
	// sftpExecDrip liefert dripData in kleinen Stücken alle dripEvery
	sftpExecDrip
	// sftpExecSilent antwortet auf den Start und friert die Verbindung ein
	sftpExecSilent
	// sftpExecNoReply nimmt den Kanal an, beantwortet "exec" aber nie (Close
	// des Clients wird normal bestätigt); execOpened/execClosed zählen mit
	sftpExecNoReply
)

// gateConn ist die Serverseite einer Verbindung, die sich einfrieren lässt:
// danach blockieren Lesen und Schreiben bis zum Schließen.
type gateConn struct {
	net.Conn
	frozen atomic.Bool
	closed chan struct{}
	once   sync.Once
}

func (g *gateConn) Read(b []byte) (int, error) {
	if g.frozen.Load() {
		<-g.closed
		return 0, net.ErrClosed
	}
	return g.Conn.Read(b)
}

func (g *gateConn) Write(b []byte) (int, error) {
	if g.frozen.Load() {
		<-g.closed
		return 0, net.ErrClosed
	}
	return g.Conn.Write(b)
}

func (g *gateConn) Close() error {
	g.once.Do(func() { close(g.closed) })
	return g.Conn.Close()
}

// freeze lässt die Verbindung verstummen.
func (g *gateConn) freeze() { g.frozen.Store(true) }

// sftpTestServer ist der steuerbare SSH-Server mit SFTP-Subsystem.
type sftpTestServer struct {
	addr    string
	hostKey ssh.PublicKey
	// root ist das Arbeitsverzeichnis des SFTP-Servers (echtes Dateisystem)
	root string
	ln   net.Listener
	mu   sync.Mutex
	// Verbindungen und SFTP-Kanäle (zum Einfrieren und Schließen)
	conns    []*gateConn
	sftpChan []ssh.Channel
	// Steuerung (vor dem Start der Anfrage setzen)
	answerKeepalive atomic.Bool
	execMode        atomic.Int32
	// keepalives zählt eingegangene Keepalives
	keepalives atomic.Int32
	// dripData wird im Modus sftpExecDrip in dripChunks Stücken im Abstand
	// dripEvery geschickt (vor dem Start setzen, danach nur lesen)
	dripData   []byte
	dripChunks int
	dripEvery  time.Duration
	// execSeen wird bei jedem exec-Start mit dem Befehl gefüllt
	execSeen chan string
	// hangOpens: neue Session-Kanäle weder annehmen noch ablehnen (nach dem
	// Öffnen des SFTP-Kanals setzen)
	hangOpens atomic.Bool
	// sessionOpens zählt alle Anfragen, einen Session-Kanal zu öffnen
	// (auch den SFTP-Kanal und die unbeantworteten)
	sessionOpens atomic.Int32
	// execOpened zählt exec-Anfragen im Modus sftpExecNoReply, execClosed die
	// davon inzwischen geschlossenen Kanäle (der Client hat sie geschlossen)
	execOpened, execClosed atomic.Int32
	// unanswered hält die unbeantworteten Kanal-Anfragen (hangOpens)
	unanswered []ssh.NewChannel
}

// startSFTPTestServer startet den Server auf 127.0.0.1:0. Alles wird per
// t.Cleanup geschlossen. Unter Windows wird übersprungen (echtes sh nötig
// ist nur für den find-Test, aber die Pfade des SFTP-Servers sind Unix-Pfade).
func startSFTPTestServer(t *testing.T) *sftpTestServer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("SFTP-Testserver nur unter Unix")
	}
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Host-Key erzeugen: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("Signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &sftpTestServer{addr: ln.Addr().String(), hostKey: signer.PublicKey(), root: root, ln: ln, execSeen: make(chan string, 16)}
	srv.answerKeepalive.Store(true)
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
			gc := &gateConn{Conn: nc, closed: make(chan struct{})}
			srv.mu.Lock()
			srv.conns = append(srv.conns, gc)
			srv.mu.Unlock()
			go srv.serve(gc, cfg)
		}
	}()
	return srv
}

// freezeAll friert alle Verbindungen des Servers ein.
func (s *sftpTestServer) freezeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.freeze()
	}
}

// closeSFTPChannels schließt nur die SFTP-Kanäle (die SSH-Verbindung bleibt).
func (s *sftpTestServer) closeSFTPChannels() {
	s.mu.Lock()
	chans := append([]ssh.Channel(nil), s.sftpChan...)
	s.mu.Unlock()
	for _, ch := range chans {
		_ = ch.Close()
	}
}

// serve bedient eine Verbindung: globale Anfragen und Session-Kanäle.
func (s *sftpTestServer) serve(gc *gateConn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(gc, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go func() {
		for req := range reqs {
			if req.Type == "keepalive@ssh-easy" {
				s.keepalives.Add(1)
				if s.answerKeepalive.Load() {
					_ = req.Reply(true, nil)
				}
				// Sonst: nie antworten
				continue
			}
			_ = req.Reply(false, nil)
		}
	}()
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "nur session")
			continue
		}
		s.sessionOpens.Add(1)
		if s.hangOpens.Load() {
			// Weder annehmen noch ablehnen: der Client wartet
			s.mu.Lock()
			s.unanswered = append(s.unanswered, newCh)
			s.mu.Unlock()
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.session(gc, ch, chReqs)
	}
}

// session beantwortet Subsystem- und exec-Anfragen eines Kanals.
func (s *sftpTestServer) session(gc *gateConn, ch ssh.Channel, reqs <-chan *ssh.Request) {
	noReply := false
	// Der Anfragenkanal schließt, sobald der Kanal zu ist (Close des Clients)
	defer func() {
		if noReply {
			s.execClosed.Add(1)
		}
	}()
	for req := range reqs {
		switch req.Type {
		case "subsystem":
			var p struct{ Name string }
			if err := ssh.Unmarshal(req.Payload, &p); err != nil || p.Name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			s.mu.Lock()
			s.sftpChan = append(s.sftpChan, ch)
			s.mu.Unlock()
			go func() {
				server, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(s.root))
				if err == nil {
					_ = server.Serve()
					_ = server.Close()
				}
			}()
		case "exec":
			var p struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &p); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			if sftpExecMode(s.execMode.Load()) == sftpExecNoReply {
				// Angenommen, aber nie beantwortet
				noReply = true
				s.execOpened.Add(1)
				continue
			}
			_ = req.Reply(true, nil)
			select {
			case s.execSeen <- p.Command:
			default:
			}
			go s.runExec(gc, ch, p.Command)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// runExec beantwortet einen exec-Befehl nach dem gewählten Modus.
func (s *sftpTestServer) runExec(gc *gateConn, ch ssh.Channel, command string) {
	status := func(code uint32) {
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
		_ = ch.Close()
	}
	switch sftpExecMode(s.execMode.Load()) {
	case sftpExecSilent:
		// Nichts mehr senden und nichts mehr lesen: auch ein Close des Clients
		// wird nie bestätigt
		gc.freeze()
	case sftpExecDrip:
		size := (len(s.dripData) + s.dripChunks - 1) / s.dripChunks
		for off := 0; off < len(s.dripData); off += size {
			end := min(off+size, len(s.dripData))
			if _, err := ch.Write(s.dripData[off:end]); err != nil {
				return
			}
			time.Sleep(s.dripEvery)
		}
		status(0)
	default:
		// Nur den festen find-Befehl von ssh-easy ausführen (treeListCommand),
		// nie einen anderen Text
		if !strings.HasPrefix(command, "sh -c 'LC_ALL=C exec xargs -0 ") {
			status(127)
			return
		}
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = ch, ch, ch.Stderr()
		code := uint32(0)
		if err := cmd.Run(); err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = uint32(ee.ExitCode())
			}
		}
		status(code)
	}
}

// dialSFTPFS verbindet sich mit dem Server (Host-Key fest) und ruft newSFTPFS
// auf. Der Zähler gibt an, wie oft onDead gerufen wurde. Aufräumen: erst die
// SSH-Verbindung, dann der sftpFS (Reihenfolge wegen eingefrorener Server).
func dialSFTPFS(t *testing.T, srv *sftpTestServer, interval, timeout time.Duration) (*sftpFS, *atomic.Int32) {
	t.Helper()
	client, err := ssh.Dial("tcp", srv.addr, &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.Password("egal")},
		HostKeyCallback: ssh.FixedHostKey(srv.hostKey),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Verbindung zum Testserver: %v", err)
	}
	dead := &atomic.Int32{}
	fsys, err := newSFTPFS(client, interval, timeout, func() { dead.Add(1) })
	if err != nil {
		client.Close()
		t.Fatalf("newSFTPFS: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = fsys.Close()
	})
	return fsys, dead
}

// sftpWaitLost wartet bis zu limit, bis die Verbindung als verloren gilt.
func sftpWaitLost(fsys *sftpFS, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if fsys.connectionLost() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fsys.connectionLost()
}

// --- C1. Lebenszeichen ---

// TestSFTPFSAnsweredKeepalivesKeepConnectionAlive - Kontrolle: Antwortet der
// Server auf die Keepalives, lebt die Verbindung auch ganz ohne SFTP-Verkehr
// länger als mehrere Zeitgrenzen; es gingen Keepalives raus, onDead blieb aus.
func TestSFTPFSAnsweredKeepalivesKeepConnectionAlive(t *testing.T) {
	srv := startSFTPTestServer(t)
	fsys, dead := dialSFTPFS(t, srv, 40*time.Millisecond, 250*time.Millisecond)
	time.Sleep(1200 * time.Millisecond)
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("Verbindung trotz beantworteter Keepalives getrennt (lost %v, onDead %d)", fsys.connectionLost(), dead.Load())
	}
	if srv.keepalives.Load() < 3 {
		t.Errorf("nur %d Keepalives angekommen", srv.keepalives.Load())
	}
}

// TestSFTPFSSFTPTrafficIsLifeSign - Der Server beantwortet KEIN Keepalive mehr,
// aber SFTP-Anfragen laufen weiter (jede Antwort ist ein Lebenszeichen,
// activityReader): Die Verbindung bleibt länger als vier Zeitgrenzen am Leben,
// onDead wird nie gerufen. So trennt die Überwachung auf einer langsamen
// Leitung keine arbeitende Verbindung.
func TestSFTPFSSFTPTrafficIsLifeSign(t *testing.T) {
	srv := startSFTPTestServer(t)
	srv.answerKeepalive.Store(false)
	const timeout = 300 * time.Millisecond
	fsys, dead := dialSFTPFS(t, srv, 40*time.Millisecond, timeout)
	deadline := time.Now().Add(4 * timeout)
	n := 0
	for time.Now().Before(deadline) {
		if _, err := fsys.Client.Stat(srv.root); err != nil {
			t.Fatalf("Stat nach %d Anfragen: %v (lost %v)", n, err, fsys.connectionLost())
		}
		n++
		time.Sleep(timeout / 4)
	}
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("Verbindung trotz laufender SFTP-Antworten getrennt (lost %v, onDead %d)", fsys.connectionLost(), dead.Load())
	}
	if srv.keepalives.Load() == 0 {
		t.Error("kein Keepalive angekommen: Test prüft nichts")
	}
}

// --- C2. Tote Verbindung ---

// TestSFTPFSSilentServerIsDeclaredDead - Der Server verstummt ganz (kein
// Keepalive, keine SFTP-Antwort): nach etwa Intervall + Zeitgrenze wird
// onDead genau einmal gerufen, connectionLost() ist wahr, und eine hängende
// SFTP-Anfrage kehrt mit Fehler zurück (sonst hinge Esc ewig).
func TestSFTPFSSilentServerIsDeclaredDead(t *testing.T) {
	srv := startSFTPTestServer(t)
	const interval, timeout = 40 * time.Millisecond, 250 * time.Millisecond
	fsys, dead := dialSFTPFS(t, srv, interval, timeout)
	if _, err := fsys.Client.Stat(srv.root); err != nil {
		t.Fatalf("Stat vor dem Verstummen: %v", err)
	}
	srv.freezeAll()
	start := time.Now()
	statDone := make(chan error, 1)
	go func() {
		_, err := fsys.Client.Stat(srv.root)
		statDone <- err
	}()
	if !sftpWaitLost(fsys, 5*time.Second) {
		t.Fatal("tote Verbindung nach 5 s nicht erkannt")
	}
	elapsed := time.Since(start)
	if elapsed < timeout*8/10 {
		t.Errorf("schon nach %v für tot erklärt (Zeitgrenze %v)", elapsed, timeout)
	}
	select {
	case err := <-statDone:
		if err == nil {
			t.Error("hängende Anfrage lieferte keinen Fehler")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hängende SFTP-Anfrage kehrte nach dem Trennen nicht zurück")
	}
	// onDead kommt vor dem Schließen; bis hierher längst gerufen, genau einmal
	time.Sleep(150 * time.Millisecond)
	if n := dead.Load(); n != 1 {
		t.Errorf("onDead %d-mal gerufen, erwartet 1", n)
	}
	if !fsys.connectionLost() {
		t.Error("connectionLost() nicht wahr")
	}
}

// TestSFTPFSClosedSFTPChannelMarksLost - Nur der SFTP-Kanal endet (sftp-server
// beendet), die SSH-Verbindung lebt: Die Goroutine nach sftpClient.Wait()
// meldet die Verbindung als verloren (Neu-Verbinden). Die Überwachung mit
// Keepalives kann das nicht gewesen sein (Intervall und Zeitgrenze: eine
// Stunde), onDead bleibt aus. Ein gewolltes Close() danach ist kein neuer
// Verlust-Alarm.
func TestSFTPFSClosedSFTPChannelMarksLost(t *testing.T) {
	srv := startSFTPTestServer(t)
	fsys, dead := dialSFTPFS(t, srv, time.Hour, time.Hour)
	if _, err := fsys.Client.Stat(srv.root); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fsys.connectionLost() {
		t.Fatal("frische Verbindung gilt als verloren")
	}
	srv.closeSFTPChannels()
	if !sftpWaitLost(fsys, 3*time.Second) {
		t.Fatal("geschlossener SFTP-Kanal nicht als Verlust erkannt")
	}
	if dead.Load() != 0 {
		t.Errorf("onDead %d-mal gerufen, obwohl die Überwachung nicht auslöste", dead.Load())
	}
	// Die SSH-Verbindung lebt noch
	if _, _, err := fsys.sshClient.SendRequest("test@ssh-easy", false, nil); err != nil {
		t.Errorf("SSH-Verbindung unerwartet tot: %v", err)
	}
	select {
	case <-fsys.lostCh:
	default:
		t.Error("lostCh nicht geschlossen")
	}
}

// TestSFTPFSWantedCloseIsNotLoss - Ein gewolltes Close() ist kein
// Verbindungsverlust (stopWatch kommt zuerst): connectionLost() bleibt falsch,
// onDead wird nicht gerufen, auch nicht nach mehreren Zeitgrenzen.
func TestSFTPFSWantedCloseIsNotLoss(t *testing.T) {
	srv := startSFTPTestServer(t)
	fsys, dead := dialSFTPFS(t, srv, 30*time.Millisecond, 100*time.Millisecond)
	if err := fsys.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("gewolltes Close als Verlust gewertet (lost %v, onDead %d)", fsys.connectionLost(), dead.Load())
	}
}

// --- C3. find-Kanal als Lebenszeichen ---

// TestSFTPFSFindOutputIsLifeSign - Keepalives bleiben unbeantwortet, SFTP ist
// still, aber der find-Kanal (ListTree) liefert tröpfelnd Daten, insgesamt
// länger als vier Zeitgrenzen: Die Verbindung bleibt am Leben (die Daten
// zählen als Lebenszeichen, ListTree packt die Ausgabe in activityReader), und
// die Ausgabe ist vollständig lesbar, wait meldet keinen Fehler. Ohne das
// trennte die Überwachung eine Verbindung, die gerade find liefert.
func TestSFTPFSFindOutputIsLifeSign(t *testing.T) {
	srv := startSFTPTestServer(t)
	srv.answerKeepalive.Store(false)
	var recs []fastRecord
	for i := 0; i < 8; i++ {
		recs = append(recs, fastRecord{typ: "f", perm: "644", size: "12", mtime: "1700000000.0000000000", start: "/x", rel: fmt.Sprintf("datei%d", i)})
	}
	srv.dripData = fastSerialize(recs)
	srv.dripChunks = 30
	srv.dripEvery = 60 * time.Millisecond
	srv.execMode.Store(int32(sftpExecDrip))
	const timeout = 300 * time.Millisecond
	fsys, dead := dialSFTPFS(t, srv, 40*time.Millisecond, timeout)
	out, wait, err := fsys.ListTree(context.Background(), []string{"/x"}, 3)
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	defer out.Close()
	type readResult struct {
		data []byte
		err  error
	}
	read := make(chan readResult, 1)
	start := time.Now()
	go func() {
		data, err := io.ReadAll(out)
		read <- readResult{data, err}
	}()
	select {
	case res := <-read:
		if res.err != nil {
			t.Fatalf("Lesen nach %v: %v (lost %v)", time.Since(start), res.err, fsys.connectionLost())
		}
		if !bytes.Equal(res.data, srv.dripData) {
			t.Errorf("Ausgabe unvollständig: %d von %d Bytes", len(res.data), len(srv.dripData))
		}
		if elapsed := time.Since(start); elapsed < 4*timeout {
			t.Errorf("Ausgabe kam schon nach %v: kürzer als 4 Zeitgrenzen, Test prüft nichts", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Lesen hängt")
	}
	if err := wait(); err != nil {
		t.Errorf("wait: %v", err)
	}
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("Verbindung trotz Daten des find-Kanals getrennt (lost %v, onDead %d)", fsys.connectionLost(), dead.Load())
	}
	if srv.keepalives.Load() == 0 {
		t.Error("kein Keepalive angekommen: Test prüft nichts")
	}
}

// --- C4. AbortTreeList ---

// TestSFTPFSAbortTreeListFreesHungRead - Der Server nimmt den find-Befehl an,
// verstummt dann ganz und bestätigt auch ein Close nie: Lesen und wait hängen,
// selbst nach out.Close() (das tut runTreeList beim Abbruch). AbortTreeList
// trennt die SSH-Verbindung: Lesen und wait kehren binnen kurzer Zeit zurück,
// die Verbindung gilt als verloren (errConnectionLost). Ein zweiter Aufruf
// schadet nicht. Die Überwachung ist hier ausgeschaltet (eine Stunde), das
// Trennen geht allein auf AbortTreeList zurück.
func TestSFTPFSAbortTreeListFreesHungRead(t *testing.T) {
	srv := startSFTPTestServer(t)
	srv.execMode.Store(int32(sftpExecSilent))
	fsys, dead := dialSFTPFS(t, srv, time.Hour, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, wait, err := fsys.ListTree(ctx, []string{"/x"}, 3)
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := out.Read(make([]byte, 64))
		readDone <- err
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- wait() }()
	// Wie beim Abbruch: Kanal schließen. Der Server bestätigt nichts.
	cancel()
	_ = out.Close()
	time.Sleep(300 * time.Millisecond)
	if fsys.connectionLost() {
		t.Fatal("Verbindung schon vor AbortTreeList verloren")
	}
	fsys.AbortTreeList()
	fsys.AbortTreeList()
	for name, ch := range map[string]chan error{"Lesen": readDone, "wait": waitDone} {
		select {
		case err := <-ch:
			if err == nil {
				t.Errorf("%s endete ohne Fehler", name)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s hängt nach AbortTreeList", name)
		}
	}
	if !fsys.connectionLost() {
		t.Error("connectionLost() nach AbortTreeList nicht wahr")
	}
	if !sftpWaitLost(fsys, time.Second) {
		t.Error("lostCh/lost nicht gesetzt")
	}
	if dead.Load() != 0 {
		t.Errorf("onDead %d-mal gerufen: die Überwachung war aus", dead.Load())
	}
	// Folgeanfragen scheitern, hängen nicht
	errCh := make(chan error, 1)
	go func() {
		_, err := fsys.Client.Stat(srv.root)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("SFTP-Anfrage nach AbortTreeList ohne Fehler")
		}
	case <-time.After(3 * time.Second):
		t.Error("SFTP-Anfrage nach AbortTreeList hängt")
	}
}

// TestSFTPFSAbortTreeListWithoutConnection - Ohne SSH-Verbindung (nur ein
// Dateisystem-Gerüst) tut AbortTreeList nichts und stürzt nicht ab; ListTree
// meldet errors.ErrUnsupported statt zu paniken.
func TestSFTPFSAbortTreeListWithoutConnection(t *testing.T) {
	fsys := &sftpFS{}
	fsys.AbortTreeList()
	if fsys.connectionLost() {
		t.Error("ohne Verbindung als verloren markiert")
	}
	if _, _, err := fsys.ListTree(context.Background(), []string{"/x"}, 3); err == nil {
		t.Error("ListTree ohne Verbindung ohne Fehler")
	}
}

// --- C5. Schnellweg über den echten Kanal ---

// TestSFTPFSFastScanThroughRealChannel - Kopieren vom sftpFS (Quelle) auf den
// lokalen PC über den Schnellweg mit dem ECHTEN find des Rechners, durch einen
// Exec-Kanal und gegen das echte Dateisystem per SFTP: Das Protokoll nennt
// "find (", der Zielbaum gleicht dem Quellbaum (Dateien, Inhalt, leere
// Verzeichnisse; ein Name mit Zeilenumbruch wird nur gemeldet), kein Rückfall auf SFTP. Ohne find mit
// -printf (z. B. BusyBox) wird übersprungen.
func TestSFTPFSFastScanThroughRealChannel(t *testing.T) {
	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find nicht gefunden")
	}
	if out, err := exec.Command("find", ".", "-maxdepth", "0", "-printf", "%y").Output(); err != nil || string(out) != "d" {
		t.Skip("find ohne -printf")
	}
	srv := startSFTPTestServer(t)
	srv.execMode.Store(int32(sftpExecReal))
	files := map[string]string{
		"proj/a.txt":          "alpha",
		"proj/sub/b.txt":      "bravo!",
		"proj/sub/deep/c.txt": "charlie..",
		"proj/mit leer/d.txt": "delta",
		"proj/ümlaut-ß/e.txt": "echo",
		"proj/-dash/f.txt":    "foxtrot",
	}
	// Unsicherer Name (Zeilenumbruch): wird gemeldet, nie kopiert
	unsafeName := "proj/zeilen\nname.go"
	for rel, data := range files {
		full := filepath.Join(srv.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Skipf("Dateiname nicht möglich: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(srv.root, "proj", "leer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv.root, filepath.FromSlash(unsafeName)), []byte("golf"), 0o644); err != nil {
		t.Skipf("Dateiname nicht möglich: %v", err)
	}
	fsys, _ := dialSFTPFS(t, srv, 200*time.Millisecond, 5*time.Second)
	dst, dstDir := newLocalTestFS(t)
	var logMu sync.Mutex
	var logs []string
	req := copyRequest{
		src: fsys, dst: dst, srcDir: srv.root, dstDir: filepath.ToSlash(dstDir), names: []string{"proj"},
		logf: func(format string, args ...interface{}) {
			logMu.Lock()
			logs = append(logs, fmt.Sprintf(format, args...))
			logMu.Unlock()
		},
	}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	logMu.Lock()
	joined := strings.Join(logs, "\n")
	logMu.Unlock()
	if !strings.Contains(joined, "Erfassen über find (") {
		t.Errorf("Schnellweg nicht genutzt; Protokoll: %q", joined)
	}
	select {
	case cmd := <-srv.execSeen:
		if cmd != treeListCommand(copyMaxDepth) {
			t.Errorf("Befehl auf dem Server: %q", cmd)
		}
	default:
		t.Error("kein exec-Befehl am Server angekommen")
	}
	// Genau ein Fehler: der unsichere Name
	if len(res.failures) != 1 || !strings.Contains(res.failures[0].path, "zeilen") || res.cancelled {
		t.Fatalf("Fehler beim Kopieren: %v (cancelled %v), erwartet nur den unsicheren Namen", res.failures, res.cancelled)
	}
	if _, err := os.Lstat(filepath.Join(dstDir, filepath.FromSlash(unsafeName))); err == nil {
		t.Error("Datei mit unsicherem Namen wurde kopiert")
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dstDir, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("%q am Ziel: %q, %v (erwartet %q)", rel, got, err, want)
		}
	}
	if info, err := os.Stat(filepath.Join(dstDir, "proj", "leer")); err != nil || !info.IsDir() {
		t.Errorf("leeres Verzeichnis fehlt am Ziel: %v", err)
	}
	if fsys.fastScanDisabled() {
		t.Error("Schnellweg ohne Anlass abgeschaltet")
	}
	if fsys.connectionLost() {
		t.Error("Verbindung nach dem Kopieren verloren")
	}
}

// --- C6. Kanal wird angenommen, aber nie beantwortet ---

// sftpHangTree legt unter root einen kleinen Quellbaum "proj" an (Dateien,
// Unterverzeichnisse, ein leeres Verzeichnis, ein Name mit Umlauten).
func sftpHangTree(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"proj/a.txt":          "alpha",
		"proj/sub/b.txt":      "bravo!",
		"proj/sub/deep/c.txt": "charlie..",
		"proj/ümlaut-ß/e.txt": "echo",
	}
	for rel, data := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "proj", "leer"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// sftpTreeSnapshot beschreibt einen Baum: relativer Pfad -> "dir" oder
// "file:" + Inhalt (ohne Rechte und Zeiten).
func sftpTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.IsDir():
			out[filepath.ToSlash(rel)] = "dir"
		case d.Type().IsRegular():
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			out[filepath.ToSlash(rel)] = "file:" + string(data)
		default:
			out[filepath.ToSlash(rel)] = "other"
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Baum lesen: %v", err)
	}
	return out
}

// sftpGoroutinesIn zählt die Goroutinen, in deren Stack eine Funktion mit dem
// Text vorkommt (direkter Nachweis, dass sie zurückgekehrt sind).
func sftpGoroutinesIn(fn string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, fn) {
			count++
		}
	}
	return count
}

// sftpWaitUntil wartet bis zu limit, bis cond wahr ist.
func sftpWaitUntil(limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// sftpCopyProj kopiert "proj" vom sftpFS (Quelle) in ein frisches lokales
// Zielverzeichnis und liefert Ergebnis, Protokoll und den Zielordner.
func sftpCopyProj(t *testing.T, fsys *sftpFS, srcRoot string) (copyResult, string, string) {
	t.Helper()
	dst, dstDir := newLocalTestFS(t)
	var logMu sync.Mutex
	var logs []string
	req := copyRequest{
		src: fsys, dst: dst, srcDir: srcRoot, dstDir: filepath.ToSlash(dstDir), names: []string{"proj"},
		logf: func(format string, args ...interface{}) {
			logMu.Lock()
			logs = append(logs, fmt.Sprintf(format, args...))
			logMu.Unlock()
		},
	}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	logMu.Lock()
	defer logMu.Unlock()
	return res, strings.Join(logs, "\n"), dstDir
}

// sftpAssertFullCopy prüft, dass der Zielbaum dem Quellbaum gleicht, ohne
// Fehler und ohne Abbruch.
func sftpAssertFullCopy(t *testing.T, label string, res copyResult, srcRoot, dstDir string) {
	t.Helper()
	if len(res.failures) != 0 || res.cancelled {
		var texts []string
		for _, f := range res.failures {
			texts = append(texts, fmt.Sprintf("%s: %v", f.path, f.err))
		}
		t.Errorf("%s: Fehler %q, abgebrochen %v", label, texts, res.cancelled)
	}
	if _, err := os.Lstat(filepath.Join(dstDir, "proj")); err != nil {
		t.Errorf("%s: Zielordner fehlt: %v", label, err)
		return
	}
	want := sftpTreeSnapshot(t, filepath.Join(srcRoot, "proj"))
	got := sftpTreeSnapshot(t, filepath.Join(dstDir, "proj"))
	if len(want) < 8 {
		t.Fatalf("%s: Quellbaum zu klein (%d Einträge), Test prüft nichts", label, len(want))
	}
	if len(got) != len(want) {
		var keys []string
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("%s: Zielbaum hat %d Einträge, Quelle %d: %q", label, len(got), len(want), keys)
	}
	for rel, w := range want {
		if g, ok := got[rel]; !ok || g != w {
			t.Errorf("%s: %q am Ziel: %q (erwartet %q)", label, rel, g, w)
		}
	}
}

// TestSFTPFSFastScanExecNeverAnsweredClosesChannel - Der Server nimmt den
// Session-Kanal an, beantwortet "exec" aber nie (ein Close bestätigt die
// Go-Bibliothek selbst). Erwartet: Nach der Leerlaufgrenze geht es per SFTP
// vollständig weiter (Zielbaum gleich Quellbaum, keine Fehler), der Schnellweg
// ist für die Verbindung aus, die Verbindung lebt (kein AbortTreeList), und
// - der eigentliche Anlass - der Abbruch schließt den exec-Kanal: Der Server
// sieht ihn zugehen, und keine Goroutine bleibt in sftpFS.ListTree (session.Start)
// zurück. Ein zweiter Lauf öffnet keinen neuen Kanal. Angriff/Randfall: ein
// Server mit ForceCommand oder Fehlfunktion, der "exec" verschluckt. Gegenprobe:
// steht die AfterFunc erst nach session.Start, bleibt der Kanal offen und
// Start hängt bis zum Ende der Verbindung.
func TestSFTPFSFastScanExecNeverAnsweredClosesChannel(t *testing.T) {
	defer fastKeepVars()()
	fastScanIdleTimeout = 250 * time.Millisecond
	fastScanAbortGrace = 150 * time.Millisecond
	srv := startSFTPTestServer(t)
	sftpHangTree(t, srv.root)
	fsys, dead := dialSFTPFS(t, srv, 100*time.Millisecond, 10*time.Second)
	baseline := sftpGoroutinesIn("(*sftpFS).ListTree")
	srv.execMode.Store(int32(sftpExecNoReply))

	start := time.Now()
	res, logs, dstDir := sftpCopyProj(t, fsys, srv.root)
	elapsed := time.Since(start)
	sftpAssertFullCopy(t, "erster Lauf", res, srv.root, dstDir)
	if srv.execOpened.Load() != 1 {
		t.Fatalf("exec-Anfragen am Server: %d, erwartet 1 (find nicht versucht: Test prüft nichts); Protokoll %q", srv.execOpened.Load(), logs)
	}
	if elapsed < fastScanIdleTimeout*8/10 {
		t.Errorf("Lauf nach %v fertig, vor der Leerlaufgrenze %v: find wurde nicht abgewartet", elapsed, fastScanIdleTimeout)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Lauf dauerte %v (Leerlaufgrenze %v)", elapsed, fastScanIdleTimeout)
	}
	if !strings.Contains(logs, "idle timeout") {
		t.Errorf("Protokoll nennt den Leerlauf nicht: %q", logs)
	}
	if !fsys.fastScanDisabled() {
		t.Errorf("Schnellweg nach der Leerlaufgrenze nicht abgeschaltet; Protokoll %q", logs)
	}
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("Verbindung getrennt (lost %v, onDead %d): kein AbortTreeList erlaubt", fsys.connectionLost(), dead.Load())
	}

	// Der Abbruch hat den Kanal geschlossen: der Server sieht ihn zugehen
	if !sftpWaitUntil(3*time.Second, func() bool { return srv.execClosed.Load() == 1 }) {
		t.Errorf("exec-Kanal nach 3 s serverseitig nicht geschlossen (geöffnet %d, geschlossen %d)", srv.execOpened.Load(), srv.execClosed.Load())
	}
	// Und ListTree/session.Start ist zurückgekehrt
	if !sftpWaitUntil(3*time.Second, func() bool { return sftpGoroutinesIn("(*sftpFS).ListTree") <= baseline }) {
		t.Errorf("%d Goroutine(n) hängen noch in sftpFS.ListTree (vorher %d)", sftpGoroutinesIn("(*sftpFS).ListTree"), baseline)
	}

	// Zweiter Lauf, dieselbe Instanz: kein neuer Kanal, weiter vollständig
	opens := srv.sessionOpens.Load()
	res2, logs2, dstDir2 := sftpCopyProj(t, fsys, srv.root)
	sftpAssertFullCopy(t, "zweiter Lauf", res2, srv.root, dstDir2)
	if n := srv.sessionOpens.Load(); n != opens {
		t.Errorf("zweiter Lauf öffnete %d neue(n) Kanal/Kanäle", n-opens)
	}
	if srv.execOpened.Load() != 1 {
		t.Errorf("zweiter Lauf: exec-Anfragen %d, erwartet 1", srv.execOpened.Load())
	}
	if !strings.Contains(logs2, "für diese Verbindung abgeschaltet") {
		t.Errorf("Protokoll des zweiten Laufs: %q", logs2)
	}
	if fsys.connectionLost() {
		t.Error("Verbindung nach dem zweiten Lauf verloren")
	}
}

// TestSFTPFSFastScanChannelOpenNeverAnswered - Der Server beantwortet das
// Öffnen NEUER Session-Kanäle nie (weder annehmen noch ablehnen); der
// SFTP-Kanal läuft weiter. NewSession kennt in golang.org/x/crypto/ssh keinen
// Abbruch: Die Goroutine in sftpFS.ListTree bleibt bis zum Ende der Verbindung
// (bekannte Grenze). Erwartet: Nach der Leerlaufgrenze geht es per SFTP
// vollständig weiter, der Schnellweg ist aus, die Verbindung lebt (nichts zu
// trennen: kein Kanal, AbortTreeList nicht gerufen). Ein zweiter Lauf öffnet
// keinen neuen Kanal (Server zählt die Öffnungen). Nach Close() der sftpFS
// endet die hängende Goroutine. Gegenprobe: ohne die Ausnahme "kein Kanal, nichts
// zu trennen" in runTreeList trennte AbortTreeList die lebende Verbindung.
func TestSFTPFSFastScanChannelOpenNeverAnswered(t *testing.T) {
	defer fastKeepVars()()
	fastScanIdleTimeout = 250 * time.Millisecond
	fastScanAbortGrace = 150 * time.Millisecond
	srv := startSFTPTestServer(t)
	sftpHangTree(t, srv.root)
	fsys, dead := dialSFTPFS(t, srv, 100*time.Millisecond, 10*time.Second)
	baseline := sftpGoroutinesIn("(*sftpFS).ListTree")
	opens := srv.sessionOpens.Load()
	srv.hangOpens.Store(true)

	start := time.Now()
	res, logs, dstDir := sftpCopyProj(t, fsys, srv.root)
	elapsed := time.Since(start)
	sftpAssertFullCopy(t, "erster Lauf", res, srv.root, dstDir)
	if n := srv.sessionOpens.Load(); n != opens+1 {
		t.Fatalf("Kanal-Öffnungen am Server: %d neue, erwartet 1 (find nicht versucht: Test prüft nichts); Protokoll %q", n-opens, logs)
	}
	if elapsed < fastScanIdleTimeout*8/10 || elapsed > 5*time.Second {
		t.Errorf("Lauf dauerte %v (Leerlaufgrenze %v)", elapsed, fastScanIdleTimeout)
	}
	if !strings.Contains(logs, "idle timeout") {
		t.Errorf("Protokoll nennt den Leerlauf nicht: %q", logs)
	}
	if !fsys.fastScanDisabled() {
		t.Errorf("Schnellweg nach der Leerlaufgrenze nicht abgeschaltet; Protokoll %q", logs)
	}
	// Auch über die Schonfrist hinaus darf nichts getrennt werden
	time.Sleep(fastScanAbortGrace + 200*time.Millisecond)
	if fsys.connectionLost() || dead.Load() != 0 {
		t.Errorf("lebende Verbindung getrennt (lost %v, onDead %d)", fsys.connectionLost(), dead.Load())
	}

	// Bekannte Grenze: die Goroutine in NewSession bleibt bis zum Ende der Verbindung
	stuck := sftpGoroutinesIn("(*sftpFS).ListTree") > baseline
	if !stuck {
		t.Log("keine Goroutine mehr in ListTree: x/crypto/ssh bricht NewSession offenbar ab, die Grenze entfällt")
	}

	// Zweiter Lauf, dieselbe Instanz: kein neuer Kanal, weiter vollständig
	res2, logs2, dstDir2 := sftpCopyProj(t, fsys, srv.root)
	sftpAssertFullCopy(t, "zweiter Lauf", res2, srv.root, dstDir2)
	if n := srv.sessionOpens.Load(); n != opens+1 {
		t.Errorf("zweiter Lauf öffnete %d neue(n) Kanal/Kanäle", n-opens-1)
	}
	if !strings.Contains(logs2, "für diese Verbindung abgeschaltet") {
		t.Errorf("Protokoll des zweiten Laufs: %q", logs2)
	}

	// Close() beendet die Verbindung, und damit die hängende Goroutine
	_ = fsys.Close()
	if !sftpWaitUntil(5*time.Second, func() bool { return sftpGoroutinesIn("(*sftpFS).ListTree") <= baseline }) {
		t.Errorf("nach Close() hängen noch %d Goroutine(n) in sftpFS.ListTree (vorher %d)", sftpGoroutinesIn("(*sftpFS).ListTree"), baseline)
	}
	if fsys.connectionLost() {
		t.Error("gewolltes Close() gilt als Verbindungsverlust")
	}
}
