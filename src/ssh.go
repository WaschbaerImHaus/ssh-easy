// Paket main - SSH-Hilfsfunktionen für ssh-easy
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
		return nil, fmt.Errorf("Port %d konnte nicht geöffnet werden: %w", tunnel.LocalPort, err)
	}

	// Remote-Adresse (immer localhost auf dem Remote-Server)
	remoteAddr := fmt.Sprintf("127.0.0.1:%d", tunnel.RemotePort)

	// Goroutine für eingehende Verbindungen
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
// @param client - SSH-Client für die Remote-Verbindung
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

// appendKnownHost fügt einen neuen Host-Key zur known_hosts-Datei hinzu.
//
// @param path - Pfad zur known_hosts-Datei
// @param hostname - Hostname des Servers
// @param key - Öffentlicher Schlüssel des Servers
// @return error - Fehler beim Schreiben
// @date   2026-03-07 21:00
func appendKnownHost(path string, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("known_hosts konnte nicht geöffnet werden: %w", err)
	}
	defer f.Close()

	line := knownhosts.Line([]string{hostname}, key)
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// DisconnectSSH beendet eine aktive SSH-Verbindung und alle zugehörigen Tunnel.
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

// GenerateSSHKey erzeugt ein neues Ed25519-Schlüsselpaar und speichert es.
//
// @param keyPath - Pfad für den privaten Schlüssel
// @param passphrase - Optionale Passphrase (leer = ohne)
// @return string - Der öffentliche Schlüssel im OpenSSH-Format
// @return error - Fehler bei der Generierung oder beim Speichern
// @date   2026-03-07 21:00
func GenerateSSHKey(keyPath string, passphrase string) (string, error) {
	// Tilde im Pfad auflösen
	if len(keyPath) > 0 && keyPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[1:])
	}

	// Verzeichnis erstellen falls nötig
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("Verzeichnis %s konnte nicht erstellt werden: %w", dir, err)
	}

	// Prüfen ob Datei bereits existiert
	if _, err := os.Stat(keyPath); err == nil {
		return "", fmt.Errorf("Datei %s existiert bereits - bitte anderen Namen wählen", keyPath)
	}

	// Ed25519-Schlüsselpaar generieren
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("Schlüsselpaar konnte nicht generiert werden: %w", err)
	}

	// Privaten Schlüssel als PEM-Block marshallen
	var pemBlock *pem.Block
	if passphrase != "" {
		pemBlock, err = ssh.MarshalPrivateKeyWithPassphrase(privKey, "", []byte(passphrase))
		if err != nil {
			return "", fmt.Errorf("Privater Schlüssel konnte nicht verschlüsselt werden: %w", err)
		}
	} else {
		pemBlock, err = ssh.MarshalPrivateKey(privKey, "")
		if err != nil {
			return "", fmt.Errorf("Privater Schlüssel konnte nicht serialisiert werden: %w", err)
		}
	}

	// Privaten Schlüssel in Datei schreiben
	privKeyBytes := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(keyPath, privKeyBytes, 0600); err != nil {
		return "", fmt.Errorf("Privater Schlüssel konnte nicht gespeichert werden: %w", err)
	}

	// Öffentlichen Schlüssel im OpenSSH-Format erstellen
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("Öffentlicher Schlüssel konnte nicht erstellt werden: %w", err)
	}
	pubKeyStr := string(ssh.MarshalAuthorizedKey(sshPubKey))

	// Öffentlichen Schlüssel in .pub-Datei schreiben
	pubKeyPath := keyPath + ".pub"
	if err := os.WriteFile(pubKeyPath, []byte(pubKeyStr), 0644); err != nil {
		return "", fmt.Errorf("Öffentlicher Schlüssel konnte nicht gespeichert werden: %w", err)
	}

	return pubKeyStr, nil
}

// removeKnownHost entfernt einen Host-Eintrag aus der known_hosts-Datei.
// Unterstützt unhashed Einträge (hostname keytype key).
// Wird benötigt wenn ein Host-Key sich geändert hat und der Nutzer
// den alten Key bewusst löschen möchte.
//
// @param knownHostsPath - Pfad zur known_hosts-Datei
// @param hostname - Hostname dessen Eintrag entfernt werden soll
// @return error - Fehler beim Lesen/Schreiben
// @date   2026-03-08 00:00
func removeKnownHost(knownHostsPath, hostname string) error {
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return fmt.Errorf("known_hosts konnte nicht gelesen werden: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var kept []string
	removed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Kommentare und Leerzeilen behalten
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}

		// Hostnamen im ersten Feld prüfen (kommagetrennte Liste möglich)
		fields := strings.Fields(trimmed)
		shouldSkip := false
		if len(fields) >= 2 {
			hosts := strings.Split(fields[0], ",")
			for _, h := range hosts {
				if h == hostname {
					shouldSkip = true
					removed = true
					break
				}
			}
		}

		if !shouldSkip {
			kept = append(kept, line)
		}
	}

	if !removed {
		// Nicht gefunden - kein Fehler (bereits entfernt oder gehasht)
		return nil
	}

	return os.WriteFile(knownHostsPath, []byte(strings.Join(kept, "\n")), 0600)
}

// getKnownHostsPath gibt den Pfad zur known_hosts-Datei zurück.
//
// @return string - Absoluter Pfad zur known_hosts-Datei
// @return error - Fehler wenn Home-Verzeichnis nicht ermittelbar
// @date   2026-03-08 00:00
func getKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// parseHostKeyChangedHostname extrahiert den Hostname aus einer
// "HOST-KEY GEÄNDERT für HOSTNAME!"-Fehlermeldung.
//
// @param err - Fehler der geparst werden soll
// @return string - Hostname oder leer wenn kein Match
// @date   2026-03-08 00:00
func parseHostKeyChangedHostname(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	marker := "HOST-KEY GEAENDERT fuer "
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return ""
	}
	rest := msg[idx+len(marker):]
	end := strings.Index(rest, "!")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// IsHostKeyChangedError prüft ob ein Fehler ein Host-Key-Änderungsfehler ist.
//
// @param err - Zu prüfender Fehler
// @return bool - Ob der Host-Key sich geändert hat
// @date   2026-03-08 00:00
func IsHostKeyChangedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "HOST-KEY GEAENDERT")
}

// deployPublicKey fügt einen öffentlichen SSH-Key zur authorized_keys des Remote-Servers hinzu.
// Überträgt den Key sicher über stdin (kein Shell-Escaping nötig).
//
// @param client - Aktiver SSH-Client
// @param pubKeyStr - Öffentlicher Schlüssel im OpenSSH-Format
// @return error - Fehler beim Deployment
// @date   2026-03-08 00:00
func deployPublicKey(client *ssh.Client, pubKeyStr string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Public Key über stdin sicher übergeben (kein Shell-Escaping nötig)
	session.Stdin = strings.NewReader(strings.TrimSpace(pubKeyStr) + "\n")

	// Befehl: Verzeichnis erstellen, Berechtigungen setzen, Key appenden
	cmd := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("authorized_keys konnte nicht aktualisiert werden: %w", err)
	}

	return nil
}

// sanitizeFilename entfernt Zeichen die in Dateinamen nicht erlaubt sind.
// Ersetzt ungültige Zeichen durch Unterstriche.
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
// @param configPath - Pfad zur Konfigurationsdatei für Update
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

	// Prüfen ob Key bereits existiert
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
