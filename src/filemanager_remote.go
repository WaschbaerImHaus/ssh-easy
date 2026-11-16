// Paket main - Datei-Manager (MC-Modus): Zugriff auf entfernte Dateisysteme
//
// Verbindet den Datei-Manager per SFTP mit einem Server. Dafür wird eine
// eigene SSH-Verbindung OHNE Tunnel aufgebaut - unabhängig von einer
// eventuell schon bestehenden Verbindung aus der Liste. Auf dem Server muss
// nur das SFTP-Subsystem aktiv sein (Standard bei OpenSSH); der mc oder
// andere Programme werden dort nicht gebraucht.
//
// Die Verbindung wird überwacht (watchConnection): Ohne Keepalive hing eine
// Kopie über eine tote Leitung (WLAN-Wechsel, Router-Neustart, NAT-Timeout,
// kein TCP-Reset) ohne Ende - SFTP-Anfragen haben keinen Timeout, Esc
// wirkte nicht, und die Temp-Datei blieb liegen (Fund 2026-10-24: 623 MB
// Rest mitten in einem Block).
//
// @author Kurt Ingwer
// @date   2026-11-16 12:13
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	// fmKeepaliveInterval - so oft fragt der Datei-Manager, ob die
	// Gegenseite noch antwortet.
	fmKeepaliveInterval = 15 * time.Second
	// fmKeepaliveTimeout - so lange darf die Antwort dauern. Vor dem
	// Keepalive kann ein volles SSH-Fenster (2 MiB) in der Leitung stecken
	// (bei 1 Mbit/s rund 17 s). Auf noch langsameren Leitungen kommt die
	// Antwort später - dann gilt die Verbindung trotzdem als lebendig,
	// solange SFTP-Antworten ankommen (activityReader). Eine tote Leitung
	// fällt nach spätestens Intervall + Timeout = 1 Minute auf.
	fmKeepaliveTimeout = 45 * time.Second
)

// fmConnectFunc baut die Verbindung für ein Dateifenster auf.
// Leeres Passwort = Auto-Auth (Agent, Schlüssel). In Tests austauschbar.
type fmConnectFunc func(conn Connection, password string) (remoteFS, error)

// sftpFS verbindet einen SFTP-Client mit seiner SSH-Verbindung, damit
// Close() beide schließt.
type sftpFS struct {
	*sftp.Client
	sshClient *ssh.Client
	// Beendet die Überwachung der Verbindung (nil = keine Überwachung)
	stopWatch context.CancelFunc
	// Verbindung ist ohne Close abgerissen oder wurde von der Überwachung
	// geschlossen (gemeinsam mit der Überwachung genutzt)
	lost *atomic.Bool
	// Wird beim Verlust geschlossen (lostAfter wartet kurz darauf; nil =
	// keine Überwachung)
	lostCh chan struct{}
	// Setzt lost und schließt lostCh genau einmal
	markLost func()
	// Zeitpunkt der letzten Daten vom Server (activityReader; nil = keine
	// Überwachung). Auch Daten des find-Kanals zählen als Lebenszeichen
	activity *atomic.Int64
	// find-Schnellweg für diese Verbindung abgeschaltet (nach hängendem
	// Kanal, Leerlauf- oder Zeitlimit; fastScanSwitch)
	noFastScan atomic.Bool
}

// disableFastScanForSession schaltet den find-Schnellweg für diese
// Verbindung ab (bis zum Ende der Sitzung).
//
// @date   2026-11-15 19:55
func (s *sftpFS) disableFastScanForSession() { s.noFastScan.Store(true) }

// fastScanDisabled meldet, ob der find-Schnellweg abgeschaltet ist.
//
// @return bool - true = nur SFTP
// @date   2026-11-15 19:55
func (s *sftpFS) fastScanDisabled() bool { return s.noFastScan.Load() }

// connectionLost meldet, ob die Verbindung verloren ging (Kopierfehler
// werden dann als errConnectionLost gemeldet).
//
// @return bool - true wenn abgerissen oder als tot erkannt
// @date   2026-10-24 20:29
func (s *sftpFS) connectionLost() bool {
	return s.lost != nil && s.lost.Load()
}

// fmLostGrace - so lange wartet lostAfter nach einem Lesefehler, ob die
// Überwachung den Verlust gleich meldet (die Goroutine nach client.Wait
// bzw. sftp.Client.Wait läuft erst kurz nach dem Fehler der Anfrage).
var fmLostGrace = time.Second

// lostAfter bewertet einen Fehler einer Anfrage (Neu-Verbinden,
// tui_filemanager_reconnect.go): Ist die Verbindung schon als verloren
// bekannt, ja. Antwortete der Server mit einem SFTP-Status (keine Rechte,
// nicht vorhanden …), lebt sie - nein, ohne zu warten. Meldet pkg/sftp
// selbst den Verlust (ErrSSHFxConnectionLost: SSH- oder nur SFTP-Kanal
// zu), ja. Sonst (EOF, Netzfehler) bis fmLostGrace auf die Meldung der
// Überwachung warten - sie setzt das Flag erst nach client.Wait().
//
// @param err - Fehler der Anfrage (nil = kein Fehler)
// @return bool - true = Verbindung verloren
// @date   2026-11-15 15:03
func (s *sftpFS) lostAfter(err error) bool {
	if s.connectionLost() {
		return true
	}
	if err == nil || s.lostCh == nil {
		return false
	}
	if errors.Is(err, sftp.ErrSSHFxConnectionLost) {
		s.markLost()
		return true
	}
	var status *sftp.StatusError
	if errors.As(err, &status) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		// Der Server hat geantwortet: Verbindung lebt
		return false
	}
	timer := time.NewTimer(fmLostGrace)
	defer timer.Stop()
	select {
	case <-s.lostCh:
		return true
	case <-timer.C:
		return s.connectionLost()
	}
}

// sshConnection liefert die SSH-Verbindung unter dem SFTP-Client. Die
// Schlüsselvergabe nach einer Passwort-Anmeldung (fmDeployKey) braucht sie,
// um den öffentlichen Schlüssel auf dem Server einzutragen.
//
// @return *ssh.Client - SSH-Verbindung (nil, wenn keine vorhanden)
// @date   2026-10-27 19:31
func (s *sftpFS) sshConnection() *ssh.Client {
	return s.sshClient
}

// OpenRead öffnet eine Datei zum Lesen. Die *sftp.File liest beim Kopieren
// (pipeCopy) Blöcke von copyChunkSize; pkg/sftp teilt jeden Block in viele
// gleichzeitige Anfragen auf (schnell auch bei hoher Latenz).
//
// @param p - Pfad
// @return io.ReadCloser - Datei
// @return error - Fehler beim Öffnen
// @date   2026-10-21 20:59
func (s *sftpFS) OpenRead(p string) (io.ReadCloser, error) {
	return s.Client.Open(p)
}

// CreateExclusive legt eine neue Datei an (O_EXCL): Existiert der Pfad
// schon - auch als Symlink -, schlägt es fehl.
//
// @param p - Pfad
// @return io.WriteCloser - Datei
// @return error - Fehler beim Anlegen (os.ErrExist wenn vorhanden)
// @date   2026-10-21 21:16
func (s *sftpFS) CreateExclusive(p string) (io.WriteCloser, error) {
	return s.Client.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}

// RenameOverwrite benennt um und ersetzt ein vorhandenes Ziel in einem
// Schritt (posix-rename@openssh.com). Kann der Server das nicht, wird ein
// vorhandenes Ziel (Datei oder Symlink, nie ein Verzeichnis) erst entfernt.
//
// @param oldPath - bisheriger Pfad
// @param newPath - neuer Pfad
// @return error - Fehler beim Umbenennen
// @date   2026-10-21 21:16
func (s *sftpFS) RenameOverwrite(oldPath, newPath string) error {
	if err := s.Client.PosixRename(oldPath, newPath); err == nil {
		return nil
	}
	if info, err := s.Client.Lstat(newPath); err == nil && !info.IsDir() {
		if err := s.Client.Remove(newPath); err != nil {
			return err
		}
	}
	return s.Client.Rename(oldPath, newPath)
}

// RenameNoReplace benennt um, ohne ein vorhandenes Ziel zu ersetzen
// (Verschieben auf demselben Konto, renameMoved): SSH_FXP_RENAME ist laut
// SFTP-Entwurf ein Fehler, wenn newPath existiert (OpenSSH: link+unlink
// bei Dateien, sonst nur wenn das Ziel fehlt). Bewusst NICHT
// posix-rename@openssh.com - das ersetzt ein vorhandenes Ziel.
//
// @param oldPath - bisheriger Pfad
// @param newPath - neuer Pfad (darf nicht existieren)
// @return error - Fehler beim Umbenennen (dann wird kopiert)
// @date   2026-11-13 09:28
func (s *sftpFS) RenameNoReplace(oldPath, newPath string) error {
	return s.Client.Rename(oldPath, newPath)
}

// RemoveDir löscht ein leeres Verzeichnis per SSH_FXP_RMDIR. Anders als
// Client.Remove versucht es nie zuerst, eine Datei zu löschen: Ist der
// Pfad eine Datei oder ein Symlink, scheitert es (Aufräumen beim
// Verschieben, Audit Build 90).
//
// @param p - Pfad
// @return error - Fehler (auch: kein Verzeichnis, nicht leer)
// @date   2026-11-15 09:09
func (s *sftpFS) RemoveDir(p string) error {
	return s.Client.RemoveDirectory(p)
}

// ListTree startet den find-Befehl des Schnellwegs (treeListCommand,
// filemanager_fastscan.go) in einem eigenen Exec-Kanal derselben
// SSH-Verbindung. Die Startpfade gehen NUL-getrennt über stdin. ctx
// schließt den Kanal (Esc, Zeitlimit). wait meldet einen Fehler bei
// Exit-Code ungleich 0 und bei jeder Ausgabe auf stderr. Server, die kein
// Exec erlauben, lehnen schon den Start ab (dann SFTP-Weg).
//
// @param ctx - Abbruch und Zeitlimit
// @param starts - absolute, kanonische Startpfade
// @param maxDepth - größte Tiefe
// @return io.ReadCloser - Standardausgabe (Close schließt den Kanal)
// @return func() error - wartet auf das Ende
// @return error - Kanal nicht nutzbar oder ctx beendet
// @date   2026-11-16 12:13
func (s *sftpFS) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	// Startpfade selbst prüfen, nicht nur dem Aufrufer glauben: find deutet
	// einen Pfad, der mit "-" beginnt, als Ausdruck ("-delete"!), xargs -0
	// trennt an NUL (Audit Build 90)
	if err := validTreeStarts(starts); err != nil {
		return nil, nil, err
	}
	if s.sshClient == nil {
		return nil, nil, errors.ErrUnsupported
	}
	session, err := s.sshClient.NewSession()
	if err != nil {
		return nil, nil, err
	}
	// Abbruch schließt den Kanal schon VOR dem Start: beantwortet der Server
	// "exec" nie, kehrt session.Start sonst erst mit dem Ende der Verbindung
	// zurück, und Kanal samt Goroutinen blieben offen (Fable-Review Build 90).
	// Das Öffnen des Kanals selbst (NewSession) kennt keinen Abbruch; hängt
	// schon das, endet es erst mit der Verbindung
	stop := context.AfterFunc(ctx, func() { session.Close() })
	// abandon gibt den Kanal bei einem Fehler vor der Übergabe auf
	abandon := func(err error) (io.ReadCloser, func() error, error) {
		stop()
		session.Close()
		return nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return abandon(err)
	}
	stderr := &cappedBuffer{limit: 4096}
	session.Stderr = stderr
	session.Stdin = bytes.NewReader(treeListInput(starts))
	if err := session.Start(treeListCommand(maxDepth)); err != nil {
		return abandon(err)
	}
	if ctx.Err() != nil {
		// Start kam erst nach dem Abbruch zurück: nicht mehr übergeben
		return abandon(context.Cause(ctx))
	}
	wait := func() error {
		defer stop()
		defer session.Close()
		if err := session.Wait(); err != nil {
			return err
		}
		if stderr.Len() > 0 {
			return fmt.Errorf("stderr: %s", stderr.String())
		}
		return nil
	}
	// Daten des find-Kanals sind ein Lebenszeichen wie SFTP-Antworten:
	// sonst trennte watchConnection auf langsamen Leitungen nach
	// fmKeepaliveTimeout eine lebende Verbindung, während find liefert
	var out io.Reader = stdout
	if s.activity != nil {
		out = &activityReader{r: stdout, last: s.activity}
	}
	return &sessionReader{Reader: out, session: session}, wait, nil
}

// errTreeListStart - ein Startpfad für find ist nicht absolut und kanonisch
var errTreeListStart = errors.New("tree listing: invalid start path")

// validTreeStarts prüft die Startpfade für ListTree: mindestens einer, jeder
// absolut ("/" am Anfang, also nie "-…"), bereinigt (path.Clean), ohne NUL
// und ohne Zeilenumbruch.
//
// @param starts - Startpfade
// @return error - nil oder errTreeListStart
// @date   2026-11-15 09:09
func validTreeStarts(starts []string) error {
	if len(starts) == 0 {
		return errTreeListStart
	}
	for _, s := range starts {
		if s == "" || !path.IsAbs(s) || path.Clean(s) != s || strings.ContainsAny(s, "\x00\n") {
			return errTreeListStart
		}
	}
	return nil
}

// AbortTreeList trennt die SSH-Verbindung, wenn ein find-Befehl nach dem
// Abbruch nicht endet (runTreeList, erst nach fastScanAbortGrace und nur,
// wenn der Kanal dann noch hängt). Der Server hat das Schließen des
// Exec-Kanals nicht bestätigt; golang.org/x/crypto/ssh gibt die Puffer
// eines Kanals erst mit dieser Bestätigung oder mit dem Ende der Verbindung
// frei - nur den Kanal aufzugeben, ließe Lesen und Warten ewig hängen. Die
// Verbindung gilt danach als verloren (errConnectionLost, neu verbinden).
//
// @date   2026-11-15 15:03
func (s *sftpFS) AbortTreeList() {
	if s.sshClient == nil {
		return
	}
	if s.markLost != nil {
		s.markLost()
	} else if s.lost != nil {
		s.lost.Store(true)
	}
	s.sshClient.Close()
}

// sessionReader ist die Standardausgabe eines Exec-Kanals; Close schließt
// den Kanal (Abbruch beim Lesen).
type sessionReader struct {
	io.Reader
	session *ssh.Session
}

// Close schließt den Kanal.
//
// @return error - Fehler beim Schließen
// @date   2026-11-13 09:28
func (r *sessionReader) Close() error { return r.session.Close() }

// cappedBuffer sammelt höchstens limit Bytes (stderr des find-Befehls);
// mehr wird verworfen, die Länge zählt trotzdem.
type cappedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	total int
	limit int
}

// Write nimmt Daten an, speichert aber nur bis limit.
//
// @param b - Daten
// @return int - immer len(b)
// @return error - immer nil
// @date   2026-11-13 09:28
func (c *cappedBuffer) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += len(b)
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(b[:min(room, len(b))])
	}
	return len(b), nil
}

// Len liefert die Zahl aller geschriebenen Bytes.
//
// @return int - Länge
// @date   2026-11-13 09:28
func (c *cappedBuffer) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// String liefert den gespeicherten Anfang.
//
// @return string - Text
// @date   2026-11-13 09:28
func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Close beendet die Überwachung, schließt dann den SFTP-Client und zuletzt
// die SSH-Verbindung.
//
// @return error - Fehler beim Schließen des SFTP-Clients
// @date   2026-10-24 20:29
func (s *sftpFS) Close() error {
	if s.stopWatch != nil {
		// Zuerst: ein gewolltes Schließen ist kein Verbindungsverlust
		s.stopWatch()
	}
	err := s.Client.Close()
	s.sshClient.Close()
	return err
}

// newSFTPConnector liefert die echte Verbindungsfunktion: eigenständige
// SSH-Verbindung über den SSH-Manager (Host-Key-Prüfung, Auth-Cache wie
// beim normalen Verbinden), darauf ein SFTP-Client.
//
// @param manager - SSH-Manager (für Auth und known_hosts)
// @return fmConnectFunc - Verbindungsfunktion
// @date   2026-10-24 20:29
func newSFTPConnector(manager *SSHManager) fmConnectFunc {
	return func(conn Connection, password string) (remoteFS, error) {
		client, err := manager.ConnectStandalone(conn, password)
		if err != nil {
			return nil, err
		}
		fsys, err := newSFTPFS(client, fmKeepaliveInterval, fmKeepaliveTimeout, func() {
			manager.logger.Warn("Datei-Manager: Verbindung zu %s antwortet nicht mehr - wird geschlossen", conn.Name)
		})
		if err != nil {
			client.Close()
			return nil, &fmSFTPError{err: err}
		}
		return fsys, nil
	}
}

// newSFTPFS startet auf einer SSH-Verbindung den SFTP-Client und die
// Überwachung der Verbindung (watchConnection). Antwortet die Gegenseite
// nicht mehr, wird die Verbindung geschlossen - so kehren hängende
// SFTP-Anfragen mit Fehler zurück.
//
// @param client - SSH-Verbindung
// @param interval - Abstand der Keepalives
// @param timeout - längste Wartezeit auf eine Antwort
// @param onDead - wird vor dem Schließen einer toten Verbindung aufgerufen
// @return *sftpFS - Dateisystem
// @return error - SFTP auf dem Server nicht verfügbar
// @date   2026-11-15 15:03
func newSFTPFS(client *ssh.Client, interval, timeout time.Duration, onDead func()) (*sftpFS, error) {
	// SFTP-Sitzung selbst öffnen (wie sftp.NewClient), damit jede Antwort
	// des Servers als Lebenszeichen gezählt werden kann (activityReader)
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if err := session.RequestSubsystem("sftp"); err != nil {
		session.Close()
		return nil, err
	}
	var lastRead atomic.Int64
	lastRead.Store(time.Now().UnixNano())
	reader := &activityReader{r: stdout, last: &lastRead}
	// Gleichzeitige Schreibanfragen: große Puffer werden parallel
	// übertragen statt Paket für Paket auf die Antwort zu warten
	sftpClient, err := sftp.NewClientPipe(reader, stdin, sftp.UseConcurrentWrites(true))
	if err != nil {
		session.Close()
		return nil, err
	}
	watchCtx, stopWatch := context.WithCancel(context.Background())
	lost := &atomic.Bool{}
	lostCh := make(chan struct{})
	var lostOnce sync.Once
	markLost := func() {
		lostOnce.Do(func() {
			lost.Store(true)
			close(lostCh)
		})
	}
	go func() {
		// Abriss erkennen (TCP-Reset, Server beendet); ein gewolltes
		// Close hat watchCtx vorher beendet
		_ = client.Wait()
		if watchCtx.Err() == nil {
			markLost()
		}
	}()
	go func() {
		// Nur die SFTP-Sitzung endet (sftp-server beendet, Kanal zu),
		// die SSH-Verbindung lebt weiter: ebenfalls verloren - neu verbinden
		_ = sftpClient.Wait()
		if watchCtx.Err() == nil {
			markLost()
		}
	}()
	ping := func() error {
		_, _, err := client.SendRequest("keepalive@ssh-easy", true, nil)
		return err
	}
	kill := func() {
		markLost()
		onDead()
		// Schließen lässt alle hängenden SFTP-Anfragen mit Fehler zurückkehren
		client.Close()
	}
	lastActive := func() time.Time { return time.Unix(0, lastRead.Load()) }
	go watchConnection(watchCtx, ping, kill, lastActive, interval, timeout)
	return &sftpFS{Client: sftpClient, sshClient: client, stopWatch: stopWatch, lost: lost, lostCh: lostCh, markLost: markLost, activity: &lastRead}, nil
}

// watchConnection prüft alle interval, ob die Gegenseite noch antwortet
// (ping). Scheitert ping, wird kill aufgerufen und die Überwachung endet.
// Bleibt die Antwort länger als timeout aus, wird nur geschlossen, wenn
// auch sonst seit timeout nichts mehr vom Server kam (lastActive) - auf
// einer sehr langsamen Leitung steckt das Keepalive hinter bis zu 2 MiB
// Daten, die Verbindung lebt aber. Die Überwachung endet ohne kill,
// sobald ctx beendet ist (gewolltes Schließen).
//
// @param ctx - Ende der Überwachung
// @param ping - Keepalive senden und auf Antwort warten
// @param kill - Verbindung schließen
// @param lastActive - Zeitpunkt, zu dem zuletzt etwas vom Server ankam
// @param interval - Abstand der Keepalives
// @param timeout - längste Wartezeit ohne Antwort und ohne Daten
// @date   2026-10-24 20:29
func watchConnection(ctx context.Context, ping func() error, kill func(), lastActive func() time.Time, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// ping kann auf einer toten Leitung beliebig lange hängen: eigene
		// Goroutine, die nach kill mit Fehler zurückkehrt
		result := make(chan error, 1)
		go func() { result <- ping() }()
		if waitForPing(ctx, result, lastActive, timeout) {
			continue
		}
		if ctx.Err() == nil {
			kill()
		}
		return
	}
}

// waitForPing wartet auf die Antwort eines Keepalives. Nach timeout wird
// weiter gewartet, solange in den letzten timeout etwas vom Server ankam.
//
// @param ctx - Ende der Überwachung
// @param result - Ergebnis des Keepalives
// @param lastActive - Zeitpunkt, zu dem zuletzt etwas vom Server ankam
// @param timeout - längste Wartezeit ohne Antwort und ohne Daten
// @return bool - true = Verbindung lebt, false = tot oder ctx beendet
// @date   2026-10-24 20:29
func waitForPing(ctx context.Context, result <-chan error, lastActive func() time.Time, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case err := <-result:
			return err == nil
		case <-ctx.Done():
			return false
		case <-timer.C:
			idle := time.Since(lastActive())
			if idle >= timeout {
				return false
			}
			// Langsam, aber aktiv: bis timeout nach dem letzten Lebenszeichen
			timer.Reset(timeout - idle)
		}
	}
}

// activityReader merkt sich, wann zuletzt Daten vom Server gelesen wurden
// (Lebenszeichen der Verbindung für watchConnection).
type activityReader struct {
	r io.Reader
	// Zeitpunkt des letzten Lesens in Nanosekunden (Unix)
	last *atomic.Int64
}

// Read liest weiter und merkt den Zeitpunkt, wenn Daten kamen.
//
// @date   2026-10-24 20:29
func (a *activityReader) Read(b []byte) (int, error) {
	n, err := a.r.Read(b)
	if n > 0 {
		a.last.Store(time.Now().UnixNano())
	}
	return n, err
}

// fmSFTPError - SSH-Verbindung klappte, aber SFTP ist auf dem Server nicht
// verfügbar (Subsystem abgeschaltet oder nicht installiert).
type fmSFTPError struct {
	err error
}

// Error liefert die ursprüngliche Fehlermeldung.
//
// @return string - Fehlermeldung
// @date   2026-10-21 19:45
func (e *fmSFTPError) Error() string { return fmt.Sprintf("sftp: %v", e.err) }

// Unwrap ermöglicht errors.Is/errors.As auf den ursprünglichen Fehler.
//
// @return error - Ursprünglicher Fehler
// @date   2026-10-21 19:45
func (e *fmSFTPError) Unwrap() error { return e.err }
