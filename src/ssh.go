// Paket main - SSH-Hilfsfunktionen fuer ssh-easy
//
// Tunnel-Verwaltung, Disconnect-Logik und SSH-Key-Generierung.
// Die Verbindungslogik ist in ssh_manager.go ausgelagert.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 21:00
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// startTunnel startet einen einzelnen Local-Port-Forwarding-Tunnel.
// Lauscht auf localhost:localPort und leitet Verbindungen an den
// Remote-Server auf remotePort weiter.
//
// @param ctx - Kontext zum Beenden des Tunnels
// @param client - Aktiver SSH-Client
// @param tunnel - Tunnel-Konfiguration
// @return net.Listener - Der lokale Listener
// @return error - Fehler beim Starten
// @date   2026-03-07 21:00
func startTunnel(ctx context.Context, client *ssh.Client, tunnel TunnelConfig) (net.Listener, error) {
	// Lokalen Listener auf 127.0.0.1 starten (nie auf 0.0.0.0!)
	localAddr := fmt.Sprintf("127.0.0.1:%d", tunnel.LocalPort)
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("Port %d konnte nicht geoeffnet werden: %w", tunnel.LocalPort, err)
	}

	// Remote-Adresse (immer localhost auf dem Remote-Server)
	remoteAddr := fmt.Sprintf("127.0.0.1:%d", tunnel.RemotePort)

	// Goroutine fuer eingehende Verbindungen
	go func() {
		for {
			select {
			case <-ctx.Done():
				listener.Close()
				return
			default:
				localConn, err := listener.Accept()
				if err != nil {
					return
				}
				go handleTunnelConnection(localConn, client, remoteAddr)
			}
		}
	}()

	return listener, nil
}

// handleTunnelConnection leitet Daten bidirektional zwischen der lokalen
// und der Remote-Verbindung weiter.
//
// @param localConn - Lokale TCP-Verbindung
// @param client - SSH-Client fuer die Remote-Verbindung
// @param remoteAddr - Zieladresse auf dem Remote-Server
// @date   2026-03-07 21:00
func handleTunnelConnection(localConn net.Conn, client *ssh.Client, remoteAddr string) {
	defer localConn.Close()

	remoteConn, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(remoteConn, localConn)
	}()

	go func() {
		defer wg.Done()
		io.Copy(localConn, remoteConn)
	}()

	wg.Wait()
}

// appendKnownHost fuegt einen neuen Host-Key zur known_hosts-Datei hinzu.
//
// @param path - Pfad zur known_hosts-Datei
// @param hostname - Hostname des Servers
// @param key - Oeffentlicher Schluessel des Servers
// @return error - Fehler beim Schreiben
// @date   2026-03-07 21:00
func appendKnownHost(path string, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("known_hosts konnte nicht geoeffnet werden: %w", err)
	}
	defer f.Close()

	line := knownhosts.Line([]string{hostname}, key)
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// DisconnectSSH beendet eine aktive SSH-Verbindung und alle zugehoerigen Tunnel.
//
// @param status - Status der zu beendenden Verbindung
// @date   2026-03-07 21:00
func DisconnectSSH(status *ConnectionStatus) {
	if status == nil {
		return
	}

	if status.Cancel != nil {
		status.Cancel()
	}

	for _, listener := range status.Listeners {
		listener.Close()
	}

	if status.SSHClient != nil {
		status.SSHClient.Close()
	}

	status.Connected = false
}

// GenerateSSHKey erzeugt ein neues Ed25519-Schluesselpaar und speichert es.
//
// @param keyPath - Pfad fuer den privaten Schluessel
// @param passphrase - Optionale Passphrase (leer = ohne)
// @return string - Der oeffentliche Schluessel im OpenSSH-Format
// @return error - Fehler bei der Generierung oder beim Speichern
// @date   2026-03-07 21:00
func GenerateSSHKey(keyPath string, passphrase string) (string, error) {
	// Tilde im Pfad aufloesen
	if len(keyPath) > 0 && keyPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[1:])
	}

	// Verzeichnis erstellen falls noetig
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("Verzeichnis %s konnte nicht erstellt werden: %w", dir, err)
	}

	// Pruefen ob Datei bereits existiert
	if _, err := os.Stat(keyPath); err == nil {
		return "", fmt.Errorf("Datei %s existiert bereits - bitte anderen Namen waehlen", keyPath)
	}

	// Ed25519-Schluesselpaar generieren
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("Schluesselpaar konnte nicht generiert werden: %w", err)
	}

	// Privaten Schluessel als PEM-Block marshallen
	var pemBlock *pem.Block
	if passphrase != "" {
		pemBlock, err = ssh.MarshalPrivateKeyWithPassphrase(privKey, "", []byte(passphrase))
		if err != nil {
			return "", fmt.Errorf("Privater Schluessel konnte nicht verschluesselt werden: %w", err)
		}
	} else {
		pemBlock, err = ssh.MarshalPrivateKey(privKey, "")
		if err != nil {
			return "", fmt.Errorf("Privater Schluessel konnte nicht serialisiert werden: %w", err)
		}
	}

	// Privaten Schluessel in Datei schreiben
	privKeyBytes := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(keyPath, privKeyBytes, 0600); err != nil {
		return "", fmt.Errorf("Privater Schluessel konnte nicht gespeichert werden: %w", err)
	}

	// Oeffentlichen Schluessel im OpenSSH-Format erstellen
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("Oeffentlicher Schluessel konnte nicht erstellt werden: %w", err)
	}
	pubKeyStr := string(ssh.MarshalAuthorizedKey(sshPubKey))

	// Oeffentlichen Schluessel in .pub-Datei schreiben
	pubKeyPath := keyPath + ".pub"
	if err := os.WriteFile(pubKeyPath, []byte(pubKeyStr), 0644); err != nil {
		return "", fmt.Errorf("Oeffentlicher Schluessel konnte nicht gespeichert werden: %w", err)
	}

	return pubKeyStr, nil
}
