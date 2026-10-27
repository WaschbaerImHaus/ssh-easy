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
// @date   2026-10-16 10:00
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// errAltF4Quit signalisiert der TUI dass der Nutzer die Session mit Alt+F4
// beendet hat und das gesamte Programm geschlossen werden soll (wie PuTTY).
var errAltF4Quit = errors.New("Alt+F4: Programm-Beenden angefordert")

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
	// Speichert Fenster- und Schriftgröße während der Sitzung (nil = nicht).
	// Sonst gingen Änderungen verloren, wenn das Fenster per X geschlossen
	// wird - Windows beendet den Prozess dann nach ca. 5 s.
	saveConsoleState func()
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
// @date   2026-10-16 10:00
func (t *sshTerminalCmd) Run() error {
	// Stdin-Puffer als ERSTES defer registrieren → wird als LETZTES ausgeführt (LIFO).
	// Muss nach allen anderen Defers laufen: term.Restore und restoreVT können beim
	// Konsolen-Mode-Wechsel selbst Input-Events erzeugen. Würde der Flush vorher laufen,
	// blieben diese Events im Puffer und Bubbletea würde sie als ersten Tastendruck lesen –
	// der Nutzer müsste dann zweimal drücken.
	defer flushStdinBuffer()

	// Zwei getrennte Dateideskriptoren:
	// stdinFd  → Raw-Modus (Tastatureingabe ungepuffert)
	// stdoutFd → Terminalgröße (Windows: GetConsoleScreenBufferInfo braucht Output-Handle)
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())

	// Windows: Virtual-Terminal-Verarbeitung aktivieren damit ANSI/VT-Escape-Sequenzen
	// (Farben, Cursorbewegungen) korrekt dargestellt werden statt als Rohtext.
	// Auf Unix ist dies ein No-Op.
	restoreVT := setupConsoleVT()
	defer restoreVT()

	// Raw-Modus aktivieren: Tastendrücke werden sofort weitergeleitet (kein Zeilenpuffer).
	// Wichtig für interaktive Programme wie mc, vim, bash etc.
	// Auch nach Alt-Tab (Windows) stellt dies den korrekten Modus sicher.
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		// Warnung: kein Raw-Modus möglich (z.B. kein echtes Terminal).
		// Trotzdem fortfahren – einfache Befehle funktionieren noch.
		oldState = nil
	}
	// Terminal-Zustand beim Beenden immer wiederherstellen
	defer func() {
		if oldState != nil {
			term.Restore(stdinFd, oldState)
		}
	}()

	// Neue SSH-Session für die Shell erstellen
	session, err := t.client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH-Session konnte nicht erstellt werden: %w", err)
	}
	defer session.Close()

	// Direkt auf os.Stdout/Stderr arbeiten (nicht Bubbleteas gefilterter Stream).
	// Das umgeht den coninput-Reader auf Windows, der nach Alt-Tab instabil werden kann.
	// Davor sitzt je ein OSC-52-Filter: Remote-Programme (z.B. Claude Code)
	// kopieren per OSC 52 in die Zwischenablage, was conhost nicht beherrscht.
	// pasteMode merkt sich, ob die Remote-Shell Bracketed Paste aktiviert hat
	// (ESC[?2004h) - dann wird Shift+Einfg-Text sicher eingeschlossen.
	pasteMode := &bracketedPasteMode{}
	flushOutput := attachOutputFilters(session, os.Stdout, os.Stderr, pasteMode)

	// Eigene stdin-Weiterleitung statt session.Stdin = os.Stdin: So können wir
	// die Shift+Insert-VT-Sequenz abfangen und stattdessen den Clipboard-Inhalt
	// einspeisen. Ohne diese Behandlung würde "Einfügen" auf Windows nicht
	// funktionieren, weil conhost Shift+Insert nicht selbst abfängt.
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("Stdin-Pipe konnte nicht erstellt werden: %w", err)
	}
	// Pipe schließen, sobald die Session endet - signalisiert der
	// Forwarder-Goroutine zu beenden (ihr nächster Write liefert dann Fehler).
	defer stdinPipe.Close()

	// PTY-Modus: minimale Einstellungen – das Remote-PTY übernimmt die Steuerung
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Echo auf Remote-Seite aktivieren
		ssh.TTY_OP_ISPEED: 38400, // Eingabe-Baudrate
		ssh.TTY_OP_OSPEED: 38400, // Ausgabe-Baudrate
	}

	// Terminalgröße vom stdout-Handle ermitteln (auf Windows zwingend Output-Handle nötig).
	// Fallback auf die zuletzt bekannte Größe aus Bubbleteas WindowSizeMsg.
	width, height, sizeErr := term.GetSize(stdoutFd)
	if sizeErr != nil || width <= 0 || height <= 0 {
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

	// Stdin-Forwarder-Goroutine starten: liest roh von os.Stdin, fängt
	// Shift+Insert-VT-Sequenz ab (Clipboard-Paste) und erkennt Alt+F4
	// (Programm beenden). Beendet sich automatisch, sobald stdinPipe durch
	// defer geschlossen wird oder Alt+F4 gedrückt wurde.
	// Abbrechbare Eingabe: Nach Sitzungsende muss die Weiterleitung gestoppt
	// werden, sonst verschluckt sie den ersten Tastendruck danach (siehe
	// terminal_input.go). Ist stdin keine Konsole, wird direkt gelesen.
	var input terminalInput
	var source io.Reader = os.Stdin
	if in, inErr := newTerminalInput(os.Stdin); inErr == nil {
		input = in
		source = in
		defer in.Close()
	}
	forwarderDone := make(chan struct{})
	var altF4Pressed atomic.Bool
	go func() {
		defer close(forwarderDone)
		forwardStdinWithPaste(source, stdinPipe, func() {
			altF4Pressed.Store(true)
			// Session schließen damit session.Wait() unten sofort zurückkehrt
			session.Close()
		}, pasteMode)
	}()

	// Terminalgröße-Änderungen weiterleiten (SIGWINCH-Äquivalent für SSH).
	// Läuft in einer Goroutine parallel zur Shell-Session.
	stopResize := make(chan struct{})
	var resizeWg sync.WaitGroup
	resizeWg.Add(1)
	go func() {
		defer resizeWg.Done()
		// stdoutFd übergeben – Größenabfrage muss auf Output-Handle erfolgen (Windows!)
		watchTerminalResize(session, stdoutFd, width, height, stopResize, newConsoleStateSaver(t.saveConsoleState))
	}()

	// Warten bis der Nutzer die Session beendet (z.B. mit "exit" oder Ctrl+D)
	sessionErr := session.Wait()

	// Wait() wartet auch auf die Ausgabe-Kopierer der SSH-Bibliothek - danach
	// kommt nichts mehr nach, zurückgehaltene Bytes können ausgegeben werden.
	flushOutput()

	// Größen-Watcher beenden
	close(stopResize)
	resizeWg.Wait()

	// stdin-Weiterleitung stoppen, BEVOR Bubbletea stdin wieder übernimmt -
	// sonst landet der nächste Tastendruck noch in der beendeten Sitzung
	if input != nil {
		stopInputForwarder(input, forwarderDone, inputForwarderStopTimeout)
	}

	// Alt+F4 hat die Session hart geschlossen: den dadurch entstandenen
	// Wait-Fehler nicht als Session-Fehler melden, sondern das Programm-
	// Beenden an die TUI signalisieren (dort: DisconnectAll + tea.Quit).
	if altF4Pressed.Load() {
		return errAltF4Quit
	}

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
// @param saver - speichert Fenster- und Schriftgröße höchstens alle 2 s
// @date   2026-10-27 17:20
func watchTerminalResize(session *ssh.Session, fd, initialWidth, initialHeight int, stop <-chan struct{}, saver *consoleStateSaver) {
	lastW, lastH := initialWidth, initialHeight

	for {
		select {
		case <-stop:
			return
		default:
			// Waagrechten Bildlauf der Windows-Konsole entfernen (sonst
			// verdeckt der Balken die letzte Zeile); danach ändert sich
			// ggf. die Höhe und wird unten an den Server gemeldet
			fixHorizontalScroll()
			// Aktuelle Terminalgröße prüfen
			w, h, err := term.GetSize(fd)
			if err == nil && (w != lastW || h != lastH) {
				// Größe hat sich geändert: SSH-PTY informieren
				_ = session.WindowChange(h, w)
				lastW, lastH = w, h
			}
			// Fenster- und Schriftgröße sofort sichern (nicht erst beim Beenden)
			saver.tick(time.Now())
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
