// Paket main - Datei-Manager (MC-Modus): Zugriff auf entfernte Dateisysteme
//
// Verbindet den Datei-Manager per SFTP mit einem Server. Dafür wird eine
// eigene SSH-Verbindung OHNE Tunnel aufgebaut - unabhängig von einer
// eventuell schon bestehenden Verbindung aus der Liste. Auf dem Server muss
// nur das SFTP-Subsystem aktiv sein (Standard bei OpenSSH); der mc oder
// andere Programme werden dort nicht gebraucht.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:16
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// fmConnectFunc baut die Verbindung für ein Dateifenster auf.
// Leeres Passwort = Auto-Auth (Agent, Schlüssel). In Tests austauschbar.
type fmConnectFunc func(conn Connection, password string) (remoteFS, error)

// sftpFS verbindet einen SFTP-Client mit seiner SSH-Verbindung, damit
// Close() beide schließt.
type sftpFS struct {
	*sftp.Client
	sshClient *ssh.Client
}

// OpenRead öffnet eine Datei zum Lesen. Die *sftp.File liest beim Kopieren
// über WriteTo mit mehreren gleichzeitigen Anfragen (schnell auch bei
// hoher Latenz).
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

// Close schließt erst den SFTP-Client, dann die SSH-Verbindung.
//
// @return error - Fehler beim Schließen des SFTP-Clients
// @date   2026-10-21 19:45
func (s *sftpFS) Close() error {
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
// @date   2026-10-21 19:45
func newSFTPConnector(manager *SSHManager) fmConnectFunc {
	return func(conn Connection, password string) (remoteFS, error) {
		client, err := manager.ConnectStandalone(conn, password)
		if err != nil {
			return nil, err
		}
		// Gleichzeitige Schreibanfragen: große Puffer werden parallel
		// übertragen statt Paket für Paket auf die Antwort zu warten
		sftpClient, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
		if err != nil {
			client.Close()
			return nil, &fmSFTPError{err: err}
		}
		return &sftpFS{Client: sftpClient, sshClient: client}, nil
	}
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
