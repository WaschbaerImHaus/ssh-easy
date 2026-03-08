// Paket main - Interaktive SSH-Terminal-Session für ssh-easy
//
// Implementiert das tea.ExecCommand-Interface von Bubbletea, um eine
// vollständige interaktive Shell-Session über eine bestehende SSH-Verbindung
// zu starten.
//
// Windows-Besonderheit: Bubbletea übergibt auf Windows einen gefilterten
// coninput-Reader als stdin. Nach Alt-Tab kann dieser den Console-Modus
// verlieren. Deshalb: Raw-Modus explizit über golang.org/x/term setzen
// und direkt auf os.Stdin/os.Stdout arbeiten statt auf Bubbleteas Streams.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// sshTerminalCmd implementiert das tea.ExecCommand-Interface.
// Startet eine interaktive PTY-Shell-Session über einen bestehenden SSH-Client.
// Die TUI wird während der Session pausiert und danach wiederhergestellt.
type sshTerminalCmd struct {
	// Bestehender SSH-Client der aktiven Verbindung
	client *ssh.Client
	// stdin/stdout/stderr werden von tea.Exec gesetzt, aber wir nutzen os.Std* direkt
	// (Windows: Bubbletea-eigener coninput-Reader verliert nach Alt-Tab den Console-Modus)
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// Terminalbreite in Zeichen (aus letztem WindowSizeMsg)
	width int
	// Terminalhöhe in Zeilen (aus letztem WindowSizeMsg)
	height int
}

// SetStdin wird von tea.Exec aufgerufen – wir speichern ihn, nutzen aber os.Stdin direkt.
//
// @param r - Eingabe-Stream von Bubbletea
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStdin(r io.Reader) { t.stdin = r }

// SetStdout wird von tea.Exec aufgerufen – wir speichern ihn, nutzen aber os.Stdout direkt.
//
// @param w - Ausgabe-Stream von Bubbletea
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStdout(w io.Writer) { t.stdout = w }

// SetStderr wird von tea.Exec aufgerufen – wir speichern ihn, nutzen aber os.Stderr direkt.
//
// @param w - Fehler-Stream von Bubbletea
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) SetStderr(w io.Writer) { t.stderr = w }

// Run startet die interaktive SSH-Shell und blockiert bis zum Ende der Session.
//
// Ablauf:
//  1. Terminal-Zustand sichern und Raw-Modus aktivieren (direkter Byte-Durchsatz)
//  2. SSH-Session erstellen und PTY anfordern
//  3. Shell starten und stdin/stdout/stderr verbinden
//  4. Warten bis der Nutzer "exit" eingibt oder die Verbindung abbricht
//  5. Terminal-Zustand wiederherstellen
//
// @return error - Fehler beim Starten der Session oder nil bei normalem Ende
// @date   2026-03-08 00:00
func (t *sshTerminalCmd) Run() error {
	// Terminal-Dateideskriptor (für Raw-Modus und Größenabfrage)
	fd := int(os.Stdin.Fd())

	// Raw-Modus aktivieren: Tastendrücke werden sofort weitergeleitet (kein Zeilenpuffer).
	// Wichtig für interaktive Programme wie mc, vim, bash etc.
	// Auch nach Alt-Tab (Windows) stellt dies den korrekten Modus sicher.
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Warnung: kein Raw-Modus möglich (z.B. kein echtes Terminal).
		// Trotzdem fortfahren – einfache Befehle funktionieren noch.
		oldState = nil
	}
	// Terminal-Zustand beim Beenden immer wiederherstellen
	defer func() {
		if oldState != nil {
			term.Restore(fd, oldState)
		}
	}()

	// Neue SSH-Session für die Shell erstellen
	session, err := t.client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Direkt auf os.Stdin/Stdout/Stderr arbeiten (nicht Bubbleteas gefilterter Stream).
	// Das umgeht den coninput-Reader auf Windows, der nach Alt-Tab instabil werden kann.
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	// PTY-Modus: minimale Einstellungen – das Remote-PTY übernimmt die Steuerung
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Echo auf Remote-Seite aktivieren
		ssh.TTY_OP_ISPEED: 38400, // Eingabe-Baudrate
		ssh.TTY_OP_OSPEED: 38400, // Ausgabe-Baudrate
	}

	// Terminalgröße ermitteln: zuerst live vom OS, dann Fallback aus WindowSizeMsg
	width, height, sizeErr := term.GetSize(fd)
	if sizeErr != nil || width <= 0 || height <= 0 {
		// Fallback auf zuletzt bekannte Größe aus Bubbletea
		width, height = t.width, t.height
	}
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

	// Terminalgröße-Änderungen weiterleiten (SIGWINCH-Äquivalent für SSH).
	// Läuft in einer Goroutine parallel zur Shell-Session.
	stopResize := make(chan struct{})
	var resizeWg sync.WaitGroup
	resizeWg.Add(1)
	go func() {
		defer resizeWg.Done()
		watchTerminalResize(session, fd, width, height, stopResize)
	}()

	// Warten bis der Nutzer die Session beendet (z.B. mit "exit" oder Ctrl+D)
	sessionErr := session.Wait()

	// Größen-Watcher beenden
	close(stopResize)
	resizeWg.Wait()

	return sessionErr
}

// watchTerminalResize überwacht Terminalgröße-Änderungen und sendet sie an die SSH-Session.
// Läuft als Goroutine solange die Shell-Session aktiv ist.
//
// @param session - Aktive SSH-Session
// @param fd - Terminal-Dateideskriptor
// @param initialWidth - Startbreite
// @param initialHeight - Starthöhe
// @param stop - Kanal zum Beenden der Goroutine
// @date   2026-03-08 00:00
func watchTerminalResize(session *ssh.Session, fd, initialWidth, initialHeight int, stop <-chan struct{}) {
	lastW, lastH := initialWidth, initialHeight

	for {
		select {
		case <-stop:
			return
		default:
			// Aktuelle Terminalgröße prüfen
			w, h, err := term.GetSize(fd)
			if err == nil && (w != lastW || h != lastH) {
				// Größe hat sich geändert: SSH-PTY informieren
				_ = session.WindowChange(h, w)
				lastW, lastH = w, h
			}
			// Kurze Pause um CPU-Auslastung zu begrenzen
			waitForResizeSignal(stop)
		}
	}
}

// newSSHTerminalCmd erstellt einen neuen SSH-Terminal-Befehl.
//
// @param client - Bestehender, verbundener SSH-Client
// @param width - Aktuelle Terminalbreite in Zeichen (0 = automatisch ermitteln)
// @param height - Aktuelle Terminalhöhe in Zeilen (0 = automatisch ermitteln)
// @return *sshTerminalCmd - Bereit für tea.Exec
// @date   2026-03-08 00:00
func newSSHTerminalCmd(client *ssh.Client, width, height int) *sshTerminalCmd {
	return &sshTerminalCmd{
		client: client,
		width:  width,
		height: height,
	}
}
