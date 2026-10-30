// Paket main - SSH-Hilfsfunktionen für ssh-easy
//
// Tunnel-Verwaltung, Disconnect-Logik, SSH-Key-Generierung und
// automatisches Key-Deployment auf Remote-Server.
//
// @author Kurt Ingwer
// @date   2026-10-30 16:23
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyChangedError signalisiert dass sich ein Host-Key gegenüber dem
// gespeicherten Eintrag in known_hosts geändert hat (möglicher MITM-Angriff).
// Typisierter Fehler ermöglicht robustes Matching via errors.As() statt
// fragilem String-Parsing der Fehlermeldung.
//
// @date   2026-04-19 12:00
type HostKeyChangedError struct {
	// Hostname/IP des betroffenen Servers
	Hostname string
}

// Error liefert die lesbare Fehlermeldung.
// Format: "HOST-KEY GEÄNDERT für HOSTNAME! Möglicher MITM-Angriff."
//
// @return string - Fehlermeldung
// @date   2026-04-19 12:00
func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("HOST-KEY GEÄNDERT für %s! Möglicher MITM-Angriff.", e.Hostname)
}

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
// @return error - Fehler beim Schreiben oder Schließen
// @date   2026-10-17 22:40
func appendKnownHost(path string, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("known_hosts konnte nicht geöffnet werden: %w", err)
	}

	line := knownhosts.Line([]string{hostname}, key)
	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		f.Close()
		return err
	}
	// Close-Fehler melden: sonst gälte ein nicht vollständig geschriebener
	// Host-Key als gespeichert
	return f.Close()
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
	// Unter Windows setzt 0600 keine Berechtigungen - Private-Key via DACL härten
	if err := restrictFilePermissions(keyPath); err != nil {
		return "", fmt.Errorf("Berechtigungen für Privatschlüssel konnten nicht gesetzt werden: %w", err)
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

// matchesHashedKnownHost prüft ob ein gehashter known_hosts-Eintrag zum Hostname passt.
// OpenSSH hasht Hostnamen mit HMAC-SHA1(Salz, Hostname) und speichert Salz+Hash in Base64.
// Format: |1|BASE64_SALT|BASE64_HASH
//
// @param hashField - Erstes Feld einer known_hosts-Zeile (z.B. "|1|abc...|xyz...")
// @param hostname - Zu prüfender Hostname oder IP
// @return bool - true wenn der Eintrag zu diesem Hostname gehört
// @date   2026-03-15 00:00
func matchesHashedKnownHost(hashField, hostname string) bool {
	// Nur Format-Version 1 wird unterstützt (|1|...)
	if !strings.HasPrefix(hashField, "|1|") {
		return false
	}

	// Salz und Hash trennen: |1|SALZ|HASH → ["SALZ", "HASH"]
	rest := hashField[3:]
	pipe := strings.Index(rest, "|")
	if pipe < 0 {
		return false
	}
	saltB64 := rest[:pipe]
	hashB64 := rest[pipe+1:]

	// Salz und gespeicherten Hash dekodieren
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return false
	}
	storedHash, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		return false
	}

	// HMAC-SHA1(Salz, Hostname) berechnen und vergleichen
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(hostname))
	computed := mac.Sum(nil)

	return hmac.Equal(computed, storedHash)
}

// removeKnownHost entfernt einen Host-Eintrag aus der known_hosts-Datei.
// Unterstützt sowohl unhashed (hostname keytype key) als auch gehashte
// Einträge (|1|SALT|HASH keytype key) wie sie neuere OpenSSH-Versionen erzeugen.
//
// Der hostname-Parameter wird vor dem Vergleich normalisiert:
// Port 22 wird weggelassen ("host:22" → "host"), andere Ports werden
// in eckige Klammern gesetzt ("[host]:port"). So stimmt der Vergleich
// mit dem von OpenSSH geschriebenen Format in known_hosts überein.
//
// Verwendet das Umbenennen-Filtern-Zurückbenennen-Muster für Atomarität:
// known_hosts → known_hosts.bak → gefiltert nach known_hosts → .bak löschen
//
// @param knownHostsPath - Pfad zur known_hosts-Datei
// @param hostname - Hostname/IP:Port dessen Eintrag entfernt werden soll
// @return error - Fehler beim Lesen/Schreiben
// @date   2026-03-15 02:00
func removeKnownHost(knownHostsPath, hostname string) error {
	bakPath := knownHostsPath + ".bak"

	// Hostname normalisieren: OpenSSH schreibt Port 22 ohne ":22" in known_hosts.
	// knownhosts.Normalize("192.168.1.1:22") → "192.168.1.1"
	// knownhosts.Normalize("192.168.1.1:2222") → "[192.168.1.1]:2222"
	// Nur so stimmt der Vergleich mit dem gespeicherten Eintrag überein.
	normalizedHostname := knownhosts.Normalize(hostname)

	// Datei umbenennen für sicheres atomares Update
	// (bei Absturz bleibt .bak erhalten, original ist weg - wird beim nächsten SSH neu erstellt)
	if err := os.Rename(knownHostsPath, bakPath); err != nil {
		return fmt.Errorf("known_hosts konnte nicht umbenannt werden: %w", err)
	}

	data, err := os.ReadFile(bakPath)
	if err != nil {
		// Umbenennung rückgängig machen damit keine Daten verloren gehen
		os.Rename(bakPath, knownHostsPath)
		return fmt.Errorf("known_hosts.bak konnte nicht gelesen werden: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var kept []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Kommentare und Leerzeilen immer behalten
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}

		// Erstes Feld ist Hostname/IP oder Hash
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			kept = append(kept, line)
			continue
		}

		hostField := fields[0]
		shouldRemove := false

		if strings.HasPrefix(hostField, "|1|") {
			// Gehashter Eintrag: HMAC-SHA1-Vergleich gegen normalisierten Hostname
			shouldRemove = matchesHashedKnownHost(hostField, normalizedHostname)
		} else {
			// Ungehashter Eintrag: Vergleich gegen normalisierten Hostname
			// (kommagetrennte Hosts möglich, z.B. "hostname,192.168.1.1")
			for _, h := range strings.Split(hostField, ",") {
				if h == normalizedHostname {
					shouldRemove = true
					break
				}
			}
		}

		if !shouldRemove {
			kept = append(kept, line)
		}
	}

	// Gefilterte known_hosts schreiben
	if err := os.WriteFile(knownHostsPath, []byte(strings.Join(kept, "\n")), 0600); err != nil {
		// Schreiben fehlgeschlagen: Backup wiederherstellen
		os.Rename(bakPath, knownHostsPath)
		return fmt.Errorf("known_hosts konnte nicht geschrieben werden: %w", err)
	}

	// Backup-Datei entfernen
	os.Remove(bakPath)
	return nil
}

// getKnownHostsPath gibt den Pfad zur known_hosts-Datei zurück.
//
// @return string - Absoluter Pfad zur known_hosts-Datei
// @return error - Fehler wenn Home-Verzeichnis nicht ermittelbar
// @date   2026-03-15 00:00
func getKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// parseHostKeyChangedHostname extrahiert den Hostname aus einem
// HostKeyChangedError (auch verpackt in anderen Fehlern).
// Verwendet errors.As statt fragilem String-Parsing.
//
// @param err - Fehler der geprüft werden soll
// @return string - Hostname oder leer wenn kein Host-Key-Wechsel
// @date   2026-04-19 12:00
func parseHostKeyChangedHostname(err error) string {
	var hkcErr *HostKeyChangedError
	if errors.As(err, &hkcErr) {
		return hkcErr.Hostname
	}
	return ""
}

// IsHostKeyChangedError prüft ob ein Fehler ein Host-Key-Änderungsfehler ist
// (auch verpackt in anderen Fehlern, z.B. durch fmt.Errorf("%w", ...)).
//
// @param err - Zu prüfender Fehler
// @return bool - Ob der Host-Key sich geändert hat
// @date   2026-04-19 12:00
func IsHostKeyChangedError(err error) bool {
	var hkcErr *HostKeyChangedError
	return errors.As(err, &hkcErr)
}

// deployKeyScript hängt den Public Key aus stdin an ~/.ssh/authorized_keys an,
// aber nur, wenn sein Base64-Teil dort noch nicht steht (Build 86, F4).
// Ablauf auf dem Server:
//  1. umask 077, Globbing aus (set -f), stdin nach KEY lesen.
//  2. Base64-Teil = zweites Wort ($2); fehlt er, Exit 2 (Fehler).
//  3. ~/.ssh anlegen (700) und authorized_keys anlegen (600), Rechte immer
//     neu setzen (korrigiert zu offene Rechte einer alten Installation).
//  4. Kommentarzeilen ("#", auch nach Leerzeichen/Tab) vorher ausfiltern:
//     sshd ignoriert sie, ein auskommentierter Schlüssel wäre wirkungslos
//     und darf nicht als "schon da" gelten (Fable-Review Runde 2, N1).
//     Danach grep -qF: fester String, kein Regex (Base64 enthält '+', '/',
//     '='). Ein Treffer heißt "schon da" - auch mit anderem Kommentar ->
//     fertig. Exit 1 = nicht gefunden, alles darüber = Fehler.
//     Der Exit-Code der Pipe ist der des zweiten grep. Liefert das erste
//     grep keine Zeile (leere Datei oder nur Kommentare, Exit 1), bekommt
//     das zweite eine leere Eingabe und meldet korrekt "nicht gefunden".
//     Einen Lesefehler des ersten grep schließt "[ -r ... ]" vorher aus.
//  5. Endet die Datei ohne Zeilenumbruch (von Hand bearbeitet), zuerst einen
//     anhängen; sonst klebte der Schlüssel an der letzten fremden Zeile.
//  6. Schlüsselzeile anhängen ("| cat >> ~/.ssh/authorized_keys" hält die
//     bisherige Form des Befehls bei, auf die sich die Tests verlassen).
//
// Das Skript läuft in "sh -c '...'": sshd startet Befehle mit der Login-Shell
// des Benutzers, und die kann fish oder csh sein. Deshalb enthält das Skript
// keine einfachen Anführungszeichen, keine Zeilenumbrüche und kein "!".
// Der einzige Backslash ("%s\n" für printf) bleibt in fish und csh innerhalb
// einfacher Anführungszeichen unverändert.
// Der Schlüssel steht nie im Befehlstext, er kommt nur über stdin.
//
// @date   2026-10-30 16:13
const deployKeyScript = `sh -c 'umask 077; set -f; KEY=$(cat); set -- $KEY; [ -n "$2" ] || exit 2; ` +
	`mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys || exit 2; ` +
	`[ -r ~/.ssh/authorized_keys ] || exit 2; ` +
	`grep -v "^[[:space:]]*#" ~/.ssh/authorized_keys | grep -qF -- "$2"; rc=$?; [ "$rc" -eq 0 ] && exit 0; [ "$rc" -eq 1 ] || exit 2; ` +
	`if [ -s ~/.ssh/authorized_keys ] && [ -n "$(tail -c 1 ~/.ssh/authorized_keys)" ]; then echo >> ~/.ssh/authorized_keys || exit 2; fi; ` +
	`printf "%s\n" "$KEY" | cat >> ~/.ssh/authorized_keys'`

// removeKeyScript entfernt jede Zeile mit dem Base64-Teil aus stdin aus
// ~/.ssh/authorized_keys (grep -vF, fester String). Ablauf auf dem Server:
//  1. umask 077, set -C (noclobber: ">" überschreibt keine vorhandene Datei).
//     Fehlt authorized_keys, gibt es nichts zu entfernen (Erfolg).
//  2. Eine alte ~/.ssh/.authorized_keys_tmp (Datei, auch schreibgeschützt,
//     oder Verzeichnis) wird mit rm -rf beseitigt; geht das nicht, Exit 2.
//     Dann legt ": >" die Temp-Datei NEU und leer an (dank set -C scheitert
//     das, falls sie inzwischen wieder existiert) und prüft, dass sie eine
//     reguläre Datei und kein Symlink ist (Fable-Review Runde 2, N2).
//     Grund: Ist /bin/sh eine bash, liefert eine gescheiterte Umleitung
//     Exit 1 statt 2 - früher landete so eine alte Temp-Datei per mv über
//     authorized_keys.
//  3. grep -vF hängt an die eigene, leere Temp-Datei an. Exit 1 heißt "keine
//     Zeile übrig" (der eigene Schlüssel war der einzige Eintrag) und ist ein
//     Erfolg, Exit 2 und mehr ein Fehler (Zusatzfund Build 86).
//  4. Zeilenabgleich vor dem mv (Fable-Review Runde 3, M1): "grep -cvF"
//     zählt, wie viele Zeilen übrig bleiben MÜSSEN; "wc -l" zählt, wie viele
//     in der Temp-Datei stehen. Weichen sie ab, Abbruch mit Exit 2 und
//     authorized_keys bleibt unverändert. Grund: BusyBox-grep meldet einen
//     Schreibfehler (volle Platte) weder per Exit-Code noch mit Text; mv
//     schöbe sonst eine leere oder abgeschnittene Datei über authorized_keys,
//     und alle Schlüssel-Anmeldungen wären weg (Aussperren). Der Abgleich
//     fängt auch einen Umleitungsfehler unter bash (Exit 1 statt 2) ab.
//     Grundlage: grep schließt jede Ausgabezeile mit einem Zeilenumbruch ab,
//     auch die letzte Zeile einer Datei ohne Zeilenende; das Leerzeichen-
//     Polster von wc schluckt "[ -eq ]" unter dash, bash und BusyBox.
//     "grep -c" endet bei 0 Treffern mit Exit 1 und gibt trotzdem "0" aus -
//     deshalb kein "||" hinter der Zuweisung. Scheitert grep -c ganz, ist n
//     leer, "[ -eq ]" schlägt fehl und es folgt ebenfalls der Abbruch.
//  5. mv -f, chmod 600; die Temp-Datei wird in jedem Fall entfernt.
//
// Läuft wie deployKeyScript in "sh -c '...'" (Login-Shell egal): keine
// einfachen Anführungszeichen, kein "!", keine Zeilenumbrüche.
//
// @date   2026-10-30 16:23
const removeKeyScript = `sh -c 'umask 077; set -C; KEY=$(cat); [ -n "$KEY" ] || exit 2; ` +
	`A="$HOME/.ssh/authorized_keys"; T="$HOME/.ssh/.authorized_keys_tmp"; [ -f "$A" ] || exit 0; ` +
	`rm -rf "$T" || exit 2; : > "$T" || exit 2; [ -f "$T" ] || exit 2; [ -L "$T" ] && exit 2; ` +
	`grep -vF -- "$KEY" "$A" >> "$T"; rc=$?; ` +
	`if [ "$rc" -gt 1 ]; then rm -f "$T"; exit 2; fi; ` +
	`n=$(grep -cvF -- "$KEY" "$A"); [ "$(wc -l < "$T")" -eq "$n" ] || { rm -f "$T"; exit 2; }; ` +
	`mv -f "$T" "$A" && chmod 600 "$A"; rc=$?; rm -f "$T"; exit $rc'`

// deployPublicKey fügt einen öffentlichen SSH-Key zur authorized_keys des
// Remote-Servers hinzu, ohne ihn doppelt einzutragen (siehe deployKeyScript).
// Überträgt den Key sicher über stdin (kein Shell-Escaping nötig).
// Der Key muss genau eine Zeile "Typ Base64 [Kommentar]" sein: Eine .pub mit
// mehreren Zeilen könnte sonst zusätzliche Einträge (etwa mit Optionen) in
// authorized_keys schmuggeln.
//
// @param client - Aktiver SSH-Client
// @param pubKeyStr - Öffentlicher Schlüssel im OpenSSH-Format
// @return error - Fehler beim Deployment
// @date   2026-10-28 13:16
func deployPublicKey(client *ssh.Client, pubKeyStr string) error {
	line := strings.TrimSpace(pubKeyStr)
	if strings.ContainsAny(line, "\r\n") || len(strings.Fields(line)) < 2 {
		return fmt.Errorf("ungültiges Public-Key-Format (erwartet eine Zeile \"Typ Base64 Kommentar\")")
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Public Key über stdin sicher übergeben (kein Shell-Escaping nötig)
	session.Stdin = strings.NewReader(line + "\n")
	if err := session.Run(deployKeyScript); err != nil {
		return fmt.Errorf("authorized_keys konnte nicht aktualisiert werden: %w", err)
	}

	return nil
}

// sanitizeFilename entfernt Zeichen die in Dateinamen nicht erlaubt sind.
// Ersetzt ungültige Zeichen durch Unterstriche.
//
// @param s - Eingabestring
// @return string - Bereinigter Dateiname
// @date   2026-03-15 00:00
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, s)
}

// autoDeployMu serialisiert ALLE Schlüssel-Abläufe dieses Pakets:
// AutoDeployKey (Hauptablauf in tui.go und Datei-Manager) und
// RemoveDeployedKey. Jeder Ablauf ist "Konfiguration lesen, entscheiden,
// Dateien und Server ändern, Konfiguration schreiben". Liefen zwei davon
// gleichzeitig, entschieden beide nach demselben veralteten Stand: zwei
// Vergaben mit gleichem Host und Benutzer wählten denselben Dateinamen,
// oder zwei Entfernungen geteilter Schlüssel hielten sich gegenseitig für
// den letzten Nutzer.
//
// Sperr-Reihenfolge (sonst Deadlock): fmDeployMu (Datei-Manager) ->
// autoDeployMu -> configUpdateMu (updateKeyFields/updateConfig) -> configFileMu
// (LoadConfig/SaveConfig). autoDeployMu wird nur am Anfang von
// AutoDeployKey und RemoveDeployedKey genommen und nie, während
// configUpdateMu gehalten wird. Innerhalb der Sperre ruft keine Funktion
// AutoDeployKey oder RemoveDeployedKey erneut auf (Go-Mutexe sind nicht
// wiedereintrittsfähig).
//
// Preis: Die Sperre umfasst auch den SSH-Befehl auf dem Server. Hängt ein
// Server, warten weitere Schlüssel-Abläufe, bis die Verbindung als tot gilt.
// Die Anmeldungen selbst blockiert das nicht.
//
// @date   2026-10-27 19:38
var autoDeployMu sync.Mutex

// keyLog schreibt Entscheidungen beim Verteilen und Entfernen von Schlüsseln
// ins Log (~/.ssh-easy/ssh-easy.log). Als Variable, damit Tests sie ersetzen
// können. NewLogger ist billig und schreibt im Append-Modus in dieselbe Datei
// wie der Logger aus main; so braucht ssh.go keinen eigenen Logger-Parameter.
//
// @date   2026-10-27 18:03
var keyLog = func(format string, args ...interface{}) {
	NewLogger().Info(format, args...)
}

// sameKeyPath prüft, ob zwei gespeicherte Schlüsselpfade dieselbe Datei meinen.
// Beide Seiten werden über expandTilde aufgelöst und normalisiert, so gilt
// "~/.ssh/x" gleich "/home/u/.ssh/x". Unter Windows wird Groß-/Kleinschreibung
// ignoriert (NTFS unterscheidet sie nicht). Leere Pfade sind nie gleich.
// Im Zweifel lieber "gleich": Das schützt eine fremde Schlüsseldatei vor dem Löschen.
//
// @param a - Erster Pfad (Tilde-Form oder absolut)
// @param b - Zweiter Pfad (Tilde-Form oder absolut)
// @return bool - true, wenn beide auf dieselbe Datei zeigen
// @date   2026-10-27 18:03
func sameKeyPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca := filepath.Clean(expandTilde(a))
	cb := filepath.Clean(expandTilde(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// connectionsSharingKey liefert alle ANDEREN Verbindungen (ID ungleich
// conn.ID), deren KeyPath auf dieselbe Datei zeigt wie conn.KeyPath.
// Hat conn keinen Schlüssel, kommt nil zurück; sonst teilten alle
// Verbindungen ohne Schlüssel "den" leeren Schlüssel.
//
// @param conn        - Verbindung, deren Schlüssel geprüft wird
// @param connections - Alle gespeicherten Verbindungen (darf conn enthalten)
// @return []Connection - Andere Nutzer desselben Schlüssels (nil, wenn keine)
// @date   2026-10-27 18:03
func connectionsSharingKey(conn Connection, connections []Connection) []Connection {
	if conn.KeyPath == "" {
		return nil
	}
	var shared []Connection
	for _, other := range connections {
		if other.ID == conn.ID {
			continue
		}
		if sameKeyPath(conn.KeyPath, other.KeyPath) {
			shared = append(shared, other)
		}
	}
	return shared
}

// autoKeyName wählt den Dateinamen für einen automatisch erzeugten Schlüssel.
// Grundname: id_ed25519_<Host>_<User>, beide über sanitizeFilename bereinigt
// (kein Pfadtrenner, kein Ausbruch aus ~/.ssh). Ist ~/.ssh/<Name> schon der
// KeyPath einer ANDEREN Verbindung, wird _2, _3 ... angehängt, bis der Name
// frei ist. Die eigene Verbindung blockiert ihren Namen nicht: Meldet sie sich
// erneut per Passwort an, bekommt sie denselben Namen wie zuvor.
// Die Schleife endet sicher: Höchstens len(connections) Namen können belegt sein.
//
// @param conn        - Verbindung, für die der Schlüssel erzeugt wird
// @param connections - Alle gespeicherten Verbindungen
// @return string - Freier Dateiname ohne Verzeichnis
// @date   2026-10-27 18:03
func autoKeyName(conn Connection, connections []Connection) string {
	base := fmt.Sprintf("id_ed25519_%s_%s",
		sanitizeFilename(conn.Host),
		sanitizeFilename(conn.User))

	// taken prüft, ob eine andere Verbindung ~/.ssh/<name> als Schlüssel nutzt.
	taken := func(name string) bool {
		candidate := "~/.ssh/" + name
		for _, other := range connections {
			if other.ID != conn.ID && sameKeyPath(candidate, other.KeyPath) {
				return true
			}
		}
		return false
	}

	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s_%d", base, i)
		if !taken(name) {
			return name
		}
	}
}

// errKeyConnectionNotFound meldet, dass die Verbindung (ID) nicht mehr in der
// Konfiguration steht, z. B. weil sie inzwischen gelöscht wurde.
var errKeyConnectionNotFound = errors.New("Verbindung nicht in der Konfiguration")

// findConnectionByID sucht eine Verbindung anhand ihrer ID.
//
// @param connections - Gespeicherte Verbindungen
// @param id          - Gesuchte ID
// @return bool - true, wenn eine Verbindung mit dieser ID existiert
// @date   2026-10-28 13:16
func findConnectionByID(connections []Connection, id string) bool {
	for i := range connections {
		if connections[i].ID == id {
			return true
		}
	}
	return false
}

// updateKeyFields ändert gezielt Felder EINES gespeicherten Eintrags (gleiche
// ID), statt die übergebene Verbindung komplett zurückzuschreiben (Build 86,
// F6). Die übergebene Verbindung kann veraltet sein - etwa ohne die Tunnel,
// die inzwischen per e gespeichert wurden. Alles außer den Feldern, die change
// setzt, bleibt so, wie es in der Datei steht - auch UpdatedAt: Das setzt
// change selbst, wenn es dazugehört (nicht beim bloßen Zählen eines
// Fehlversuchs).
//
// change bekommt einen Zeiger auf den gespeicherten Eintrag. So setzen
// Erfolg, Fehlschlag und Entfernen ihre Felder ("Merken und aussetzen":
// AutoKeyOff, AutoKeyFailures) im selben Schritt, ohne neue Lese-Schreib-Runde.
//
// Läuft über updateConfig, also unter configUpdateMu. Aufrufer halten bereits
// autoDeployMu; die Reihenfolge autoDeployMu -> configUpdateMu bleibt gewahrt.
// Gibt es die ID nicht, wird nichts geschrieben und errKeyConnectionNotFound
// gemeldet.
//
// @param configPath - Pfad zur Konfigurationsdatei
// @param id         - ID der zu ändernden Verbindung
// @param change     - Setzt die gewünschten Felder am gespeicherten Eintrag
// @return error - errKeyConnectionNotFound oder Fehler beim Laden/Speichern
// @date   2026-10-28 13:23
func updateKeyFields(configPath, id string, change func(stored *Connection)) error {
	found := false
	err := updateConfig(configPath, func(cfg *AppConfig) bool {
		for i := range cfg.Connections {
			if cfg.Connections[i].ID != id {
				continue
			}
			change(&cfg.Connections[i])
			found = true
			return true
		}
		// Unbekannte ID: nichts speichern, Datei bleibt Byte für Byte gleich
		return false
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w (ID %s)", errKeyConnectionNotFound, id)
	}
	return nil
}

// recordAutoKeyFailure zählt einen gescheiterten Vergabeversuch
// ("Merken und aussetzen", Build 86): AutoKeyFailures des gespeicherten
// Eintrags +1, sonst nichts - AutoKeyOff, AuthType, KeyPath und UpdatedAt
// bleiben. Ab autoKeyMaxFailures vergibt der Auto-Login (ohne Passwort) keinen
// Schlüssel mehr; die Entscheidung trifft connectionNeedsOwnKey.
// Scheitert das Zählen selbst (Konfiguration nicht schreibbar, ID weg), wird
// das nur protokolliert; der Aufrufer meldet weiter den ursprünglichen Fehler.
//
// @param configPath - Pfad zur Konfigurationsdatei
// @param conn       - Verbindung (ID und Name werden genutzt)
// @param cause      - Ursprünglicher Fehler (nur fürs Log)
// @date   2026-10-28 13:23
func recordAutoKeyFailure(configPath string, conn Connection, cause error) {
	failures := 0
	err := updateKeyFields(configPath, conn.ID, func(stored *Connection) {
		stored.AutoKeyFailures++
		failures = stored.AutoKeyFailures
	})
	if err != nil {
		keyLog("Schlüsselvergabe für Verbindung %q gescheitert (%v); Fehlversuch nicht gezählt: %v", conn.Name, cause, err)
		return
	}
	keyLog("Schlüsselvergabe für Verbindung %q gescheitert, Fehlversuch %d von %d: %v", conn.Name, failures, autoKeyMaxFailures, cause)
}

// AutoDeployKey generiert einen neuen SSH-Key und deployt ihn automatisch
// auf dem Remote-Server nach einer erfolgreichen Passwort-Anmeldung.
// Aktualisiert auch die Verbindungskonfiguration auf Key-Auth.
//
// Der Dateiname kommt aus autoKeyName: Gehört der Grundname schon einer
// anderen Verbindung (z. B. einer Kopie mit Taste c), bekommt diese Verbindung
// einen eigenen Schlüssel (_2, _3 ...). Eine vorhandene Datei, die keiner
// anderen Verbindung gehört, wird wie bisher wiederverwendet.
//
// In der Konfiguration ändert sich nur der Eintrag mit derselben ID
// (updateKeyFields); conn darf veraltet sein. Fehlt die ID, bricht die
// Funktion vor jeder Änderung mit einem Fehler ab.
//   - Erfolg: AuthType key, KeyPath, UpdatedAt, AutoKeyOff=false,
//     AutoKeyFailures=0.
//   - Fehlschlag bei Erzeugung, .pub oder Server: nur AutoKeyFailures+1
//     (recordAutoKeyFailure). Ladefehler und unbekannte ID zählen nicht.
//
// @param conn - Verbindungskonfiguration (ID, Host, User, Name werden genutzt)
// @param client - Aktiver SSH-Client (nach Passwort-Login)
// @param configPath - Pfad zur Konfigurationsdatei für Update
// @return string - Pfad zum generierten Key (~/.ssh/...)
// @return error - Fehler bei Generierung oder Deployment
// @date   2026-10-28 13:23
func AutoDeployKey(conn Connection, client *ssh.Client, configPath string) (string, error) {
	// Ganzer Ablauf unter autoDeployMu: Name wählen, Datei erzeugen,
	// hinterlegen und updateKeyFields. Erst danach sieht die nächste
	// Vergabe den belegten Namen und wählt _2, _3 ...
	autoDeployMu.Lock()
	defer autoDeployMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Home-Verzeichnis nicht ermittelbar: %w", err)
	}

	// Verbindungen laden, um einen freien Namen zu wählen.
	// Entscheidung bei Ladefehler: abbrechen, NICHT auf den alten Namen
	// zurückfallen. Begründung:
	//   - Ohne die Liste ist unbekannt, ob der Grundname einer anderen
	//     Verbindung gehört. Ein Rückfall würde genau den Fehler von Build 85
	//     wieder erzeugen (zwei Verbindungen, eine Datei).
	//   - updateKeyFields lädt am Ende dieselbe Datei. Scheitert das Laden
	//     jetzt, scheitert es dort sehr wahrscheinlich auch. Dann läge ein
	//     Schlüssel auf dem Server, den die Konfiguration nicht kennt.
	// Abbruch vor jeder Änderung hinterlässt weder Datei noch Server-Eintrag.
	// Die Anmeldung per Passwort bleibt bestehen; nur der Schlüssel fehlt.
	// Eine fehlende Konfigurationsdatei liefert LoadConfig als leere Liste;
	// dann schlägt die folgende ID-Prüfung an.
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("Key-Deployment abgebrochen, Konfiguration nicht lesbar: %w", err)
	}
	// Gibt es die Verbindung nicht mehr, gar nicht erst Schlüssel erzeugen
	// oder auf dem Server hinterlegen (er gehörte sonst niemandem).
	if !findConnectionByID(cfg.Connections, conn.ID) {
		return "", fmt.Errorf("Key-Deployment abgebrochen: %w (ID %s)", errKeyConnectionNotFound, conn.ID)
	}

	keyName := autoKeyName(conn, cfg.Connections)
	keyPath := filepath.Join(home, ".ssh", keyName)
	keyPathTilde := "~/.ssh/" + keyName

	// Ab hier zählt jeder Fehlschlag als Fehlversuch (Merken und aussetzen):
	// Schlüsselerzeugung, Lesen der .pub und Hinterlegen auf dem Server.
	// Ladefehler und unbekannte ID oben zählen nicht - dort ist die
	// Konfiguration nicht beschreibbar bzw. der Eintrag gar nicht da.
	if err := provideAndDeployKey(client, keyPath); err != nil {
		recordAutoKeyFailure(configPath, conn, err)
		return "", err
	}
	keyLog("Schlüssel %s für Verbindung %q auf dem Server hinterlegt", keyPathTilde, conn.Name)

	// Verbindungskonfiguration auf Key-Auth umstellen - nur AuthType, KeyPath,
	// UpdatedAt und der Zustand der automatischen Vergabe (F6, Merken und
	// aussetzen), nie die ganze, womöglich veraltete Verbindung. Ein Erfolg
	// schaltet die Vergabe wieder ein und vergisst frühere Fehlversuche.
	if err := updateKeyFields(configPath, conn.ID, func(stored *Connection) {
		stored.AuthType = AuthKey
		stored.KeyPath = keyPathTilde
		stored.UpdatedAt = time.Now().Format(time.RFC3339)
		stored.AutoKeyOff = false
		stored.AutoKeyFailures = 0
	}); err != nil {
		// Nicht kritisch - Key ist deployed, nur Config-Update fehlgeschlagen
		return keyPathTilde, fmt.Errorf("Key hinterlegt, aber Konfiguration nicht gespeichert: %w", err)
	}

	return keyPathTilde, nil
}

// provideAndDeployKey sorgt für das Schlüsselpaar unter keyPath und hinterlegt
// den öffentlichen Teil auf dem Server: Gibt es die Datei schon (sie gehört
// laut autoKeyName keiner anderen Verbindung, z. B. erneute Anmeldung
// derselben Verbindung), wird ihre .pub wiederverwendet; sonst entsteht ein
// neuer Ed25519-Schlüssel. Jeder Fehler hier ist ein Fehlversuch im Sinne von
// recordAutoKeyFailure.
//
// @param client  - Aktiver SSH-Client
// @param keyPath - Absoluter Pfad des privaten Schlüssels
// @return error - Fehler bei Erzeugung, Lesen der .pub oder Hinterlegen (ohne eigenes Präfix)
// @date   2026-10-30 16:13
func provideAndDeployKey(client *ssh.Client, keyPath string) error {
	if _, err := os.Stat(keyPath); err == nil {
		// Key existiert - nur Public Key deployen
		pubKeyData, err := os.ReadFile(keyPath + ".pub")
		if err != nil {
			return fmt.Errorf("Existierender Public Key konnte nicht gelesen werden: %w", err)
		}
		return deployPublicKey(client, string(pubKeyData))
	}

	// Neuen Ed25519-Key generieren
	pubKeyStr, err := GenerateSSHKey(keyPath, "")
	if err != nil {
		return fmt.Errorf("Schlüssel konnte nicht erzeugt werden: %w", err)
	}

	// Key auf Remote-Server deployen. Kein eigenes Präfix: Die Oberfläche
	// setzt m.lang.KeyDeployFailed ("Key-Deployment fehlgeschlagen: ") schon
	// davor; früher stand es dann doppelt da (Fable-Review Runde 2, N5).
	return deployPublicKey(client, pubKeyStr)
}

// sharesServerEntry prüft, ob eine der Verbindungen denselben Server-Eintrag
// in authorized_keys nutzt: gleicher Host (Groß-/Kleinschreibung egal) und
// gleicher Benutzer. Der Port zählt NICHT: Ein zweiter sshd auf demselben Host
// liest meist dieselbe ~/.ssh/authorized_keys.
//
// @param conn   - Verbindung, deren Schlüssel entfernt wird
// @param others - Andere Verbindungen mit demselben Schlüssel
// @return bool - true, wenn der Server-Eintrag stehen bleiben muss
// @date   2026-10-27 18:03
func sharesServerEntry(conn Connection, others []Connection) bool {
	for _, other := range others {
		if strings.EqualFold(other.Host, conn.Host) && other.User == conn.User {
			return true
		}
	}
	return false
}

// RemoveDeployedKey entfernt einen SSH-Key vollständig:
//  1. Public Key aus ~/.ssh/authorized_keys auf dem Remote-Server
//  2. Lokale Private-Key-Datei
//  3. Lokale Public-Key-Datei (.pub)
//  4. Verbindungs-Config zurück auf Passwort-Auth (KeyPath geleert); nur
//     AuthType, KeyPath, UpdatedAt und AutoKeyOff=true des gespeicherten
//     Eintrags (F6, Merken und aussetzen).
//     Fehlt die ID in der Konfiguration, bricht die Funktion vorher ab.
//
// Geteilte Schlüssel (seit Build 86): Nutzt eine andere Verbindung dieselbe
// Schlüsseldatei, bleiben die lokalen Dateien liegen. Hat eine solche
// Verbindung zusätzlich denselben Host und Benutzer (Port egal), bleibt auch
// der Server-Eintrag stehen. Zurückgesetzt wird immer nur DIESE Verbindung.
// Die Entscheidung landet im Log; einen neuen UI-Text gibt es dafür nicht.
//
// Wird über stdin übergeben um Shell-Escaping von Key-Daten zu vermeiden.
// Fehler beim Löschen lokaler Dateien werden ignoriert (Key könnte schon fehlen).
//
// @param conn       - Verbindungskonfiguration mit KeyPath
// @param client     - Aktiver SSH-Client für Remote-Befehl (nil = nur lokal)
// @param configPath - Pfad zur Konfigurationsdatei
// @return error     - Fehler beim Laden der Konfiguration, beim Remote-Befehl oder Config-Update
// @date   2026-10-28 13:23
func RemoveDeployedKey(conn Connection, client *ssh.Client, configPath string) error {
	// Unter autoDeployMu (siehe dort): Die Entscheidung "geteilt oder nicht"
	// gilt nur, solange niemand zwischen Laden und updateKeyFields einen
	// Schlüssel vergibt oder einen anderen Nutzer derselben Datei zurücksetzt.
	autoDeployMu.Lock()
	defer autoDeployMu.Unlock()

	keyPath := expandTilde(conn.KeyPath)
	pubKeyPath := keyPath + ".pub"

	// Verbindungen laden, um geteilte Schlüssel zu erkennen. Scheitert das,
	// wird vor jeder Änderung abgebrochen: Ohne die Liste könnte eine fremde
	// Schlüsseldatei gelöscht werden, und das abschließende updateKeyFields
	// würde an derselben Datei ohnehin scheitern.
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("Key nicht entfernt, Konfiguration nicht lesbar: %w", err)
	}
	// Gibt es die Verbindung nicht mehr, nichts löschen: Ihr Schlüssel könnte
	// inzwischen einer anderen Verbindung gehören, und zurücksetzen ginge nicht.
	if !findConnectionByID(cfg.Connections, conn.ID) {
		return fmt.Errorf("Key nicht entfernt: %w (ID %s)", errKeyConnectionNotFound, conn.ID)
	}
	shared := connectionsSharingKey(conn, cfg.Connections)
	keepServerEntry := sharesServerEntry(conn, shared)

	// Public Key lesen – nötig für den Remote-Abgleich
	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("Public Key nicht lesbar (%s): %w", pubKeyPath, err)
	}

	// Zweites Feld der pub-Key-Zeile = reiner Base64-Key ohne Typ und Kommentar.
	// grep -vF matcht als fester String (kein Regex) – sicher für beliebige Key-Daten.
	parts := strings.Fields(string(pubKeyData))
	if len(parts) < 2 {
		return fmt.Errorf("ungültiges Public-Key-Format in %s", pubKeyPath)
	}
	keyData := parts[1] // nur Base64-Teil – eindeutig und regex-sicher

	// Key vom Server entfernen (über bestehende SSH-Verbindung),
	// außer eine andere Verbindung braucht genau diesen Eintrag noch
	if client != nil && !keepServerEntry {
		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("SSH-Session für Key-Entfernung nicht möglich: %w", err)
		}
		defer session.Close()

		// grep -vF filtert die Zeile mit dem Key heraus (siehe removeKeyScript).
		// Temp-Datei verhindert dass authorized_keys bei grep-Fehler geleert wird.
		session.Stdin = strings.NewReader(keyData)
		if err := session.Run(removeKeyScript); err != nil {
			return fmt.Errorf("Key konnte nicht vom Server entfernt werden: %w", err)
		}
	}

	if len(shared) > 0 {
		// Lokale Dateien behalten: andere Verbindungen nutzen sie noch
		names := make([]string, 0, len(shared))
		for _, other := range shared {
			names = append(names, fmt.Sprintf("%q", other.Name))
		}
		serverAction := "entfernt"
		if client == nil {
			serverAction = "nicht berührt (keine Verbindung)"
		} else if keepServerEntry {
			serverAction = "behalten (gleicher Host und Benutzer)"
		}
		keyLog("Schlüssel %s von Verbindung %q gelöst: lokale Dateien behalten, weiter genutzt von %s; Server-Eintrag %s",
			conn.KeyPath, conn.Name, strings.Join(names, ", "), serverAction)
	} else {
		// Lokale Key-Dateien löschen (Fehler ignorieren – könnten schon fehlen)
		_ = os.Remove(keyPath)
		_ = os.Remove(pubKeyPath)
	}

	// Verbindungs-Config zurücksetzen: AuthType=Password, KeyPath leer -
	// nur diese Felder des gespeicherten Eintrags (F6). Dazu AutoKeyOff=true
	// (Merken und aussetzen): Sonst vergäbe der nächste Auto-Login sofort
	// wieder einen Schlüssel, den der Nutzer eben entfernt hat. Erst eine
	// Passwort-Anmeldung mit erfolgreicher Vergabe schaltet das wieder ab.
	// AutoKeyFailures bleibt; andere Verbindungen (auch mit geteiltem
	// Schlüssel) bleiben unberührt.
	if err := updateKeyFields(configPath, conn.ID, func(stored *Connection) {
		stored.AuthType = AuthPassword
		stored.KeyPath = ""
		stored.UpdatedAt = time.Now().Format(time.RFC3339)
		stored.AutoKeyOff = true
	}); err != nil {
		return fmt.Errorf("Key entfernt, Config-Update fehlgeschlagen: %w", err)
	}

	return nil
}
