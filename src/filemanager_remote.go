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
// @date   2026-10-24 20:29
package main

import (
	"context"
	"fmt"
	"io"
	"os"
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
}

// connectionLost meldet, ob die Verbindung verloren ging (Kopierfehler
// werden dann als errConnectionLost gemeldet).
//
// @return bool - true wenn abgerissen oder als tot erkannt
// @date   2026-10-24 20:29
func (s *sftpFS) connectionLost() bool {
	return s.lost != nil && s.lost.Load()
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
// @date   2026-10-24 20:29
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
	go func() {
		// Abriss erkennen (TCP-Reset, Server beendet); ein gewolltes
		// Close hat watchCtx vorher beendet
		_ = client.Wait()
		if watchCtx.Err() == nil {
			lost.Store(true)
		}
	}()
	ping := func() error {
		_, _, err := client.SendRequest("keepalive@ssh-easy", true, nil)
		return err
	}
	kill := func() {
		lost.Store(true)
		onDead()
		// Schließen lässt alle hängenden SFTP-Anfragen mit Fehler zurückkehren
		client.Close()
	}
	lastActive := func() time.Time { return time.Unix(0, lastRead.Load()) }
	go watchConnection(watchCtx, ping, kill, lastActive, interval, timeout)
	return &sftpFS{Client: sftpClient, sshClient: client, stopWatch: stopWatch, lost: lost}, nil
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
