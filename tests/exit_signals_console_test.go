// Tests für das Schließ-Ereignis der Windows-Konsole (Build 91)
//
// AllocConsole (icon_windows.go, init) setzt die Handler-Tabelle der Konsole
// zurück; Gos eigener Handler ist danach weg, und das Schließkreuz beendet den
// Prozess per ExitProcess ohne Aufräumen. Der Fix registriert einen eigenen
// Handler (SetConsoleCtrlHandler), der das Ereignis über ein Relay an den
// Watcher weiterreicht. Die Tests prüfen den plattformneutralen Kern
// (Ereignis-Zuordnung, Relay, Callback-Kern, Logzeile) und per Quelltext die
// Verdrahtung in icon_windows.go und exit_signals.go, die unter Linux nicht
// laufen kann.
//
// Das globale consoleCloseRelay wird in keinem Test befüllt.
//
// @author Kurt Ingwer
// @date   2026-11-16 16:45
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// --- Hilfsmittel ---

// stripGoComments entfernt Kommentare aus Go-Quelltext: // -Kommentare (ganze
// Zeilen und der Rest hinter //) sowie /* */ -Blöcke (auch über mehrere Zeilen;
// die Zeilenumbrüche bleiben erhalten). Zeichenketten ("...", `...`) und
// Runen ('/') werden dabei übersprungen, damit ein // oder /* in einem Text
// nichts abschneidet. So treffen Quelltexttests nie auf Wörter in Kommentaren.
func stripGoComments(text string) string {
	var out strings.Builder
	n := len(text)
	for i := 0; i < n; {
		c := text[i]
		switch {
		case c == '/' && i+1 < n && text[i+1] == '/':
			// Zeilenkommentar: bis vor den Zeilenumbruch verwerfen
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			// Blockkommentar: bis hinter */ verwerfen, Umbrüche behalten
			i += 2
			for i < n && !(text[i] == '*' && i+1 < n && text[i+1] == '/') {
				if text[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			i += 2
			out.WriteByte(' ')
		case c == '"' || c == '\'':
			// Zeichenkette bzw. Rune mit Escape-Zeichen unverändert übernehmen
			out.WriteByte(c)
			i++
			for i < n && text[i] != c && text[i] != '\n' {
				if text[i] == '\\' && i+1 < n {
					out.WriteByte(text[i])
					i++
				}
				out.WriteByte(text[i])
				i++
			}
			if i < n {
				out.WriteByte(text[i])
				i++
			}
		case c == '`':
			// Rohtext unverändert übernehmen
			out.WriteByte(c)
			i++
			for i < n && text[i] != '`' {
				out.WriteByte(text[i])
				i++
			}
			if i < n {
				out.WriteByte(text[i])
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// normalizeSpace faltet jeden Leerraum (Leerzeichen, Tabs, Umbrüche) zu einem
// einzelnen Leerzeichen, damit Quelltextvergleiche formatierungsunabhängig sind.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// braceDepthAt liefert die Klammertiefe ({ minus }) im Text vor der Stelle pos,
// Zeichenketten und Runen zählen nicht mit. Der Text muss kommentarfrei sein.
// Tiefe 0 = ganz außen; die Tiefe ist direkt vor pos gemeint.
func braceDepthAt(text string, pos int) int {
	depth, n := 0, len(text)
	for i := 0; i < pos && i < n; i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '"', '\'':
			q := text[i]
			for i++; i < n && text[i] != q && text[i] != '\n'; i++ {
				if text[i] == '\\' {
					i++
				}
			}
		case '`':
			for i++; i < n && text[i] != '`'; i++ {
			}
		}
	}
	return depth
}

// trimmedLines liefert die getrimmten Zeilen des Textes.
func trimmedLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

// funcBodyByLine liefert den Rumpf ab der Zeile mit prefix bis zur nächsten
// Zeile, die genau "}" lautet (Funktionsende auf oberster Ebene). Gefunden =
// false, wenn prefix fehlt oder kein Ende kommt.
func funcBodyByLine(text, prefix string) (string, bool) {
	from := strings.Index(text, prefix)
	if from < 0 {
		return "", false
	}
	rest := text[from:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n}") {
			return rest, true
		}
		return "", false
	}
	return rest[:end+2], true
}

// consoleSleepRecorder zeichnet Aufrufe der Schlaf-Funktion auf.
type consoleSleepRecorder struct {
	mu    sync.Mutex
	calls []time.Duration
}

// sleep ist die Funktion, die handleConsoleCtrlEvent statt time.Sleep erhält.
func (s *consoleSleepRecorder) sleep(d time.Duration) {
	s.mu.Lock()
	s.calls = append(s.calls, d)
	s.mu.Unlock()
}

// list liefert eine Kopie der aufgezeichneten Dauern.
func (s *consoleSleepRecorder) list() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.calls...)
}

// drainRelay liest alle sofort verfügbaren Signale aus dem Relay.
func drainRelay(r *closeEventRelay) []os.Signal {
	var got []os.Signal
	for {
		select {
		case s := <-r.events():
			got = append(got, s)
		default:
			return got
		}
	}
}

// --- 1: Ereignis -> Signal ---

// TestConsoleCtrlEventSignal prüft die Zuordnung der Windows-Konsolen-
// Ereignisse: Schließen (2), Abmelden (5) und Herunterfahren (6) werden zu
// SIGTERM und behandelt. Strg+C (0) und Strg+Untbr (1) bleiben unbehandelt
// (Strg+C gehört dem Programm bzw. der SSH-Sitzung, kein Beenden). Randfälle:
// unbekannte Nummern (3, 4, 7) und 0xFFFFFFFF dürfen nie als Schließen gelten.
func TestConsoleCtrlEventSignal(t *testing.T) {
	cases := []struct {
		ev   uint32
		want bool
	}{
		{0, false}, {1, false}, {2, true}, {3, false}, {4, false},
		{5, true}, {6, true}, {7, false}, {0xFFFFFFFF, false},
	}
	for _, c := range cases {
		sig, ok := consoleCtrlEventSignal(c.ev)
		if ok != c.want {
			t.Errorf("Ereignis %d: behandelt = %v, erwartet %v", c.ev, ok, c.want)
		}
		if c.want && sig != syscall.SIGTERM {
			t.Errorf("Ereignis %d: Signal = %v, erwartet SIGTERM", c.ev, sig)
		}
		if !c.want && sig != nil {
			t.Errorf("Ereignis %d: Signal = %v, erwartet nil", c.ev, sig)
		}
	}
}

// --- 2/3: Relay ---

// TestCloseEventRelayForwardNonBlocking prüft: Der Windows-Callback darf nie
// hängen. Drei forward-Aufrufe hintereinander kehren sofort zurück (Puffer 1:
// der zweite und dritte treffen auf einen vollen Puffer und werden verworfen),
// liefern jeweils true, und im Kanal liegt danach genau EIN Signal.
func TestCloseEventRelayForwardNonBlocking(t *testing.T) {
	r := newCloseEventRelay()
	results := make(chan bool, 3)
	go func() {
		for i := 0; i < 3; i++ {
			results <- r.forward(syscall.SIGTERM)
		}
	}()
	for i := 0; i < 3; i++ {
		select {
		case ok := <-results:
			if !ok {
				t.Errorf("forward Nr. %d lieferte false, erwartet true", i+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("forward Nr. %d blockiert (mehr als 1 s)", i+1)
		}
	}
	if got := drainRelay(r); len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Errorf("im Kanal: %v, erwartet genau ein SIGTERM", got)
	}
}

// TestCloseEventRelayNil prüft den Randfall nil-Relay: forward liefert false
// ohne Panik und ohne Blockieren, events() liefert nil.
func TestCloseEventRelayNil(t *testing.T) {
	var r *closeEventRelay
	done := make(chan bool, 1)
	go func() { done <- r.forward(syscall.SIGTERM) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("forward auf nil-Relay lieferte true, erwartet false")
		}
	case <-time.After(time.Second):
		t.Fatal("forward auf nil-Relay blockiert")
	}
	if r.events() != nil {
		t.Error("events() auf nil-Relay ist nicht nil")
	}
}

// --- 4: Relay und Watcher ---

// consoleWatcher startet watchCloseSignalsFrom mit Aufzeichnung und kurzer Frist.
type consoleWatcher struct {
	allDone   chan struct{}
	onSignal  chan struct{}
	quit      chan struct{}
	exitCodes chan int
	returned  chan struct{}
}

// startConsoleWatcher startet den Watcher in einer Goroutine. allDone wird nie
// geschlossen (außer der Test tut es), also läuft nach der Frist der Notfall-
// Ablauf und ruft exit(0).
func startConsoleWatcher(sigs, extra <-chan os.Signal) *consoleWatcher {
	w := &consoleWatcher{
		allDone:   make(chan struct{}),
		onSignal:  make(chan struct{}, 4),
		quit:      make(chan struct{}, 4),
		exitCodes: make(chan int, 4),
		returned:  make(chan struct{}),
	}
	go func() {
		defer close(w.returned)
		watchCloseSignalsFrom(sigs, extra,
			func() { w.onSignal <- struct{}{} },
			func() { w.quit <- struct{}{} },
			w.allDone, nil, 20*time.Millisecond,
			func(code int) { w.exitCodes <- code }, nil)
	}()
	return w
}

// expectCloseHandled prüft, dass onSignal, quit und exit(0) je einmal kamen.
func (w *consoleWatcher) expectCloseHandled(t *testing.T) {
	t.Helper()
	select {
	case <-w.onSignal:
	case <-time.After(3 * time.Second):
		t.Fatal("onSignal wurde nicht aufgerufen")
	}
	select {
	case <-w.quit:
	case <-time.After(3 * time.Second):
		t.Fatal("quit wurde nicht aufgerufen")
	}
	select {
	case code := <-w.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code %d, erwartet 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exit(0) kam nicht (Notfall-Ablauf blieb aus)")
	}
	select {
	case <-w.returned:
	case <-time.After(3 * time.Second):
		t.Error("Watcher kehrte nicht zurück")
	}
}

// TestCloseEventRelayEarlyEventDelivered prüft den Randfall "Ereignis kommt vor
// dem Watcher": Das Schließen wird schon weitergeleitet, bevor der Watcher
// läuft (Windows ruft den Handler früh, z. B. während des Starts). Der Puffer
// hält es; der Watcher sieht es danach trotzdem und läuft komplett durch
// (onSignal, quit, Notfall-Ablauf, exit(0)). Gegenstück: Ein Signal über sigs
// funktioniert weiter, wenn extra nil ist (Linux: SIGHUP).
func TestCloseEventRelayEarlyEventDelivered(t *testing.T) {
	relay := newCloseEventRelay()
	if !relay.forward(syscall.SIGTERM) {
		t.Fatal("forward lieferte false")
	}
	w := startConsoleWatcher(make(chan os.Signal), relay.events())
	w.expectCloseHandled(t)

	// Gegenstück: extra == nil, Signal über sigs
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGHUP
	w2 := startConsoleWatcher(sigs, nil)
	w2.expectCloseHandled(t)
}

// TestWatchCloseSignalsFromNoSignalNormalEnd prüft: Ohne Signal (beide Kanäle
// nil, blockieren einfach) endet der Watcher still, sobald allDone schließt -
// ohne onSignal, quit oder exit (normales Programmende mit q).
func TestWatchCloseSignalsFromNoSignalNormalEnd(t *testing.T) {
	w := startConsoleWatcher(nil, nil)
	close(w.allDone)
	select {
	case <-w.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("Watcher kehrte trotz allDone nicht zurück")
	}
	if len(w.onSignal) != 0 || len(w.quit) != 0 || len(w.exitCodes) != 0 {
		t.Errorf("onSignal/quit/exit trotz normalem Ende: %d/%d/%d",
			len(w.onSignal), len(w.quit), len(w.exitCodes))
	}
}

// --- 5: Kern des Callbacks ---

// TestHandleConsoleCtrlEvent prüft den Kern des Windows-Callbacks:
//   - Schließen/Abmelden/Herunterfahren: Rückgabe 1 ("behandelt"), das Signal
//     liegt im Relay, und sleep läuft genau einmal mit consoleCtrlHoldTimeout
//     (der Callback hält Windows so lange hin, bis der Watcher aufgeräumt hat);
//   - Strg+C, Strg+Untbr und Unbekanntes: 0, kein sleep, Relay leer;
//   - nil-Relay: 0 und kein sleep (nichts zum Warten);
//   - sleep == nil: 1 ohne Panik;
//   - volles Relay (Ereignis wartet schon): trotzdem 1, nicht blockierend,
//     weiterhin nur ein Signal im Kanal.
func TestHandleConsoleCtrlEvent(t *testing.T) {
	for _, ev := range []uint32{consoleCtrlCloseEvent, consoleCtrlLogoffEvent, consoleCtrlShutdownEvent} {
		relay := newCloseEventRelay()
		rec := &consoleSleepRecorder{}
		if got := handleConsoleCtrlEvent(ev, relay, consoleCtrlHoldTimeout, rec.sleep); got != 1 {
			t.Errorf("Ereignis %d: Rückgabe %d, erwartet 1", ev, got)
		}
		if calls := rec.list(); len(calls) != 1 || calls[0] != consoleCtrlHoldTimeout {
			t.Errorf("Ereignis %d: sleep-Aufrufe %v, erwartet einmal %v", ev, calls, consoleCtrlHoldTimeout)
		}
		if got := drainRelay(relay); len(got) != 1 || got[0] != syscall.SIGTERM {
			t.Errorf("Ereignis %d: Relay enthält %v, erwartet ein SIGTERM", ev, got)
		}
	}

	for _, ev := range []uint32{consoleCtrlCEvent, consoleCtrlBreakEvent, 3, 4, 7, 0xFFFFFFFF} {
		relay := newCloseEventRelay()
		rec := &consoleSleepRecorder{}
		if got := handleConsoleCtrlEvent(ev, relay, consoleCtrlHoldTimeout, rec.sleep); got != 0 {
			t.Errorf("unbehandeltes Ereignis %d: Rückgabe %d, erwartet 0", ev, got)
		}
		if calls := rec.list(); len(calls) != 0 {
			t.Errorf("unbehandeltes Ereignis %d: sleep gerufen: %v", ev, calls)
		}
		if got := drainRelay(relay); len(got) != 0 {
			t.Errorf("unbehandeltes Ereignis %d: Relay enthält %v", ev, got)
		}
	}

	// nil-Relay bei Close: 0 und kein Warten
	rec := &consoleSleepRecorder{}
	if got := handleConsoleCtrlEvent(consoleCtrlCloseEvent, nil, consoleCtrlHoldTimeout, rec.sleep); got != 0 {
		t.Errorf("nil-Relay: Rückgabe %d, erwartet 0", got)
	}
	if calls := rec.list(); len(calls) != 0 {
		t.Errorf("nil-Relay: sleep gerufen: %v", calls)
	}

	// sleep == nil: kein Warten, keine Panik
	relay := newCloseEventRelay()
	if got := handleConsoleCtrlEvent(consoleCtrlCloseEvent, relay, consoleCtrlHoldTimeout, nil); got != 1 {
		t.Errorf("sleep nil: Rückgabe %d, erwartet 1", got)
	}

	// volles Relay: nicht blockierend, trotzdem 1
	full := newCloseEventRelay()
	full.forward(syscall.SIGTERM)
	rec2 := &consoleSleepRecorder{}
	done := make(chan uintptr, 1)
	go func() {
		done <- handleConsoleCtrlEvent(consoleCtrlCloseEvent, full, consoleCtrlHoldTimeout, rec2.sleep)
	}()
	select {
	case got := <-done:
		if got != 1 {
			t.Errorf("volles Relay: Rückgabe %d, erwartet 1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("volles Relay: Callback blockiert")
	}
	if got := drainRelay(full); len(got) != 1 {
		t.Errorf("volles Relay: %d Signale im Kanal, erwartet 1", len(got))
	}
}

// TestConsoleCtrlHoldTimeoutValue prüft die Haltezeit des Callbacks: Kehrt der
// Callback zurück, beendet Windows den Prozess sofort. Die Zeit muss daher
// länger sein als Frist plus Notfall-Budget des Watchers, sonst käme der
// Watcher nicht zum Aufräumen. Eine Obergrenze (unter einer Minute) verhindert
// einen versehentlich riesigen Wert, der Abmelden/Herunterfahren aufhielte.
func TestConsoleCtrlHoldTimeoutValue(t *testing.T) {
	lower := closeGracePeriod + emergencyStepsTimeout
	if consoleCtrlHoldTimeout <= lower {
		t.Errorf("consoleCtrlHoldTimeout %v <= %v (Frist + Notfall-Budget)", consoleCtrlHoldTimeout, lower)
	}
	if consoleCtrlHoldTimeout >= time.Minute {
		t.Errorf("consoleCtrlHoldTimeout %v unvernünftig lang", consoleCtrlHoldTimeout)
	}
}

// --- 6: Logzeile zur Registrierung ---

// readLogOrEmpty liest die Logdatei; fehlt sie, ist das Ergebnis leer.
func readLogOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// TestLogConsoleCtrlRegistration prüft die Logzeile zur Registrierung des
// Konsolen-Handlers: nicht versucht (Linux) -> keine Zeile; Erfolg -> INFO
// "Konsolen-Handler registriert"; Fehler -> WARN "Konsolen-Handler nicht
// registriert" samt Fehlertext (sonst bliebe der Grund für ein
// aufräumloses Schließen unsichtbar). Randfall log == nil: keine Panik.
func TestLogConsoleCtrlRegistration(t *testing.T) {
	newLog := func() (*Logger, string) {
		path := filepath.Join(t.TempDir(), "ssh-easy.log")
		return &Logger{filePath: path, enabled: true}, path
	}

	log, path := newLog()
	logConsoleCtrlRegistration(log, consoleCtrlRegistration{attempted: false})
	if text := readLogOrEmpty(t, path); strings.Contains(text, "Konsolen-Handler") {
		t.Errorf("nicht versucht, aber Logzeile vorhanden:\n%s", text)
	}

	// attempted=false darf auch mit gesetztem Fehler nichts schreiben
	log, path = newLog()
	logConsoleCtrlRegistration(log, consoleCtrlRegistration{attempted: false, err: errors.New("x")})
	if text := readLogOrEmpty(t, path); strings.Contains(text, "Konsolen-Handler") {
		t.Errorf("nicht versucht (mit Fehler), aber Logzeile vorhanden:\n%s", text)
	}

	log, path = newLog()
	logConsoleCtrlRegistration(log, consoleCtrlRegistration{attempted: true})
	text := readLogOrEmpty(t, path)
	if !strings.Contains(text, "Konsolen-Handler registriert") || !strings.Contains(text, "[INFO]") {
		t.Errorf("Erfolg: erwartet INFO-Zeile \"Konsolen-Handler registriert\", Log:\n%s", text)
	}
	if strings.Contains(text, "nicht registriert") {
		t.Errorf("Erfolg: Fehlerzeile im Log:\n%s", text)
	}

	log, path = newLog()
	logConsoleCtrlRegistration(log, consoleCtrlRegistration{attempted: true, err: errors.New("Zugriff verweigert 5")})
	text = readLogOrEmpty(t, path)
	if !strings.Contains(text, "Konsolen-Handler nicht registriert") ||
		!strings.Contains(text, "Zugriff verweigert 5") || !strings.Contains(text, "[WARN]") {
		t.Errorf("Fehler: erwartet WARN-Zeile mit Text und Fehler, Log:\n%s", text)
	}

	// log == nil: keine Panik, in allen drei Zuständen
	logConsoleCtrlRegistration(nil, consoleCtrlRegistration{})
	logConsoleCtrlRegistration(nil, consoleCtrlRegistration{attempted: true})
	logConsoleCtrlRegistration(nil, consoleCtrlRegistration{attempted: true, err: errors.New("x")})
}

// --- 7/8: Quelltexttests der Verdrahtung ---

// checkConsoleCtrlWiring prüft den (kommentarbereinigten) Text von
// icon_windows.go auf die Verdrahtung des Konsolen-Handlers und liefert die
// gefundenen Mängel (leer = in Ordnung):
//   - init(): allocOwnConsole() steht als eigene Anweisung (getrimmte Zeile
//     genau "allocOwnConsole()"), NICHT in einer if-Bedingung - AllocConsole
//     muss immer laufen und der Handler unabhängig von seinem Ergebnis
//     angemeldet werden (auch bei Teilfehlern hat AllocConsole die Tabelle
//     schon geleert);
//   - registerConsoleCtrlHandler() steht NACH allocOwnConsole() (AllocConsole
//     setzt die Handler-Tabelle zurück), auf derselben Blocktiefe (also nicht
//     in einem if, das die Anmeldung vom Ergebnis abhängig machte) und das
//     Ergebnis landet in consoleCtrlRegistrationState;
//   - consoleCtrlCallback ruft genau handleConsoleCtrlEvent(uint32(ev),
//     consoleCloseRelay, consoleCtrlHoldTimeout, time.Sleep) auf (nil statt
//     Relay, Haltezeit 0 oder nil statt Sleep hielten das Ereignis auf dem
//     Weg zum Watcher oder Windows nicht lange genug hin);
//   - registerConsoleCtrlHandler erzeugt den Callback per
//     syscall.NewCallback(consoleCtrlCallback) und meldet ihn mit
//     SetConsoleCtrlHandler(callback, 1) an (Add = TRUE).
func checkConsoleCtrlWiring(text string) []string {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	body, ok := funcBodyByLine(text, "func init() {")
	if !ok {
		return append(problems, "func init() { in icon_windows.go nicht gefunden")
	}
	// Eigene Anweisung: Zeile genau "allocOwnConsole()", Versatz im Rumpf merken
	allocPos, offset := -1, 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "allocOwnConsole()" && allocPos < 0 {
			allocPos = offset + strings.Index(line, "allocOwnConsole()")
		}
		offset += len(line) + 1
	}
	regPos := strings.Index(body, "registerConsoleCtrlHandler()")
	if allocPos < 0 {
		bad("init() ruft allocOwnConsole() nicht als eigene Anweisung auf (z. B. in einer if-Bedingung)")
	}
	if regPos < 0 {
		bad("init() ruft registerConsoleCtrlHandler() nicht auf")
	}
	if allocPos >= 0 && regPos >= 0 {
		if regPos < allocPos {
			bad("registerConsoleCtrlHandler() steht VOR allocOwnConsole() - AllocConsole würde den Handler wieder entfernen")
		}
		// Tiefe am Anfang der Zeile: Die Klammer des Struct-Literals in der
		// Zuweisung (consoleCtrlRegistration{...}) zählt nicht als Block.
		lineStart := func(pos int) int { return strings.LastIndex(body[:pos], "\n") + 1 }
		if da, dr := braceDepthAt(body, lineStart(allocPos)), braceDepthAt(body, lineStart(regPos)); da != dr {
			bad("registerConsoleCtrlHandler() steht auf Blocktiefe %d, allocOwnConsole() auf %d - die Anmeldung hängt von einem Block ab", dr, da)
		} else if da != 1 {
			// Rumpf von init() = Tiefe 1; tiefer hieße: beide Aufrufe laufen nur bedingt
			bad("allocOwnConsole() und registerConsoleCtrlHandler() stehen auf Blocktiefe %d, erwartet 1 (unbedingt in init())", da)
		}
	}
	if !regexp.MustCompile(`consoleCtrlRegistrationState\s*=`).MatchString(body) {
		bad("init() weist consoleCtrlRegistrationState nicht zu")
	}

	cb, ok := funcBodyByLine(text, "func consoleCtrlCallback(")
	if !ok {
		bad("func consoleCtrlCallback( nicht gefunden")
	} else if want := "handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, consoleCtrlHoldTimeout, time.Sleep)"; !strings.Contains(normalizeSpace(cb), want) {
		bad("consoleCtrlCallback ruft nicht %s auf", want)
	}

	fn, ok := funcBodyByLine(text, "func registerConsoleCtrlHandler() error")
	if !ok {
		return append(problems, "func registerConsoleCtrlHandler() error in icon_windows.go nicht gefunden")
	}
	norm := normalizeSpace(fn)
	if !strings.Contains(norm, "syscall.NewCallback(consoleCtrlCallback)") {
		bad("registerConsoleCtrlHandler erzeugt den Callback nicht per syscall.NewCallback(consoleCtrlCallback)")
	}
	if !strings.Contains(norm, "procSetConsoleCtrlHandler.Call(callback, 1)") {
		bad("registerConsoleCtrlHandler ruft procSetConsoleCtrlHandler.Call(callback, 1) nicht auf")
	}
	return problems
}

// TestIconInitRegistersConsoleCtrlHandler prüft den Quelltext von
// icon_windows.go (läuft auch unter Linux) mit checkConsoleCtrlWiring:
// Reihenfolge und Blocktiefe in init(), der genaue Aufruf im Callback und die
// Anmeldung per NewCallback. Kommentare (// und /* */) zählen nicht.
func TestIconInitRegistersConsoleCtrlHandler(t *testing.T) {
	data, err := os.ReadFile("icon_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range checkConsoleCtrlWiring(stripGoComments(string(data))) {
		t.Error(problem)
	}
}

// consoleWiringSample ist ein gültiges Muster von icon_windows.go (nur die
// geprüften Teile). Die Mutanten-Tests leiten daraus je einen Fehler ab.
const consoleWiringSample = `package main

func consoleCtrlCallback(ev uintptr) uintptr {
	return handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, consoleCtrlHoldTimeout, time.Sleep)
}

func registerConsoleCtrlHandler() error {
	if err := procSetConsoleCtrlHandler.Find(); err != nil {
		return err
	}
	callback := syscall.NewCallback(consoleCtrlCallback)
	ret, _, callErr := procSetConsoleCtrlHandler.Call(callback, 1)
	if ret == 0 {
		return callErr
	}
	return nil
}

func init() {
	if isInstallHelperInvocation(os.Args) {
		return
	}
	allocOwnConsole()
	consoleCtrlRegistrationState = consoleCtrlRegistration{attempted: true, err: registerConsoleCtrlHandler()}
	fixLipglossColorProfile()
}
`

// TestConsoleCtrlWiringCheckAcceptsSample prüft die Prüffunktion selbst: Das
// gültige Muster ergibt keinen Mangel, auch nach Umformatierung des Aufrufs
// (Umbrüche, doppelte Leerzeichen) und mit Kommentaren dazwischen. Sonst wäre
// jede Rotmeldung der Mutanten-Tests wertlos.
func TestConsoleCtrlWiringCheckAcceptsSample(t *testing.T) {
	if problems := checkConsoleCtrlWiring(stripGoComments(consoleWiringSample)); len(problems) != 0 {
		t.Errorf("gültiges Muster abgelehnt: %v", problems)
	}
	reformatted := strings.Replace(consoleWiringSample,
		"handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, consoleCtrlHoldTimeout, time.Sleep)",
		"handleConsoleCtrlEvent(uint32(ev),\n\t\tconsoleCloseRelay,   consoleCtrlHoldTimeout,\n\t\ttime.Sleep)", 1)
	reformatted = strings.Replace(reformatted, "\tallocOwnConsole()\n",
		"\t// ruft allocOwnConsole() auf\n\t/* if allocOwnConsole() { */\n\tallocOwnConsole() // Konsole\n", 1)
	if problems := checkConsoleCtrlWiring(stripGoComments(reformatted)); len(problems) != 0 {
		t.Errorf("umformatiertes Muster abgelehnt: %v", problems)
	}
}

// TestConsoleCtrlWiringCheckRejectsMutants ist die dauerhafte Gegenprobe der
// Quelltextprüfung: Jeder Mutant des Musters (falsche Argumente im Callback,
// Anmeldung in if-Bedingung oder Block, falsche Reihenfolge, fehlender oder nur
// im Kommentar stehender Aufruf) muss mindestens einen Mangel ergeben.
func TestConsoleCtrlWiringCheckRejectsMutants(t *testing.T) {
	const call = "handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, consoleCtrlHoldTimeout, time.Sleep)"
	const alloc = "\tallocOwnConsole()\n"
	const reg = "\tconsoleCtrlRegistrationState = consoleCtrlRegistration{attempted: true, err: registerConsoleCtrlHandler()}\n"
	mutants := map[string]string{
		"M1 nil statt Relay":              strings.Replace(consoleWiringSample, call, "handleConsoleCtrlEvent(uint32(ev), nil, consoleCtrlHoldTimeout, time.Sleep)", 1),
		"M1 Haltezeit 0":                  strings.Replace(consoleWiringSample, call, "handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, 0, time.Sleep)", 1),
		"M1 nil statt Sleep":              strings.Replace(consoleWiringSample, call, "handleConsoleCtrlEvent(uint32(ev), consoleCloseRelay, consoleCtrlHoldTimeout, nil)", 1),
		"M1 Aufruf nur im Kommentar":      strings.Replace(consoleWiringSample, "\treturn "+call, "\t// "+call+"\n\treturn 0", 1),
		"M1 Aufruf nur im Blockkommentar": strings.Replace(consoleWiringSample, "\treturn "+call, "\t/* "+call+" */\n\treturn 0", 1),
		"M2 Anmeldung in if allocOwnConsole()": strings.Replace(consoleWiringSample, alloc+reg,
			"\tif allocOwnConsole() {\n\t"+reg+"\t}\n", 1),
		"M2 beide in einem Block": strings.Replace(consoleWiringSample, alloc+reg,
			"\tif true {\n\t"+alloc+"\t"+reg+"\t}\n", 1),
		"M2 Anmeldung allein im Block": strings.Replace(consoleWiringSample, reg,
			"\tif true {\n\t"+reg+"\t}\n", 1),
		"Anmeldung vor AllocConsole":       strings.Replace(consoleWiringSample, alloc+reg, reg+alloc, 1),
		"allocOwnConsole nur im Kommentar": strings.Replace(consoleWiringSample, alloc, "\t// allocOwnConsole()\n", 1),
		"allocOwnConsole fehlt":            strings.Replace(consoleWiringSample, alloc, "", 1),
		"Anmeldung fehlt":                  strings.Replace(consoleWiringSample, "err: registerConsoleCtrlHandler()", "err: nil", 1),
		"Anmeldung nur im Kommentar":       strings.Replace(consoleWiringSample, reg, "\t// registerConsoleCtrlHandler()\n\tconsoleCtrlRegistrationState = consoleCtrlRegistration{}\n", 1),
		"Ergebnis nicht gemerkt":           strings.Replace(consoleWiringSample, "consoleCtrlRegistrationState = ", "_ = ", 1),
		"NewCallback mit anderem Ziel":     strings.Replace(consoleWiringSample, "syscall.NewCallback(consoleCtrlCallback)", "syscall.NewCallback(otherCallback)", 1),
		"Handler mit Add = 0":              strings.Replace(consoleWiringSample, "Call(callback, 1)", "Call(callback, 0)", 1),
	}
	for name, text := range mutants {
		if text == consoleWiringSample {
			t.Errorf("%s: Mutation hat das Muster nicht verändert (Testfehler)", name)
			continue
		}
		if problems := checkConsoleCtrlWiring(stripGoComments(text)); len(problems) == 0 {
			t.Errorf("%s: Mutant wurde nicht erkannt", name)
		}
	}
}

// TestStripGoComments prüft den Kommentarentferner: Zeilen- und Blockkommentare
// (auch mehrzeilig) verschwinden samt Inhalt, die Zeilenzahl bleibt; ein // oder
// /* in einer Zeichenkette, einem Rohtext oder einer Rune bleibt unangetastet
// (Randfall: "http://x" darf nicht abgeschnitten werden); Escape-Anführungszeichen
// beenden die Zeichenkette nicht.
func TestStripGoComments(t *testing.T) {
	in := "a() // x()\n// y()\n/* z()\n w() */ b()\nc(\"http://x\", `/* r */`, '/')\nd(\"q\\\" // keep\")\n"
	got := stripGoComments(in)
	for _, gone := range []string{"x()", "y()", "z()", "w()"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s steht noch im bereinigten Text:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"a()", "b()", "c(", "\"http://x\"", "`/* r */`", "'/'", "// keep"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s fehlt im bereinigten Text:\n%s", kept, got)
		}
	}
	if strings.Count(got, "\n") != strings.Count(in, "\n") {
		t.Errorf("Zeilenzahl geändert: %d statt %d", strings.Count(got, "\n"), strings.Count(in, "\n"))
	}
}

// TestWatchCloseSignalsUsesConsoleRelay prüft den Quelltext von
// exit_signals.go: watchCloseSignals (die Hülle mit unveränderter Signatur)
// ruft logConsoleCtrlRegistration( und gibt dem eigentlichen Watcher
// watchCloseSignalsFrom( das Relay mit (consoleCloseRelay.events()). Wird dort
// nil statt des Relays übergeben, käme das Schließkreuz-Ereignis nie an.
func TestWatchCloseSignalsUsesConsoleRelay(t *testing.T) {
	data, err := os.ReadFile("exit_signals.go")
	if err != nil {
		t.Fatal(err)
	}
	text := stripGoComments(string(data))
	body, ok := funcBodyByLine(text, "func watchCloseSignals(")
	if !ok {
		t.Fatal("func watchCloseSignals( in exit_signals.go nicht gefunden")
	}
	if !strings.Contains(body, "logConsoleCtrlRegistration(") {
		t.Error("watchCloseSignals ruft logConsoleCtrlRegistration( nicht auf")
	}
	from := strings.Index(body, "watchCloseSignalsFrom(")
	if from < 0 {
		t.Fatal("watchCloseSignals ruft watchCloseSignalsFrom( nicht auf")
	}
	if !strings.Contains(body[from:], "consoleCloseRelay.events()") {
		t.Error("watchCloseSignalsFrom( bekommt consoleCloseRelay.events() nicht")
	}
}
