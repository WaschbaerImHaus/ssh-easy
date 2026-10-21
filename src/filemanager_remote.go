// Paket main - Datei-Manager (MC-Modus): Zugriff auf entfernte Dateisysteme
//
// Verbindet den Datei-Manager per SFTP mit einem Server. Dafür wird eine
// eigene SSH-Verbindung OHNE Tunnel aufgebaut - unabhängig von einer
// eventuell schon bestehenden Verbindung aus der Liste. Auf dem Server muss
// nur das SFTP-Subsystem aktiv sein (Standard bei OpenSSH); der mc oder
// andere Programme werden dort nicht gebraucht.
//
// @author Kurt Ingwer
// @date   2026-10-21 19:45
package main

import (
	"fmt"

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
		sftpClient, err := sftp.NewClient(client)
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
