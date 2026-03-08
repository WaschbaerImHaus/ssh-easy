// Paket main - Interaktive SSH-Terminal-Session für ssh-easy
//
// Implementiert das tea.ExecCommand-Interface von Bubbletea, um eine
// vollständige interaktive Shell-Session über eine bestehende SSH-Verbindung
// zu starten. Die TUI wird während der Session pausiert und danach wiederhergestellt.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// sshTerminalCmd implementiert das tea.ExecCommand-Interface.
// Startet eine interaktive PTY-Shell-Session über einen bestehenden SSH-Client.
// Bubbletea pausiert seine eigene TUI während Run() läuft und stellt sie danach wieder her.
type sshTerminalCmd struct {
	// Bestehender SSH-Client der aktiven Verbindung
	client *ssh.Client
	// Eingabe-Stream (von tea.Exec gesetzt: echtes Terminal-stdin)
	stdin io.Reader
	// Ausgabe-Stream (von tea.Exec gesetzt: echtes Terminal-stdout)
	stdout io.Writer
	// Fehler-Stream (von tea.Exec gesetzt: echtes Terminal-stderr)
	stderr io.Writer
	// Terminalbreite in Zeichen (aus letztem WindowSizeMsg)
	width int
	// Terminalhöhe in Zeilen (aus letztem WindowSizeMsg)
	height int
}

// SetStdin wird von tea.Exec aufgerufen um den Eingabe-Stream zu setzen.
//
// @param r - Eingabe-Stream (normalerweise os.Stdin im Raw-Modus)
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStdin(r io.Reader) { t.stdin = r }

// SetStdout wird von tea.Exec aufgerufen um den Ausgabe-Stream zu setzen.
//
// @param w - Ausgabe-Stream (normalerweise os.Stdout)
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStdout(w io.Writer) { t.stdout = w }

// SetStderr wird von tea.Exec aufgerufen um den Fehler-Stream zu setzen.
//
// @param w - Fehler-Stream (normalerweise os.Stderr)
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStderr(w io.Writer) { t.stderr = w }

// Run startet die interaktive SSH-Shell und blockiert bis zum Ende der Session.
// Bubbletea ruft diese Methode in einem eigenen Kontext auf und gibt dem Prozess
// die volle Terminalsteuerung (Raw-Modus, kein Bubbletea-Rendering).
//
// @return error - Fehler beim Starten der Session oder nil bei normalem Ende
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) Run() error {
	// Neue SSH-Session für die Shell erstellen
	session, err := t.client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Datenströme verbinden (Bubbletea hat Terminal in Raw-Modus gesetzt)
	session.Stdin = t.stdin
	session.Stdout = t.stdout
	session.Stderr = t.stderr

	// PTY-Konfiguration: Terminal-Emulation und Übertragungsrate
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Zeichenecho aktivieren
		ssh.TTY_OP_ISPEED: 38400, // Eingabe-Baudrate
		ssh.TTY_OP_OSPEED: 38400, // Ausgabe-Baudrate
	}

	// Terminalgröße verwenden (aus Bubbletea WindowSizeMsg, Fallback 80x24)
	width, height := t.width, t.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	// Pseudo-Terminal auf dem Remote-Server anfordern
	if err := session.RequestPty("xterm-256color", height, width, modes); err != nil {
		return fmt.Errorf("PTY konnte nicht angefordert werden: %w", err)
	}

	// Remote-Shell starten
	if err := session.Shell(); err != nil {
		return fmt.Errorf("Shell konnte nicht gestartet werden: %w", err)
	}

	// Blockieren bis der Nutzer die Session beendet (z.B. mit "exit")
	return session.Wait()
}

// newSSHTerminalCmd erstellt einen neuen SSH-Terminal-Befehl.
//
// @param client - Bestehender, verbundener SSH-Client
// @param width - Aktuelle Terminalbreite in Zeichen (0 = Fallback 80)
// @param height - Aktuelle Terminalhöhe in Zeilen (0 = Fallback 24)
// @return *sshTerminalCmd - Bereit für tea.Exec
// @date   2026-03-08 00:00
func newSSHTerminalCmd(client *ssh.Client, width, height int) *sshTerminalCmd {
	return &sshTerminalCmd{
		client: client,
		width:  width,
		height: height,
	}
}
