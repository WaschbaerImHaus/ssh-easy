// Paket main - SSH-Hilfsfunktionen fuer ssh-easy
//
// Tunnel-Verwaltung, Disconnect-Logik, SSH-Key-Generierung und
// automatisches Key-Deployment auf Remote-Server.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
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
	"strings"
	"sync"
	"time"
	"unicode"

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

// deployPublicKey fuegt einen oeffentlichen SSH-Key zur authorized_keys des Remote-Servers hinzu.
// Uebertraegt den Key sicher ueber stdin (kein Shell-Escaping noetig).
//
// @param client - Aktiver SSH-Client
// @param pubKeyStr - Oeffentlicher Schluessel im OpenSSH-Format
// @return error - Fehler beim Deployment
// @date   2026-03-08 00:00
func deployPublicKey(client *ssh.Client, pubKeyStr string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Public Key ueber stdin sicher uebergeben (kein Shell-Escaping noetig)
	session.Stdin = strings.NewReader(strings.TrimSpace(pubKeyStr) + "\n")

	// Befehl: Verzeichnis erstellen, Berechtigungen setzen, Key appenden
	cmd := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("authorized_keys konnte nicht aktualisiert werden: %w", err)
	}

	return nil
}

// sanitizeFilename entfernt Zeichen die in Dateinamen nicht erlaubt sind.
// Ersetzt ungueltige Zeichen durch Unterstriche.
//
// @param s - Eingabestring
// @return string - Bereinigter Dateiname
// @date   2026-03-08 00:00
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, s)
}

// AutoDeployKey generiert einen neuen SSH-Key und deployt ihn automatisch
// auf dem Remote-Server nach einer erfolgreichen Passwort-Anmeldung.
// Aktualisiert auch die Verbindungskonfiguration auf Key-Auth.
//
// @param conn - Verbindungskonfiguration (wird aktualisiert)
// @param client - Aktiver SSH-Client (nach Passwort-Login)
// @param configPath - Pfad zur Konfigurationsdatei fuer Update
// @return string - Pfad zum generierten Key (~/.ssh/...)
// @return error - Fehler bei Generierung oder Deployment
// @date   2026-03-08 00:00
func AutoDeployKey(conn Connection, client *ssh.Client, configPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
	}

	// Eindeutigen Key-Namen aus Host und User generieren
	keyName := fmt.Sprintf("id_ed25519_%s_%s",
		sanitizeFilename(conn.Host),
		sanitizeFilename(conn.User))
	keyPath := filepath.Join(home, ".ssh", keyName)
	keyPathTilde := "~/.ssh/" + keyName

	// Pruefen ob Key bereits existiert
	if _, err := os.Stat(keyPath); err == nil {
		// Key existiert - nur Public Key deployen
		pubKeyData, err := os.ReadFile(keyPath + ".pub")
		if err != nil {
			return "", fmt.Errorf("Existierender Public Key konnte nicht gelesen werden: %w", err)
		}
		if err := deployPublicKey(client, string(pubKeyData)); err != nil {
			return "", err
		}
	} else {
		// Neuen Ed25519-Key generieren
		pubKeyStr, err := GenerateSSHKey(keyPath, "")
		if err != nil {
			return "", fmt.Errorf("Key-Generierung fehlgeschlagen: %w", err)
		}

		// Key auf Remote-Server deployen
		if err := deployPublicKey(client, pubKeyStr); err != nil {
			return "", fmt.Errorf("Key-Deployment fehlgeschlagen: %w", err)
		}
	}

	// Verbindungskonfiguration auf Key-Auth umstellen
	conn.AuthType = AuthKey
	conn.KeyPath = keyPathTilde
	conn.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := UpdateConnection(configPath, conn); err != nil {
		// Nicht kritisch - Key ist deployed, nur Config-Update fehlgeschlagen
		return keyPathTilde, fmt.Errorf("Key deployed, Config-Update fehlgeschlagen: %w", err)
	}

	return keyPathTilde, nil
}
