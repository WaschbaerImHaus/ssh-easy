// Paket main - Tests für die abbrechbare Terminal-Eingabe (Build 48)
//
// Hintergrund (Bug): Nach dem Verlassen des SSH-Terminals blieb die
// stdin-Weiterleitung in einem blockierenden Read hängen. Der nächste
// Tastendruck (z.B. "x" zum Trennen) landete dort und wurde verworfen -
// erst der zweite kam bei Bubbletea an. Die Eingabe ist jetzt abbrechbar:
// Nach Sitzungsende wird die Weiterleitung gestoppt, BEVOR Bubbletea stdin
// wieder übernimmt.
//
// Der Pipe-Test läuft nur unter Unix: Unter Windows ist eine Pipe keine
// Konsole, newTerminalInput lehnt sie dort bewusst ab (GetConsoleMode).
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// lockedBuffer ist ein threadsicherer Puffer für die Weiterleitungs-Ausgabe.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor wartet bis cond() true liefert oder die Frist abläuft.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestForwarderStopsWithoutSwallowingNextKey reproduziert den Bug: Nach
// dem Stoppen darf die Weiterleitung keine weiteren Bytes mehr lesen - der
// nächste Tastendruck muss für den nachfolgenden Leser (Bubbletea) übrig
// bleiben.
func TestForwarderStopsWithoutSwallowingNextKey(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()

	input, err := newTerminalInput(pr)
	if err != nil {
		t.Fatalf("newTerminalInput: %v", err)
	}

	sink := &lockedBuffer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardStdinWithPaste(input, sink, nil, nil)
	}()

	// Während der Sitzung: Eingabe wird weitergeleitet
	if _, err := pw.Write([]byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, time.Second, func() bool { return sink.String() == "ls\r" }) {
		t.Fatalf("Weiterleitung lieferte %q, erwartet %q", sink.String(), "ls\r")
	}

	// Sitzung endet: Weiterleitung stoppen
	if !stopInputForwarder(input, done, time.Second) {
		t.Fatal("Weiterleitung hat sich nicht rechtzeitig beendet")
	}
	_ = input.Close()

	// Nächster Tastendruck nach dem Terminal: muss im stdin verbleiben
	if _, err := pw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = pr.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8)
	n, err := pr.Read(buf)
	if err != nil || string(buf[:n]) != "x" {
		t.Errorf("nachfolgender Leser bekam %q (err=%v), erwartet \"x\" - Taste wurde verschluckt", buf[:n], err)
	}
	if got := sink.String(); got != "ls\r" {
		t.Errorf("Weiterleitung hat nach dem Stopp noch gelesen: %q", got)
	}
}

// blockingInput simuliert eine Eingabe, deren Cancel wirkungslos ist.
type blockingInput struct{ release chan struct{} }

func (b *blockingInput) Read(p []byte) (int, error) { <-b.release; return 0, errors.New("eof") }
func (b *blockingInput) Cancel() bool               { return false }
func (b *blockingInput) Close() error               { return nil }

// TestStopInputForwarderTimeout prüft dass das Stoppen nicht ewig hängt,
// wenn die Eingabe sich nicht abbrechen lässt.
func TestStopInputForwarderTimeout(t *testing.T) {
	in := &blockingInput{release: make(chan struct{})}
	defer close(in.release)
	done := make(chan struct{})
	start := time.Now()
	if stopInputForwarder(in, done, 50*time.Millisecond) {
		t.Error("stopInputForwarder meldet Erfolg, obwohl die Goroutine nie endete")
	}
	if time.Since(start) > time.Second {
		t.Errorf("Stoppen dauerte %v", time.Since(start))
	}
}

// TestStopInputForwarderNil prüft den Aufruf ohne Eingabe (Fallback-Pfad).
func TestStopInputForwarderNil(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if !stopInputForwarder(nil, done, 10*time.Millisecond) {
		t.Error("nil-Eingabe mit beendeter Goroutine muss true liefern")
	}
}

// TestHasCharacterInput prüft die Windows-Logik: Nur gedrückte Tasten mit
// Zeichen machen ReadFile nicht-blockierend; reine Loslass-, Fokus- oder
// Größen-Ereignisse müssen verworfen statt gelesen werden.
func TestHasCharacterInput(t *testing.T) {
	cases := []struct {
		name   string
		events []consoleKeyEvent
		want   bool
	}{
		{"leer", nil, false},
		{"nur losgelassen", []consoleKeyEvent{{isKey: true, keyDown: false, char: 'x'}}, false},
		{"Shift ohne Zeichen", []consoleKeyEvent{{isKey: true, keyDown: true, char: 0}}, false},
		{"Fokus-Ereignis", []consoleKeyEvent{{isKey: false}}, false},
		{"Zeichen", []consoleKeyEvent{{isKey: true, keyDown: true, char: 'x'}}, true},
		{"Zeichen nach Loslassen", []consoleKeyEvent{
			{isKey: true, keyDown: false, char: 'a'},
			{isKey: false},
			{isKey: true, keyDown: true, char: 0x1b},
		}, true},
	}
	for _, c := range cases {
		if got := hasCharacterInput(c.events); got != c.want {
			t.Errorf("%s: hasCharacterInput = %v, erwartet %v", c.name, got, c.want)
		}
	}
}
