// Paket main - Tests für das Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87, ergänzt in Build 88 und 89)
//
// Hintergrund: Wer das Fenster schließt, bekommt unter Linux SIGHUP und unter
// Windows CTRL_CLOSE_EVENT (in Go: SIGTERM). Früher lief dann der Code nach
// p.Run() nie, und ein heruntergeladenes Update wurde nicht installiert.
// Spezifikation: Scratchpad spec_exit.md (Abschnitte A bis D).
//
// STAND BUILD 89 (src/exit_signals.go, die Specialists übernehmen es):
//
//	func closeSignals() []os.Signal
//	    Unix: {SIGHUP, SIGTERM}; Windows: {SIGTERM}; nie SIGINT/Interrupt/Kill.
//
//	type exitTasks struct {
//	    disconnect, apply, cleanup, persist func()   // nil = Schritt überspringen
//	    // dazu interne sync.Once-Felder; Nullwert muss benutzbar sein, die Tests
//	    // bauen &exitTasks{disconnect: ..., apply: ..., cleanup: ..., persist: ...}
//	}
//	func (t *exitTasks) runNormal()
//	    Reihenfolge: disconnect, cleanup, persist, apply (wie bisher in main.go).
//	func (t *exitTasks) runEmergency(deadline time.Duration, allDone <-chan struct{})
//	    Reihenfolge (Build 89):
//	      1. runEmergencyRestore()               Terminal zurücksetzen
//	      2. apply                               synchron
//	      3. cleanup, persist, disconnect        in EINER Goroutine, disconnect ZULETZT
//	         (ein hängendes disconnect hält cleanup und persist nicht mehr auf)
//	      4. im Rest der deadline auf allDone warten (Bubbletea kehrt nach dem
//	         disconnect aus tea.Exec zurück und stellt das Terminal selbst her);
//	         allDone == nil: nicht warten
//	      5. kam allDone NICHT rechtzeitig, die zu Beginn angemeldeten
//	         Wiederherstellungen (Momentaufnahme vom Anfang) ein ZWEITES Mal
//	         unmittelbar vor der Rückkehr; kam allDone rechtzeitig, kein zweites Mal
//	    Schritte 3 und 4 teilen sich EINE deadline. Jeder Schritt läuft über alle
//	    Aufrufe beider Methoden genau einmal (je Schritt ein sync.Once; die zweite
//	    Methode wartet, bis der Schritt fertig ist, oder überspringt ihn).
//
//	const closeGracePeriod = 1500 * time.Millisecond        // Wartezeit auf normales Ende
//	const emergencyStepsTimeout = 750 * time.Millisecond   // gemeinsames Zeitlimit der Notfall-Schritte
//	    Beide zusammen müssen unter 3 s liegen (Windows beendet nach ca. 5 s).
//
//	func watchCloseSignals(sigs <-chan os.Signal, onSignal func(), quit func(),
//	    allDone <-chan struct{}, tasks *exitTasks, grace time.Duration,
//	    exit func(int), log *Logger)
//	    BLOCKIEREND (main startet sie mit "go"). Kehrt zurück, wenn
//	    (a) allDone geschlossen wird, bevor ein Signal kam, oder
//	    (b) das erste Signal behandelt ist: ALS ALLERERSTES onSignal (SYNCHRON, noch
//	        VOR dem ersten log.Info; nil erlaubt), dann "go quit()", dann auf allDone
//	        bis grace warten; kommt allDone rechtzeitig -> zurück ohne Notfall und ohne
//	        exit; sonst tasks.runEmergency(emergencyStepsTimeout, allDone), exit(0).
//	    Weitere Signale lösen keine zweite Behandlung aus. quit darf beliebig lange
//	    blockieren (Senden während tea.Exec), ohne den Watcher zu blockieren.
//	    log darf nil sein.
//
//	func exitCodeFor(err error, closeRequested bool) int
//	    (nil,*)->0, (err,true)->0, (err,false)->1.
//
//	func finishRun(runErr error, closeRequested bool, tasks *exitTasks,
//	    allDone chan struct{}, log *Logger) int
//	    Der Abschnitt nach p.Run() in main: runErr nur protokollieren (log.Error,
//	    Text des Fehlers enthalten), tasks.runNormal(), DANACH close(allDone),
//	    Rückgabe exitCodeFor(runErr, closeRequested). Genau einmal aufrufbar.
//
//	func registerEmergencyRestore(fn func()) (unregister func())
//	func runEmergencyRestore()
//	    Registrierte Funktionen (Terminal wiederherstellen) werden von
//	    runEmergencyRestore aufgerufen; Panik in einer stoppt die anderen nicht;
//	    nebenläufigkeitssicher; unregister darf mehrfach gerufen werden.
//
// VON MIR FESTGELEGT (Spezifikation ließ es offen):
//   - Die Tests der Registry (TestEmergencyRestore*) rufen runEmergencyRestore je
//     Fall direkt; wie oft eine Funktion in runEmergency läuft (Build 89: ein- oder
//     zweimal), prüfen die Tests zu runEmergency.
//   - Die Quelltext-Tests lesen main.go und ssh_terminal.go (Tests laufen als
//     Kopie in src/).
//   - "Rückkehr nach ca. deadline": Untergrenze deadline minus 30 ms, Obergrenze
//     deadline plus Toleranz (siehe emergencyTolerance).
//
// @author Kurt Ingwer
// @date   2026-11-07 10:00
package main

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// --- Hilfsmittel ---

// exitRecorder zeichnet auf, welche Schritte in welcher Reihenfolge liefen.
type exitRecorder struct {
	mu    sync.Mutex
	steps []string
}

// step liefert einen Schritt, der seinen Namen aufzeichnet.
func (r *exitRecorder) step(name string) func() {
	return func() {
		r.mu.Lock()
		r.steps = append(r.steps, name)
		r.mu.Unlock()
	}
}

// list liefert eine Kopie der aufgezeichneten Schritte.
func (r *exitRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

// count zählt, wie oft ein Schritt lief.
func (r *exitRecorder) count(name string) int {
	n := 0
	for _, s := range r.list() {
		if s == name {
			n++
		}
	}
	return n
}

// tasks baut exitTasks, deren vier Schritte aufzeichnen.
func (r *exitRecorder) tasks() *exitTasks {
	return &exitTasks{
		disconnect: r.step("disconnect"),
		apply:      r.step("apply"),
		cleanup:    r.step("cleanup"),
		persist:    r.step("persist"),
	}
}

// waitChan wartet höchstens d auf ein geschlossenes Signal-Chan.
func waitChan(ch <-chan struct{}, d time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

// watchHarness startet watchCloseSignals mit Fake-Kanal und Fake-exit.
type watchHarness struct {
	sigs      chan os.Signal
	allDone   chan struct{}
	rec       *exitRecorder
	tasks     *exitTasks
	quitCalls atomic.Int32
	exitCodes chan int
	returned  chan struct{}
	started   time.Time
}

// startWatcher startet den Watcher. quitBody (darf nil sein) läuft nach dem
// Zählen im quit-Aufruf; tasksOverride (darf nil sein) ersetzt die Aufgaben.
func startWatcher(grace time.Duration, quitBody func(h *watchHarness), tasksOverride func(h *watchHarness) *exitTasks) *watchHarness {
	return startWatcherWithSignal(grace, nil, quitBody, tasksOverride)
}

// startWatcherWithSignal wie startWatcher, aber mit onSignal (darf nil sein),
// das der Watcher beim ersten Signal synchron vor quit aufruft (Build 88, N4).
func startWatcherWithSignal(grace time.Duration, onSignal func(h *watchHarness), quitBody func(h *watchHarness), tasksOverride func(h *watchHarness) *exitTasks) *watchHarness {
	h := &watchHarness{
		sigs:      make(chan os.Signal, 4),
		allDone:   make(chan struct{}),
		rec:       &exitRecorder{},
		exitCodes: make(chan int, 8),
		returned:  make(chan struct{}),
	}
	h.tasks = h.rec.tasks()
	if tasksOverride != nil {
		h.tasks = tasksOverride(h)
	}
	quit := func() {
		h.quitCalls.Add(1)
		if quitBody != nil {
			quitBody(h)
		}
	}
	exit := func(code int) { h.exitCodes <- code }
	var signalHook func()
	if onSignal != nil {
		signalHook = func() { onSignal(h) }
	}
	h.started = time.Now()
	go func() {
		defer close(h.returned)
		watchCloseSignals(h.sigs, signalHook, quit, h.allDone, h.tasks, grace, exit, &Logger{})
	}()
	return h
}

// --- Fall 1: closeSignals ---

// TestCloseSignalsContent prüft die abgefangenen Signale: Unix SIGHUP+SIGTERM,
// Windows nur SIGTERM (CTRL_CLOSE_EVENT). Kein SIGINT: Strg+C soll weiter
// wie bisher wirken; SIGKILL lässt sich ohnehin nicht abfangen.
func TestCloseSignalsContent(t *testing.T) {
	sigs := closeSignals()
	has := func(s os.Signal) bool {
		for _, x := range sigs {
			if x == s {
				return true
			}
		}
		return false
	}
	if !has(syscall.SIGTERM) {
		t.Error("SIGTERM fehlt (unter Windows ist das CTRL_CLOSE_EVENT)")
	}
	if runtime.GOOS == "windows" {
		if len(sigs) != 1 {
			t.Errorf("Windows: %v, erwartet genau {SIGTERM}", sigs)
		}
	} else {
		if !has(syscall.SIGHUP) {
			t.Error("SIGHUP fehlt: Fenster schließen beendet sonst den Prozess ohne Update")
		}
		if len(sigs) != 2 {
			t.Errorf("Unix: %v, erwartet genau {SIGHUP, SIGTERM}", sigs)
		}
	}
	for _, bad := range []os.Signal{os.Interrupt, syscall.SIGINT, os.Kill} {
		if has(bad) {
			t.Errorf("%v darf nicht abgefangen werden", bad)
		}
	}
	seen := map[os.Signal]bool{}
	for _, s := range sigs {
		if seen[s] {
			t.Errorf("Signal %v doppelt", s)
		}
		seen[s] = true
	}
}

// TestCloseSignalsReceivesRealHangup sendet ein echtes SIGHUP an den eigenen
// Prozess. Ohne Abfangen würde es den Test-Prozess beenden (genau der Fehler,
// den Build 87 behebt); mit closeSignals() kommt es im Kanal an.
func TestCloseSignalsReceivesRealHangup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGHUP gibt es unter Windows nicht")
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, closeSignals()...)
	defer signal.Stop(ch)
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got != syscall.SIGHUP {
			t.Errorf("Signal = %v, erwartet SIGHUP", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGHUP kam nicht im Kanal an")
	}
}

// --- Fälle 2 bis 4: exitTasks ---

// TestExitTasksRunNormalOrder prüft den normalen Ablauf: jeder Schritt genau
// einmal, Reihenfolge wie bisher in main.go (Verbindungen trennen, lokale
// Kopien löschen, Fenstergröße merken, ZULETZT das Update installieren).
func TestExitTasksRunNormalOrder(t *testing.T) {
	rec := &exitRecorder{}
	rec.tasks().runNormal()
	want := []string{"disconnect", "cleanup", "persist", "apply"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, want)
	}
}

// TestExitTasksRunNormalTwice prüft, dass ein zweiter Aufruf nichts doppelt tut
// (zwei Wege, ein Ende: Signal-Watcher und Hauptablauf).
func TestExitTasksRunNormalTwice(t *testing.T) {
	rec := &exitRecorder{}
	tasks := rec.tasks()
	tasks.runNormal()
	tasks.runNormal()
	tasks.runEmergency(100*time.Millisecond, nil)
	if got := rec.list(); len(got) != 4 {
		t.Errorf("Schritte = %v, erwartet je einen (4)", got)
	}
}

// TestExitTasksRunEmergencyOrder prüft den Notfall (Build 88 N1, Build 89): das
// Update kommt ALS ERSTES, noch vor allem anderen - ein hängendes disconnect
// darf das Update nicht verhindern, weil Windows nach rund 5 s hart beendet.
// Seit Build 89 steht disconnect ZULETZT (nach cleanup und persist): Es beendet
// die SSH-Sitzung, und danach muss Bubbletea Zeit haben, das Terminal selbst
// wiederherzustellen; hängt es, sind cleanup und persist schon erledigt.
func TestExitTasksRunEmergencyOrder(t *testing.T) {
	rec := &exitRecorder{}
	rec.tasks().runEmergency(time.Second, nil)
	want := []string{"apply", "cleanup", "persist", "disconnect"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, want)
	}
}

// TestExitTasksEmergencyPersistTimeout prüft den Randfall "persist hängt"
// (z.B. blockierter Konsolenaufruf): runEmergency kehrt nach dem Zeitlimit
// zurück, apply wurde vorher aufgerufen.
func TestExitTasksEmergencyPersistTimeout(t *testing.T) {
	rec := &exitRecorder{}
	release := make(chan struct{})
	defer close(release)
	tasks := rec.tasks()
	tasks.persist = func() { <-release }

	const limit = 150 * time.Millisecond
	start := time.Now()
	tasks.runEmergency(limit, nil)
	elapsed := time.Since(start)
	if elapsed < limit-20*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als das Zeitlimit %v", elapsed, limit)
	}
	if elapsed > limit+700*time.Millisecond {
		t.Errorf("Rückkehr erst nach %v, Zeitlimit war %v", elapsed, limit)
	}
	if rec.count("apply") != 1 {
		t.Errorf("apply lief %d mal, erwartet 1 (auch bei hängendem persist)", rec.count("apply"))
	}
}

// TestExitTasksNilStepsSkipped prüft, dass fehlende Schritte (nil) übersprungen
// werden statt abzustürzen - im Beenden-Pfad darf nichts paniken.
func TestExitTasksNilStepsSkipped(t *testing.T) {
	rec := &exitRecorder{}
	tasks := &exitTasks{apply: rec.step("apply")}
	tasks.runNormal()
	(&exitTasks{apply: rec.step("apply2")}).runEmergency(50*time.Millisecond, nil)
	(&exitTasks{}).runNormal()
	(&exitTasks{}).runEmergency(50*time.Millisecond, nil)
	if got := rec.list(); !reflect.DeepEqual(got, []string{"apply", "apply2"}) {
		t.Errorf("Schritte = %v", got)
	}
}

// TestExitTasksConcurrentRunsOnce (mit -race) startet runNormal und
// runEmergency gleichzeitig - Hauptablauf und Signal-Watcher treffen sich
// real so. Jeder Schritt muss genau einmal laufen, sonst würde z.B. das Setup
// doppelt gestartet.
func TestExitTasksConcurrentRunsOnce(t *testing.T) {
	for round := 0; round < 100; round++ {
		rec := &exitRecorder{}
		tasks := rec.tasks()
		var wg sync.WaitGroup
		wg.Add(3)
		start := make(chan struct{})
		go func() { defer wg.Done(); <-start; tasks.runNormal() }()
		go func() { defer wg.Done(); <-start; tasks.runEmergency(200*time.Millisecond, nil) }()
		go func() { defer wg.Done(); <-start; tasks.runNormal() }()
		close(start)
		wg.Wait()
		for _, name := range []string{"disconnect", "apply", "cleanup", "persist"} {
			if n := rec.count(name); n != 1 {
				t.Fatalf("Runde %d: %s lief %d mal, erwartet 1", round, name, n)
			}
		}
	}
}

// --- Fall 5: watchCloseSignals ---

// TestWatchCloseSignalsFinishesInTime prüft den Normalfall: Das Signal löst
// quit aus, das Programm endet von selbst innerhalb der Frist - kein Notfall,
// kein exit (der Hauptablauf beendet den Prozess selbst).
func TestWatchCloseSignalsFinishesInTime(t *testing.T) {
	h := startWatcher(500*time.Millisecond, func(h *watchHarness) {
		h.tasks.runNormal()
		close(h.allDone)
	}, nil)
	h.sigs <- syscall.SIGTERM
	if !waitChan(h.returned, 3*time.Second) {
		t.Fatal("Watcher kehrte nicht zurück")
	}
	// Länger als die Frist warten: ein verspäteter Notfall würde jetzt auftauchen
	time.Sleep(600 * time.Millisecond)
	select {
	case code := <-h.exitCodes:
		t.Fatalf("exit(%d) trotz rechtzeitigem Ende", code)
	default:
	}
	if h.quitCalls.Load() != 1 {
		t.Errorf("quit lief %d mal, erwartet 1", h.quitCalls.Load())
	}
	want := []string{"disconnect", "cleanup", "persist", "apply"}
	if got := h.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Schritte = %v, erwartet %v (normaler Ablauf)", got, want)
	}
}

// TestWatchCloseSignalsEmergencyAfterGrace prüft den Notfall: allDone bleibt
// offen (z.B. Bubbletea verwirft das Signal während tea.Exec). Nach der Frist
// - nicht früher - laufen die Aufgaben im Notfall-Ablauf (apply zuerst,
// disconnect zuletzt), dann exit(0).
func TestWatchCloseSignalsEmergencyAfterGrace(t *testing.T) {
	const grace = 150 * time.Millisecond
	h := startWatcher(grace, nil, nil)
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
		if elapsed := time.Since(h.started); elapsed < grace-20*time.Millisecond {
			t.Errorf("Notfall nach %v, vor Ablauf der Frist %v", elapsed, grace)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kein exit trotz offenem allDone")
	}
	if !waitChan(h.returned, 2*time.Second) {
		t.Error("Watcher kehrte nach dem Notfall nicht zurück")
	}
	want := []string{"apply", "cleanup", "persist", "disconnect"}
	if got := h.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Schritte = %v, erwartet Notfall-Reihenfolge %v (apply zuerst, disconnect zuletzt)", got, want)
	}
	if h.quitCalls.Load() != 1 {
		t.Errorf("quit lief %d mal, erwartet 1", h.quitCalls.Load())
	}
}

// TestWatchCloseSignalsNoSignal prüft, dass ohne Signal nichts geschieht und
// der Watcher endet, sobald das Programm normal fertig ist (allDone zu).
func TestWatchCloseSignalsNoSignal(t *testing.T) {
	h := startWatcher(50*time.Millisecond, nil, nil)
	time.Sleep(150 * time.Millisecond) // länger als die Frist: darf nichts auslösen
	select {
	case <-h.returned:
		t.Fatal("Watcher endete ohne Signal und ohne allDone")
	default:
	}
	close(h.allDone)
	if !waitChan(h.returned, 2*time.Second) {
		t.Fatal("Watcher endete nicht nach allDone")
	}
	select {
	case code := <-h.exitCodes:
		t.Fatalf("exit(%d) ohne Signal", code)
	default:
	}
	if h.quitCalls.Load() != 0 || len(h.rec.list()) != 0 {
		t.Errorf("ohne Signal: quit %d mal, Schritte %v", h.quitCalls.Load(), h.rec.list())
	}
}

// TestWatchCloseSignalsTwoSignals prüft, dass ein zweites Signal (Windows
// schickt bei Alt+F4 und Kreuz mitunter mehrere) keine zweite Behandlung
// auslöst: ein quit, ein exit, jeder Schritt einmal. Zweimal geprüft: beide
// Signale schon im Kanal, und das zweite mitten in der Wartezeit.
func TestWatchCloseSignalsTwoSignals(t *testing.T) {
	t.Run("beide sofort", func(t *testing.T) {
		h := startWatcher(100*time.Millisecond, nil, nil)
		h.sigs <- syscall.SIGTERM
		h.sigs <- syscall.SIGTERM
		if !waitChan(h.returned, 3*time.Second) {
			t.Fatal("Watcher kehrte nicht zurück")
		}
		time.Sleep(250 * time.Millisecond)
		if len(h.exitCodes) != 1 || h.quitCalls.Load() != 1 {
			t.Errorf("exit %d mal, quit %d mal, erwartet je 1", len(h.exitCodes), h.quitCalls.Load())
		}
		if len(h.rec.list()) != 4 {
			t.Errorf("Schritte = %v, erwartet je einen", h.rec.list())
		}
	})
	t.Run("zweites in der Wartezeit", func(t *testing.T) {
		h := startWatcher(200*time.Millisecond, nil, nil)
		h.sigs <- syscall.SIGHUP
		time.Sleep(50 * time.Millisecond)
		h.sigs <- syscall.SIGTERM
		if !waitChan(h.returned, 3*time.Second) {
			t.Fatal("Watcher kehrte nicht zurück")
		}
		time.Sleep(250 * time.Millisecond)
		if len(h.exitCodes) != 1 || h.quitCalls.Load() != 1 {
			t.Errorf("exit %d mal, quit %d mal, erwartet je 1", len(h.exitCodes), h.quitCalls.Load())
		}
	})
}

// TestWatchCloseSignalsBlockingQuit prüft den Randfall tea.Exec: p.Quit()
// blockiert dort, weil niemand die Nachricht annimmt. Der Watcher muss
// trotzdem nach der Frist den Notfall auslösen.
func TestWatchCloseSignalsBlockingQuit(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := startWatcher(100*time.Millisecond, func(h *watchHarness) { <-release }, nil)
	h.sigs <- syscall.SIGTERM
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Watcher hing an blockierendem quit")
	}
	if !waitChan(h.returned, 2*time.Second) {
		t.Error("Watcher kehrte wegen blockierendem quit nicht zurück")
	}
	if h.rec.count("apply") != 1 {
		t.Errorf("apply lief %d mal, erwartet 1", h.rec.count("apply"))
	}
}

// TestWatchCloseSignalsTotalBudget prüft die Gesamtzeit: Frist plus
// Notfall-Zeitlimit liegen unter 3 s (Windows beendet nach ca. 5 s), und der
// Watcher hält sie auch bei hängendem persist ein.
func TestWatchCloseSignalsTotalBudget(t *testing.T) {
	if closeGracePeriod+emergencyStepsTimeout >= 3*time.Second {
		t.Errorf("Frist %v + Schritte-Limit %v >= 3 s", closeGracePeriod, emergencyStepsTimeout)
	}
	if closeGracePeriod <= 0 || emergencyStepsTimeout <= 0 {
		t.Fatal("Frist und Zeitlimit müssen positiv sein")
	}
	release := make(chan struct{})
	defer close(release)
	const grace = 100 * time.Millisecond
	h := startWatcher(grace, nil, func(h *watchHarness) *exitTasks {
		tasks := h.rec.tasks()
		tasks.persist = func() { <-release }
		return tasks
	})
	h.sigs <- syscall.SIGHUP
	select {
	case <-h.exitCodes:
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit bei hängendem persist")
	}
	if elapsed := time.Since(h.started); elapsed > grace+emergencyStepsTimeout+time.Second {
		t.Errorf("Notfall dauerte %v, erlaubt %v", elapsed, grace+emergencyStepsTimeout)
	}
	if h.rec.count("apply") != 1 {
		t.Errorf("apply lief %d mal, erwartet 1", h.rec.count("apply"))
	}
}

// --- Fall 10: exitCodeFor, finishRun ---

// TestExitCodeFor prüft die Exit-Codes: Fehler aus Run() zählt nur dann als
// Fehler, wenn nicht gerade das Fenster geschlossen wurde (Bubbletea meldet
// beim Schließen mitunter einen Fehler, der kein Programmfehler ist).
func TestExitCodeFor(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name   string
		err    error
		closed bool
		want   int
	}{
		{"kein Fehler", nil, false, 0},
		{"kein Fehler, Fenster geschlossen", nil, true, 0},
		{"Fehler", boom, false, 1},
		{"Fehler beim Schließen", boom, true, 0},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err, c.closed); got != c.want {
			t.Errorf("%s: %d, erwartet %d", c.name, got, c.want)
		}
	}
}

// TestFinishRunRunsTasksOnError prüft den Randfall "Run() liefert einen
// Fehler": Früher ging es mit os.Exit(1) sofort raus, ohne Trennen,
// Aufräumen und Update. Jetzt laufen alle Aufgaben trotzdem, der Fehler wird
// protokolliert, allDone wird erst NACH den Aufgaben geschlossen.
func TestFinishRunRunsTasksOnError(t *testing.T) {
	rec := &exitRecorder{}
	allDone := make(chan struct{})
	closedEarly := false
	tasks := rec.tasks()
	inner := tasks.apply
	tasks.apply = func() {
		select {
		case <-allDone:
			closedEarly = true
		default:
		}
		inner()
	}
	logPath := filepath.Join(t.TempDir(), "test.log")
	log := &Logger{filePath: logPath, enabled: true}

	code := finishRun(errors.New("tty kaputt-4711"), false, tasks, allDone, log)
	if code != 1 {
		t.Errorf("Code = %d, erwartet 1", code)
	}
	for _, name := range []string{"disconnect", "apply", "cleanup", "persist"} {
		if rec.count(name) != 1 {
			t.Errorf("%s lief %d mal, erwartet 1", name, rec.count(name))
		}
	}
	if closedEarly {
		t.Error("allDone war schon geschlossen, bevor die Aufgaben fertig waren")
	}
	if !waitChan(allDone, time.Second) {
		t.Error("allDone wurde nicht geschlossen")
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "tty kaputt-4711") {
		t.Errorf("Fehler nicht protokolliert, Log: %q", data)
	}
}

// TestFinishRunNormalAndClosed prüft die übrigen Codes: ohne Fehler 0; Fehler
// nach Fenster-Schließen ebenfalls 0 (die Aufgaben laufen in beiden Fällen).
func TestFinishRunNormalAndClosed(t *testing.T) {
	for _, c := range []struct {
		err    error
		closed bool
		want   int
	}{{nil, false, 0}, {nil, true, 0}, {errors.New("x"), true, 0}} {
		rec := &exitRecorder{}
		allDone := make(chan struct{})
		if code := finishRun(c.err, c.closed, rec.tasks(), allDone, nil); code != c.want {
			t.Errorf("err=%v closed=%v: Code %d, erwartet %d", c.err, c.closed, code, c.want)
		}
		if len(rec.list()) != 4 || !waitChan(allDone, time.Second) {
			t.Errorf("err=%v closed=%v: Schritte %v", c.err, c.closed, rec.list())
		}
	}
}

// =============================================================================
// Build 88
// =============================================================================

// waitCount wartet höchstens d darauf, dass der Schritt name n-mal gelaufen ist.
func waitCount(rec *exitRecorder, name string, n int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if rec.count(name) >= n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return rec.count(name) >= n
}

// --- N1: apply zuerst, ein gemeinsames Zeitlimit ---

// TestExitTasksEmergencyDisconnectHangs prüft den Kernfall von N1 und der
// neuen Reihenfolge (Build 89): disconnect hängt (z.B. tote Verbindung ohne
// Antwort). Weil disconnect jetzt ZULETZT kommt, sind apply, cleanup und persist
// trotzdem gelaufen, und runEmergency kehrt nach etwa deadline zurück statt zu
// hängen. Nach dem Freigeben läuft disconnect genau einmal zu Ende.
func TestExitTasksEmergencyDisconnectHangs(t *testing.T) {
	rec := &exitRecorder{}
	release := make(chan struct{})
	tasks := rec.tasks()
	record := rec.step("disconnect")
	tasks.disconnect = func() { <-release; record() }

	const limit = 150 * time.Millisecond
	start := time.Now()
	returned := make(chan struct{})
	go func() { defer close(returned); tasks.runEmergency(limit, nil) }()
	if !waitChan(returned, 3*time.Second) {
		close(release)
		t.Fatal("runEmergency kehrte nicht zurück (hängt an disconnect)")
	}
	elapsed := time.Since(start)
	if elapsed < limit-20*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als das Zeitlimit %v", elapsed, limit)
	}
	if elapsed > limit+emergencyTolerance {
		t.Errorf("Rückkehr erst nach %v, Zeitlimit war %v (hängt an disconnect)", elapsed, limit)
	}
	// Schon VOR dem Freigeben müssen die anderen drei Schritte gelaufen sein
	want := []string{"apply", "cleanup", "persist"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("bei hängendem disconnect: Schritte = %v, erwartet %v", got, want)
	}
	close(release)
	if !waitCount(rec, "disconnect", 1, 2*time.Second) {
		t.Fatalf("nach dem Freigeben lief disconnect nicht: %v", rec.list())
	}
	time.Sleep(30 * time.Millisecond)
	if rec.count("disconnect") != 1 {
		t.Errorf("disconnect lief %d mal, erwartet 1", rec.count("disconnect"))
	}
}

// TestExitTasksEmergencyDeadlineIsShared prüft, dass das Zeitlimit für ALLE
// Schritte nach apply zusammen gilt (nicht je Schritt): Drei Schritte zu je
// 200 ms brauchen 600 ms; bei 300 ms Limit muss runEmergency nach rund 300 ms
// zurückkehren. Mit einem Limit je Schritt würde es 600 ms dauern - und im
// Ernstfall Windows' harte Beendigung nach ca. 5 s herausfordern.
func TestExitTasksEmergencyDeadlineIsShared(t *testing.T) {
	rec := &exitRecorder{}
	slow := func(name string) func() {
		inner := rec.step(name)
		return func() { time.Sleep(200 * time.Millisecond); inner() }
	}
	tasks := &exitTasks{
		apply:      rec.step("apply"),
		disconnect: slow("disconnect"),
		cleanup:    slow("cleanup"),
		persist:    slow("persist"),
	}
	const limit = 300 * time.Millisecond
	start := time.Now()
	tasks.runEmergency(limit, nil)
	elapsed := time.Since(start)
	if elapsed < limit-30*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als das Zeitlimit %v", elapsed, limit)
	}
	if elapsed > limit+250*time.Millisecond {
		t.Errorf("Rückkehr nach %v, das gemeinsame Zeitlimit %v wurde überschritten", elapsed, limit)
	}
	// Aufräumen: die abgegebene Goroutine läuft zu Ende (keine Zusatzprüfung nötig);
	// disconnect kommt seit Build 89 als letzter der drei Schritte
	waitCount(rec, "disconnect", 1, 2*time.Second)
}

// TestExitTasksEmergencyFastStepsDoNotWait prüft den Normalfall ohne allDone
// (nil): Laufen alle Schritte schnell durch, kehrt runEmergency sofort zurück
// (nicht erst nach dem Zeitlimit - bei nil wird nicht auf allDone gewartet) und
// alle vier Schritte sind fertig.
func TestExitTasksEmergencyFastStepsDoNotWait(t *testing.T) {
	rec := &exitRecorder{}
	start := time.Now()
	rec.tasks().runEmergency(5*time.Second, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("runEmergency brauchte %v bei sofort fertigen Schritten", elapsed)
	}
	if len(rec.list()) != 4 {
		t.Errorf("Schritte = %v, erwartet 4", rec.list())
	}
}

// TestWatchCloseSignalsEmergencyDisconnectHangs prüft N1 im Watcher: Das Signal
// kommt, quit hängt (tea.Exec), disconnect hängt auch. Der Watcher muss trotzdem
// nach grace + Zeitlimit exit(0) aufrufen, und apply muss gelaufen sein.
func TestWatchCloseSignalsEmergencyDisconnectHangs(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	const grace = 100 * time.Millisecond
	h := startWatcher(grace, func(h *watchHarness) { <-release }, func(h *watchHarness) *exitTasks {
		tasks := h.rec.tasks()
		tasks.disconnect = func() { <-release }
		return tasks
	})
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit(0) bei hängendem disconnect")
	}
	if elapsed := time.Since(h.started); elapsed > grace+emergencyStepsTimeout+time.Second {
		t.Errorf("Notfall dauerte %v, erlaubt %v", elapsed, grace+emergencyStepsTimeout)
	}
	if h.rec.count("apply") != 1 {
		t.Errorf("apply lief %d mal, erwartet 1", h.rec.count("apply"))
	}
	// Seit Build 89 sind cleanup und persist VOR dem hängenden disconnect dran
	if h.rec.count("cleanup") != 1 || h.rec.count("persist") != 1 {
		t.Errorf("Schritte = %v, erwartet cleanup und persist trotz hängendem disconnect", h.rec.list())
	}
}

// --- N4: closeRequested synchron setzen ---

// TestWatchCloseSignalsOnSignalBeforeQuit prüft N4: onSignal läuft beim ersten
// Signal SYNCHRON und ist fertig, bevor quit startet. Gemessen wird in quit,
// ob der von onSignal gesetzte Merker schon steht - 100 Runden mit kurzer
// Verzögerung in onSignal (hätte der Watcher onSignal nebenläufig gestartet,
// sähe quit den Merker nicht).
func TestWatchCloseSignalsOnSignalBeforeQuit(t *testing.T) {
	for round := 0; round < 100; round++ {
		var flag atomic.Bool
		var calls atomic.Int32
		saw := make(chan bool, 1)
		h := startWatcherWithSignal(2*time.Second,
			func(h *watchHarness) {
				calls.Add(1)
				time.Sleep(time.Millisecond)
				flag.Store(true)
			},
			func(h *watchHarness) {
				saw <- flag.Load()
				close(h.allDone)
			}, nil)
		h.sigs <- syscall.SIGHUP
		select {
		case got := <-saw:
			if !got {
				t.Fatalf("Runde %d: quit lief, bevor onSignal fertig war", round)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("Runde %d: quit wurde nicht aufgerufen", round)
		}
		if !waitChan(h.returned, 3*time.Second) {
			t.Fatalf("Runde %d: Watcher kehrte nicht zurück", round)
		}
		if calls.Load() != 1 {
			t.Fatalf("Runde %d: onSignal lief %d mal, erwartet 1", round, calls.Load())
		}
	}
}

// TestWatchCloseSignalsOnSignalSlow prüft N4 mit deutlich langsamem onSignal
// (100 ms): quit darf in dieser Zeit nicht beginnen.
func TestWatchCloseSignalsOnSignalSlow(t *testing.T) {
	var onSignalDone atomic.Bool
	saw := make(chan bool, 1)
	h := startWatcherWithSignal(2*time.Second,
		func(h *watchHarness) {
			time.Sleep(100 * time.Millisecond)
			onSignalDone.Store(true)
		},
		func(h *watchHarness) {
			saw <- onSignalDone.Load()
			close(h.allDone)
		}, nil)
	h.sigs <- syscall.SIGTERM
	select {
	case got := <-saw:
		if !got {
			t.Error("quit begann, während onSignal noch lief")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quit wurde nicht aufgerufen")
	}
}

// TestWatchCloseSignalsOnSignalOnceForTwoSignals prüft, dass zwei Signale
// (Windows schickt bei Alt+F4 und Kreuz mitunter mehrere) onSignal nur einmal
// auslösen, und dass ohne Signal onSignal gar nicht läuft.
func TestWatchCloseSignalsOnSignalOnceForTwoSignals(t *testing.T) {
	var calls atomic.Int32
	h := startWatcherWithSignal(100*time.Millisecond,
		func(h *watchHarness) { calls.Add(1) }, nil, nil)
	time.Sleep(60 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("onSignal lief %d mal ohne Signal", calls.Load())
	}
	h.sigs <- syscall.SIGTERM
	h.sigs <- syscall.SIGTERM
	if !waitChan(h.returned, 3*time.Second) {
		t.Fatal("Watcher kehrte nicht zurück")
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Errorf("onSignal lief %d mal, erwartet 1", calls.Load())
	}
}

// TestWatchCloseSignalsNilOnSignalAllowed prüft: onSignal == nil ist erlaubt
// (Notfall läuft wie bisher, kein Absturz).
func TestWatchCloseSignalsNilOnSignalAllowed(t *testing.T) {
	h := startWatcherWithSignal(80*time.Millisecond, nil, nil, nil)
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kein exit mit onSignal == nil")
	}
}

// TestCloseRequestedVisibleWhenRunReturnsAtOnce prüft den Randfall von N4 wie
// in main.go: Bubbletea liefert nach dem Quit sofort einen Fehler aus Run()
// zurück. main liest dann closeRequested - das muss schon true sein, weil
// onSignal es VOR quit gesetzt hat. Erwartet: Exit-Code 0 (kein Programmfehler,
// das Fenster wurde geschlossen), alle Aufgaben liefen. 100 Runden, weil ein
// zu spät gesetzter Merker nur manchmal auffällt.
func TestCloseRequestedVisibleWhenRunReturnsAtOnce(t *testing.T) {
	for round := 0; round < 100; round++ {
		var closeRequested atomic.Bool
		rec := &exitRecorder{}
		tasks := rec.tasks()
		codes := make(chan int, 1)
		h := startWatcherWithSignal(2*time.Second,
			func(h *watchHarness) { closeRequested.Store(true) },
			func(h *watchHarness) {
				// So endet main: Run() kehrt sofort mit Fehler zurück
				codes <- finishRun(errors.New("program was killed"), closeRequested.Load(), tasks, h.allDone, nil)
			},
			func(h *watchHarness) *exitTasks { return tasks })
		h.sigs <- syscall.SIGHUP
		select {
		case code := <-codes:
			if code != 0 {
				t.Fatalf("Runde %d: Exit-Code %d, erwartet 0 (closeRequested nicht rechtzeitig gesetzt)", round, code)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("Runde %d: finishRun wurde nicht erreicht", round)
		}
		if !waitChan(h.returned, 3*time.Second) {
			t.Fatalf("Runde %d: Watcher kehrte nicht zurück", round)
		}
		if len(h.exitCodes) != 0 {
			t.Fatalf("Runde %d: exit trotz normalem Ende", round)
		}
		if len(rec.list()) != 4 {
			t.Fatalf("Runde %d: Schritte = %v", round, rec.list())
		}
	}
}

// TestMainSetsCloseRequestedInOnSignal prüft den Quelltext von main.go: Der
// Merker closeRequested wird in einer eigenen Funktion (onSignal) gesetzt, die
// VOR p.Quit steht und nicht dieselbe Funktion wie quit ist. Steht Store(true)
// im selben Funktionsliteral wie p.Quit(), läuft es erst in der quit-Goroutine
// (das Rennen, das N4 beseitigt).
func TestMainSetsCloseRequestedInOnSignal(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	from := strings.Index(text, "watchCloseSignals(")
	to := strings.Index(text, "p.Run()")
	if from < 0 || to < from {
		t.Fatalf("watchCloseSignals(...) vor p.Run() nicht gefunden in main.go")
	}
	call := text[from:to]
	store := strings.Index(call, "closeRequested.Store(true)")
	quit := strings.Index(call, "p.Quit")
	if store < 0 || quit < 0 {
		t.Fatalf("closeRequested.Store(true) oder p.Quit fehlt im Aufruf:\n%s", call)
	}
	if store > quit {
		t.Error("closeRequested.Store(true) steht hinter p.Quit - onSignal muss als erster Funktionsparameter vor quit stehen")
	}
	if store < quit && !strings.Contains(call[store:quit], "}") {
		t.Error("closeRequested.Store(true) und p.Quit stehen im selben Funktionsliteral - onSignal und quit müssen getrennt sein")
	}
}

// --- N5: Terminal im Notfall wiederherstellen ---

// TestEmergencyRestoreCalledOnce prüft: Eine registrierte Funktion wird von
// runEmergencyRestore aufgerufen, mehrere Funktionen alle.
func TestEmergencyRestoreCalledOnce(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("a")))
	t.Cleanup(registerEmergencyRestore(rec.step("b")))
	runEmergencyRestore()
	if rec.count("a") != 1 || rec.count("b") != 1 || len(rec.list()) != 2 {
		t.Errorf("aufgerufen: %v, erwartet je a und b einmal", rec.list())
	}
}

// TestEmergencyRestoreUnregister prüft das Abmelden: Eine abgemeldete Funktion
// wird nicht mehr aufgerufen (das Terminal ist normal zurückgesetzt, ein
// zweites Restore mit altem Zustand wäre falsch), die anderen bleiben
// erhalten. Doppeltes Abmelden ist harmlos.
func TestEmergencyRestoreUnregister(t *testing.T) {
	rec := &exitRecorder{}
	unA := registerEmergencyRestore(rec.step("a"))
	t.Cleanup(registerEmergencyRestore(rec.step("b")))
	unA()
	unA() // zweites Abmelden: kein Absturz, nichts Falsches abgemeldet
	runEmergencyRestore()
	if got := rec.list(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("aufgerufen: %v, erwartet nur b", got)
	}
}

// TestEmergencyRestoreNothingRegistered prüft den Randfall "nichts
// registriert" (kein Terminal-Modus aktiv): runEmergencyRestore tut nichts.
func TestEmergencyRestoreNothingRegistered(t *testing.T) {
	runEmergencyRestore()
	// Registrieren und sofort abmelden: danach wieder leer
	rec := &exitRecorder{}
	registerEmergencyRestore(rec.step("x"))()
	runEmergencyRestore()
	if len(rec.list()) != 0 {
		t.Errorf("aufgerufen: %v, erwartet nichts", rec.list())
	}
}

// TestEmergencyRestorePanicDoesNotStopOthers prüft den Randfall "eine
// Wiederherstellung paniert" (z.B. ungültiger Handle): Die anderen laufen
// trotzdem, und runEmergencyRestore selbst paniert nicht. Auch eine nil-
// Funktion darf nichts verhindern.
func TestEmergencyRestorePanicDoesNotStopOthers(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("vorher")))
	t.Cleanup(registerEmergencyRestore(func() { panic("Terminal kaputt") }))
	t.Cleanup(registerEmergencyRestore(nil))
	t.Cleanup(registerEmergencyRestore(rec.step("nachher")))
	runEmergencyRestore()
	if rec.count("vorher") != 1 || rec.count("nachher") != 1 {
		t.Errorf("aufgerufen: %v, erwartet vorher und nachher je einmal", rec.list())
	}
}

// TestEmergencyRestoreConcurrent (mit -race) registriert, meldet ab und ruft
// gleichzeitig aus mehreren Goroutinen auf: Signal-Watcher und Terminal-
// Sitzung treffen sich real so. Am Ende ist nichts mehr registriert.
func TestEmergencyRestoreConcurrent(t *testing.T) {
	var calls atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				un := registerEmergencyRestore(func() { calls.Add(1) })
				un()
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				runEmergencyRestore()
			}
		}()
	}
	wg.Wait()
	calls.Store(0)
	runEmergencyRestore()
	if calls.Load() != 0 {
		t.Errorf("nach dem Abmelden aller Funktionen liefen noch %d", calls.Load())
	}
}

// TestRunEmergencyRestoresTerminalFirst prüft N5 und die Reihenfolge von Build
// 89: runEmergency stellt das Terminal ALS ALLERERSTES wieder her, noch vor
// apply (nur term.Restore, ms), und zum Schluss (ohne allDone) ein zweites Mal -
// auch dann, wenn die Wiederherstellung paniert, laufen die Schritte danach.
func TestRunEmergencyRestoresTerminalFirst(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	rec.tasks().runEmergency(time.Second, nil)
	want := []string{"restore", "apply", "cleanup", "persist", "disconnect", "restore"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, want)
	}

	rec2 := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(func() { panic("Handle ungültig") }))
	rec2.tasks().runEmergency(time.Second, nil)
	if rec2.count("apply") != 1 || rec2.count("persist") != 1 || rec2.count("disconnect") != 1 {
		t.Errorf("nach panischer Wiederherstellung: Schritte = %v", rec2.list())
	}
}

// TestWatchCloseSignalsEmergencyRestoresTerminal prüft N5 im Watcher: Im
// Notfall (kein normales Ende) wird das Terminal vor allen Schritten
// wiederhergestellt, dann exit(0).
func TestWatchCloseSignalsEmergencyRestoresTerminal(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	h := startWatcher(80*time.Millisecond, nil, func(h *watchHarness) *exitTasks { return rec.tasks() })
	h.sigs <- syscall.SIGHUP
	select {
	case <-h.exitCodes:
	case <-time.After(3 * time.Second):
		t.Fatal("kein exit im Notfall")
	}
	got := rec.list()
	if len(got) == 0 || got[0] != "restore" {
		t.Errorf("Reihenfolge = %v, erwartet restore zuerst", got)
	}
	// allDone blieb offen: Build 89 setzt den Zustand zum Schluss noch einmal
	// zurück, bevor exit(0) kommt (exit wurde erst NACH runEmergency gerufen)
	if rec.count("restore") != 2 || got[len(got)-1] != "restore" {
		t.Errorf("Reihenfolge = %v, erwartet restore als Erstes und als Letztes (zweimal)", got)
	}
}

// TestWatchCloseSignalsNormalEndNoRestore prüft: Endet das Programm rechtzeitig
// normal, ruft der Watcher die Notfall-Wiederherstellung NICHT auf (das
// Terminal wird dann von der Sitzung selbst zurückgesetzt).
func TestWatchCloseSignalsNormalEndNoRestore(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	h := startWatcher(500*time.Millisecond, func(h *watchHarness) { close(h.allDone) }, nil)
	h.sigs <- syscall.SIGTERM
	if !waitChan(h.returned, 3*time.Second) {
		t.Fatal("Watcher kehrte nicht zurück")
	}
	if rec.count("restore") != 0 {
		t.Errorf("Wiederherstellung lief %d mal beim normalen Ende", rec.count("restore"))
	}
}

// matchingBraceEnd liefert die Position der schließenden geschweiften Klammer
// zur öffnenden bei text[open]. Zeilenkommentare, Zeichenketten (auch Raw-
// Strings) und Runen-Literale werden übersprungen, damit Klammern darin nicht
// mitzählen. -1, wenn keine passende Klammer folgt.
func matchingBraceEnd(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch c := text[i]; c {
		case '/':
			if i+1 < len(text) && text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
			}
		case '"', '\'':
			for i++; i < len(text) && text[i] != c; i++ {
				if text[i] == '\\' {
					i++
				}
			}
		case '`':
			for i++; i < len(text) && text[i] != '`'; i++ {
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TestMatchingBraceEnd prüft den Hilfsmann der Quelltextprüfung selbst (ein
// falsch zählender Helfer ließe die Prüfung unten grundlos grün oder rot
// werden): verschachtelte Klammern, Klammern in Kommentaren, Zeichenketten
// und Runen-Literalen, fehlende schließende Klammer.
func TestMatchingBraceEnd(t *testing.T) {
	cases := []struct {
		name, text string
		want       int
	}{
		{"einfach", "{ a }", 4},
		{"verschachtelt", "{ { } }", 6},
		{"Kommentar", "{ // } \n }", 9},
		{"Zeichenkette", `{ "}" }`, 6},
		{"Escape in Zeichenkette", `{ "\"}" }`, 8},
		{"Rune", "{ '}' }", 6},
		{"Raw-String", "{ `}` }", 6},
		{"offen", "{ { }", -1},
	}
	for _, c := range cases {
		if got := matchingBraceEnd(c.text, 0); got != c.want {
			t.Errorf("%s: matchingBraceEnd(%q) = %d, erwartet %d", c.name, c.text, got, c.want)
		}
	}
}

// TestSSHTerminalRegistersEmergencyRestore prüft den Quelltext von
// ssh_terminal.go (verschärft in Build 89, OPTIMIZE.md Build 88): Nach
// term.MakeRaw wird die Wiederherstellung mit registerEmergencyRestore
// angemeldet. Das Funktionsliteral (bis zur passenden Klammer, nicht bis zum
// nächsten Auftreten irgendwo in der Datei) muss term.Restore mit dem alten
// Zustand und danach restoreVT() enthalten. Im anschließenden defer muss
// term.Restore VOR unregister() stehen: Würde zuerst abgemeldet, gäbe es einen
// Moment, in dem ein Notfall das Terminal im Raw-Modus zurückließe. Die Sitzung
// selbst lässt sich ohne echtes Terminal nicht starten.
func TestSSHTerminalRegistersEmergencyRestore(t *testing.T) {
	data, err := os.ReadFile("ssh_terminal.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	raw := strings.Index(text, "term.MakeRaw(")
	if raw < 0 {
		t.Fatal("term.MakeRaw fehlt in ssh_terminal.go")
	}
	rest := text[raw:]

	// Anmeldung als Funktionsliteral: registerEmergencyRestore(func() { ... })
	const regCall = "registerEmergencyRestore(func() {"
	reg := strings.Index(rest, regCall)
	if reg < 0 {
		t.Fatal(regCall + " fehlt hinter term.MakeRaw in ssh_terminal.go (Funktionsliteral erwartet)")
	}
	if !strings.Contains(rest[:reg], "unregister") {
		t.Error("Die Abmeldefunktion wird nicht in einer Variablen (unregister) gehalten")
	}
	open := reg + len(regCall) - 1
	end := matchingBraceEnd(rest, open)
	if end < 0 || !strings.HasPrefix(rest[end:], "})") {
		t.Fatalf("Ende des Funktionsliterals nicht als \"})\" gefunden (Position %d)", end)
	}
	literal := rest[open : end+2]
	restoreAt := strings.Index(literal, "term.Restore(stdinFd, oldState)")
	vtAt := strings.Index(literal, "restoreVT()")
	if restoreAt < 0 {
		t.Error("term.Restore(stdinFd, oldState) fehlt im angemeldeten Funktionsliteral")
	}
	if vtAt < 0 {
		t.Error("restoreVT() fehlt im angemeldeten Funktionsliteral")
	}
	if restoreAt >= 0 && vtAt >= 0 && restoreAt > vtAt {
		t.Error("restoreVT() steht vor term.Restore im Funktionsliteral: erst Raw-Modus zurück, dann VT-Modus")
	}
	if strings.Contains(literal, "unregister(") {
		t.Error("Das Funktionsliteral meldet sich selbst ab - das gehört in den defer")
	}

	// Der defer hinter der Anmeldung: term.Restore, dann unregister()
	afterLiteral := rest[end+2:]
	const deferCall = "defer func() {"
	d := strings.Index(afterLiteral, deferCall)
	if d < 0 {
		t.Fatal("defer func() { fehlt hinter der Anmeldung: die Wiederherstellung wird am Ende nicht abgemeldet")
	}
	dOpen := d + len(deferCall) - 1
	dEnd := matchingBraceEnd(afterLiteral, dOpen)
	if dEnd < 0 {
		t.Fatal("Ende des defer-Blocks nicht gefunden")
	}
	body := afterLiteral[dOpen : dEnd+1]
	rs := strings.Index(body, "term.Restore(stdinFd")
	un := strings.Index(body, "unregister()")
	if rs < 0 {
		t.Error("term.Restore(stdinFd, ...) fehlt im defer-Block")
	}
	if un < 0 {
		t.Error("unregister() fehlt im defer-Block: die Wiederherstellung wird am Ende nicht abgemeldet")
	}
	if rs >= 0 && un >= 0 && un < rs {
		t.Error("unregister() steht VOR term.Restore im defer: erst wiederherstellen, dann abmelden")
	}
}

// =============================================================================
// Build 89
// =============================================================================

// emergencyTolerance - Toleranz für "Rückkehr nach ca. deadline" (Zeitgeber und
// Goroutinen-Wechsel auf einem belasteten Rechner, auch mit -race).
const emergencyTolerance = 300 * time.Millisecond

// restoreOrder ist die erwartete Reihenfolge ohne rechtzeitiges allDone:
// Terminal, Update, Aufräumen, Speichern, Trennen, Terminal ein zweites Mal.
var restoreOrder = []string{"restore", "apply", "cleanup", "persist", "disconnect", "restore"}

// TestExitTasksEmergencyWaitsForAllDoneUntilDeadline prüft Punkt 4 und 5 der
// neuen Notfall-Reihenfolge: Alle Schritte sind sofort fertig, allDone bleibt
// offen (Bubbletea kehrt nicht zurück). runEmergency wartet dann im Rest
// derselben deadline auf allDone - nicht kürzer, nicht länger - und setzt das
// Terminal unmittelbar vor der Rückkehr ein ZWEITES Mal zurück (Bubbletea hat
// den Raw-Modus womöglich wieder eingeschaltet). Reihenfolge laut Lead:
// restore, apply, cleanup, persist, disconnect, restore.
func TestExitTasksEmergencyWaitsForAllDoneUntilDeadline(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	allDone := make(chan struct{})
	const limit = 300 * time.Millisecond
	start := time.Now()
	rec.tasks().runEmergency(limit, allDone)
	elapsed := time.Since(start)
	if elapsed < limit-30*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als die deadline %v (es muss auf allDone gewartet werden)", elapsed, limit)
	}
	if elapsed > limit+emergencyTolerance {
		t.Errorf("Rückkehr erst nach %v, deadline war %v", elapsed, limit)
	}
	if got := rec.list(); !reflect.DeepEqual(got, restoreOrder) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, restoreOrder)
	}
}

// TestExitTasksEmergencyNilAllDoneDoesNotWait prüft den Randfall nil-allDone:
// nicht warten. Alle Schritte laufen, das Terminal wird zweimal zurückgesetzt
// (es gibt keine Rückmeldung, ob Bubbletea übernommen hat), und die Rückkehr
// kommt sofort - nicht erst nach der (hier großzügigen) deadline.
func TestExitTasksEmergencyNilAllDoneDoesNotWait(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	start := time.Now()
	rec.tasks().runEmergency(5*time.Second, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("runEmergency brauchte %v mit allDone == nil, erwartet sofortige Rückkehr", elapsed)
	}
	if got := rec.list(); !reflect.DeepEqual(got, restoreOrder) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, restoreOrder)
	}
}

// TestExitTasksEmergencyAllDoneClosedEarly prüft: Ist allDone schon geschlossen
// (main war schneller), endet das Warten sofort - und das zweite Restore
// unterbleibt, denn Bubbletea hat das Terminal selbst wiederhergestellt; ein
// altes Restore danach würde einen veralteten Zustand einsetzen. Die Schritte
// laufen trotzdem alle einmal.
func TestExitTasksEmergencyAllDoneClosedEarly(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	allDone := make(chan struct{})
	close(allDone)
	start := time.Now()
	rec.tasks().runEmergency(2*time.Second, allDone)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("runEmergency brauchte %v bei geschlossenem allDone und deadline 2 s", elapsed)
	}
	want := []string{"restore", "apply", "cleanup", "persist", "disconnect"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge = %v, erwartet %v (nur EIN restore, da allDone rechtzeitig kam)", got, want)
	}
}

// TestExitTasksEmergencyAllDoneClosedByDisconnect bildet den echten Ablauf nach:
// disconnect beendet die SSH-Sitzung, Bubbletea kehrt zurück und main schließt
// kurz darauf allDone (hier 50 ms nach dem Trennen). runEmergency muss dann
// sofort zurückkehren (lange vor der deadline von 2 s) und darf das Terminal
// nicht ein zweites Mal zurücksetzen.
func TestExitTasksEmergencyAllDoneClosedByDisconnect(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	allDone := make(chan struct{})
	tasks := rec.tasks()
	record := rec.step("disconnect")
	tasks.disconnect = func() {
		record()
		go func() { time.Sleep(50 * time.Millisecond); close(allDone) }()
	}
	start := time.Now()
	tasks.runEmergency(2*time.Second, allDone)
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("runEmergency brauchte %v, obwohl allDone nach 50 ms kam", elapsed)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("Rückkehr nach %v, vor dem Schließen von allDone", elapsed)
	}
	if rec.count("restore") != 1 {
		t.Errorf("restore lief %d mal, erwartet 1 (allDone kam rechtzeitig): %v", rec.count("restore"), rec.list())
	}
}

// TestExitTasksEmergencySharedBudgetForWait prüft, dass Schritte UND Warten auf
// allDone EINE deadline teilen: Dauern die Schritte 200 ms (cleanup) von 300 ms,
// bleiben für allDone nur ca. 100 ms. Mit einer eigenen deadline fürs Warten
// käme die Rückkehr erst nach ca. 500 ms - im Ernstfall Windows' harte
// Beendigung herausfordernd.
func TestExitTasksEmergencySharedBudgetForWait(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	tasks := rec.tasks()
	inner := rec.step("cleanup")
	tasks.cleanup = func() { time.Sleep(200 * time.Millisecond); inner() }
	const limit = 300 * time.Millisecond
	start := time.Now()
	tasks.runEmergency(limit, make(chan struct{}))
	elapsed := time.Since(start)
	if elapsed < limit-30*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als die deadline %v", elapsed, limit)
	}
	if elapsed > limit+150*time.Millisecond {
		t.Errorf("Rückkehr nach %v, die gemeinsame deadline %v wurde überschritten (Warten hat eine eigene deadline?)", elapsed, limit)
	}
	if got := rec.list(); !reflect.DeepEqual(got, restoreOrder) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, restoreOrder)
	}
}

// TestExitTasksEmergencyHangingDisconnectNoAllDone prüft die Kombination aus
// Hänger und fehlendem allDone (der Ernstfall bei toter Verbindung): disconnect
// hängt, allDone kommt nie. cleanup und persist liefen trotzdem, die Rückkehr
// kommt nach ca. deadline - und das Terminal wird auch dann noch ein zweites Mal
// zurückgesetzt, obwohl die Schritte das Budget schon aufgebraucht haben.
func TestExitTasksEmergencyHangingDisconnectNoAllDone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allDone func() <-chan struct{}
	}{
		{"allDone nil", func() <-chan struct{} { return nil }},
		{"allDone nie geschlossen", func() <-chan struct{} { return make(chan struct{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &exitRecorder{}
			t.Cleanup(registerEmergencyRestore(rec.step("restore")))
			release := make(chan struct{})
			defer close(release)
			tasks := rec.tasks()
			tasks.disconnect = func() { <-release }
			const limit = 200 * time.Millisecond
			start := time.Now()
			returned := make(chan struct{})
			go func() { defer close(returned); tasks.runEmergency(limit, tc.allDone()) }()
			if !waitChan(returned, 3*time.Second) {
				t.Fatal("runEmergency kehrte nicht zurück (hängt an disconnect)")
			}
			elapsed := time.Since(start)
			if elapsed < limit-30*time.Millisecond {
				t.Errorf("Rückkehr nach %v, früher als die deadline %v (disconnect hängt noch)", elapsed, limit)
			}
			if elapsed > limit+emergencyTolerance {
				t.Errorf("Rückkehr erst nach %v, deadline war %v", elapsed, limit)
			}
			want := []string{"restore", "apply", "cleanup", "persist", "restore"}
			if got := rec.list(); !reflect.DeepEqual(got, want) {
				t.Errorf("Reihenfolge = %v, erwartet %v (cleanup und persist trotz Hänger, restore zweimal)", got, want)
			}
		})
	}
}

// TestExitTasksEmergencySecondRestoreUsesStartSnapshot prüft Punkt 5 genau: Die
// SSH-Sitzung meldet ihre Funktion im Defer ab, sobald disconnect sie beendet.
// Hier meldet der cleanup-Schritt die Funktion ab (wie der Defer es täte). Das
// zweite Restore muss sie trotzdem rufen - es nimmt die Momentaufnahme vom
// ANFANG, nicht den Stand danach. Sonst bliebe der vom wieder eingeschalteten
// Raw-Modus veränderte Zustand unbehandelt. Erwartet: zweimal gerufen (einmal
// am Anfang, einmal am Ende), auch wenn sie dazwischen abgemeldet wurde.
func TestExitTasksEmergencySecondRestoreUsesStartSnapshot(t *testing.T) {
	rec := &exitRecorder{}
	unregister := registerEmergencyRestore(rec.step("restore"))
	t.Cleanup(unregister)
	tasks := rec.tasks()
	inner := rec.step("cleanup")
	tasks.cleanup = func() { unregister(); inner() }
	tasks.runEmergency(time.Second, nil)
	if rec.count("restore") != 2 {
		t.Errorf("restore lief %d mal, erwartet 2 (Momentaufnahme vom Anfang): %v", rec.count("restore"), rec.list())
	}
	if got := rec.list(); len(got) == 0 || got[len(got)-1] != "restore" {
		t.Errorf("Reihenfolge = %v, erwartet restore als Letztes", got)
	}
}

// TestExitTasksEmergencySecondRestorePanicIsHarmless prüft den Randfall "eine
// Wiederherstellung paniert" auch beim zweiten Mal: runEmergency paniert nicht,
// die zweite, harmlose Funktion läuft in beiden Durchgängen trotzdem.
func TestExitTasksEmergencySecondRestorePanicIsHarmless(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(func() { panic("Handle ungültig") }))
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	rec.tasks().runEmergency(time.Second, nil)
	if rec.count("restore") != 2 {
		t.Errorf("restore lief %d mal, erwartet 2 trotz panischer Nachbarfunktion", rec.count("restore"))
	}
	if rec.count("disconnect") != 1 {
		t.Errorf("Schritte = %v", rec.list())
	}
}

// TestWatchCloseSignalsEmergencyPassesAllDone prüft, dass der Watcher sein
// allDone an runEmergency weitergibt (nicht nil, nicht eine eigene deadline):
// quit hängt (tea.Exec), disconnect "beendet die Sitzung", und 50 ms später
// schließt main allDone. Der Watcher muss dann sofort exit(0) rufen - weit vor
// grace + emergencyStepsTimeout - und das Terminal nur EINMAL zurücksetzen.
func TestWatchCloseSignalsEmergencyPassesAllDone(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	release := make(chan struct{})
	defer close(release)
	const grace = 100 * time.Millisecond
	h := startWatcher(grace, func(h *watchHarness) { <-release }, func(h *watchHarness) *exitTasks {
		tasks := rec.tasks()
		record := rec.step("disconnect")
		tasks.disconnect = func() {
			record()
			go func() { time.Sleep(50 * time.Millisecond); close(h.allDone) }()
		}
		return tasks
	})
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit(0)")
	}
	// Mit weitergegebenem allDone: ca. grace + 50 ms. Ohne: grace + 750 ms.
	if elapsed := time.Since(h.started); elapsed > grace+emergencyStepsTimeout/2 {
		t.Errorf("exit nach %v, erwartet deutlich vor grace + %v (allDone nicht an runEmergency weitergegeben?)", elapsed, emergencyStepsTimeout)
	}
	if rec.count("restore") != 1 {
		t.Errorf("restore lief %d mal, erwartet 1 (allDone kam rechtzeitig): %v", rec.count("restore"), rec.list())
	}
	if !waitChan(h.returned, 2*time.Second) {
		t.Error("Watcher kehrte nach dem Notfall nicht zurück")
	}
}

// TestWatchCloseSignalsEmergencyWaitsFullBudget prüft das Gegenstück: Kommt
// allDone nie, wartet der Watcher im Notfall das ganze Limit
// emergencyStepsTimeout ab (die Schritte sind sofort fertig, trotzdem wird
// gewartet, damit Bubbletea Zeit zum Aufräumen hat), setzt das Terminal danach
// ein zweites Mal zurück und ruft dann exit(0). Gesamtzeit grace + Limit,
// nicht mehr (Windows beendet nach ca. 5 s).
func TestWatchCloseSignalsEmergencyWaitsFullBudget(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	const grace = 100 * time.Millisecond
	h := startWatcher(grace, nil, func(h *watchHarness) *exitTasks { return rec.tasks() })
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit(0)")
	}
	elapsed := time.Since(h.started)
	if elapsed < grace+emergencyStepsTimeout-30*time.Millisecond {
		t.Errorf("exit nach %v, erwartet nicht vor grace + %v (es muss auf allDone gewartet werden)", elapsed, emergencyStepsTimeout)
	}
	if elapsed > grace+emergencyStepsTimeout+emergencyTolerance {
		t.Errorf("exit nach %v, erlaubt %v", elapsed, grace+emergencyStepsTimeout+emergencyTolerance)
	}
	if got := rec.list(); !reflect.DeepEqual(got, restoreOrder) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, restoreOrder)
	}
}

// TestWatchCloseSignalsOnSignalBeforeFirstLogEntry prüft B (Build 89): onSignal
// läuft als ALLERERSTES nach dem Empfang des Signals, noch VOR dem ersten
// Logeintrag. Grund: Das Schreiben ins Log kann auf einem langsamen oder
// blockierten Laufwerk hängen; in dieser Zeit wäre closeRequested noch nicht
// gesetzt, und main würde ein sofort zurückkehrendes Run() als Fehler werten
// (Exit-Code 1). Gemessen wird deterministisch (beide Aufrufe laufen nacheinander
// in der Watcher-Goroutine): onSignal schaut in die echte Logdatei, die zu diesem
// Zeitpunkt noch leer oder nicht vorhanden sein muss. Gegenprobe: Danach steht
// der Logeintrag zum Signal wirklich in der Datei (sonst wäre der Test leer).
func TestWatchCloseSignalsOnSignalBeforeFirstLogEntry(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "watch.log")
	logger := &Logger{enabled: true, filePath: logPath}
	var called bool
	var sizeAtSignal int64
	onSignal := func() {
		called = true
		if info, err := os.Stat(logPath); err == nil {
			sizeAtSignal = info.Size()
		}
	}
	sigs := make(chan os.Signal, 1)
	allDone := make(chan struct{})
	quit := func() { close(allDone) }
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		watchCloseSignals(sigs, onSignal, quit, allDone, nil, time.Second, func(int) {}, logger)
	}()
	sigs <- syscall.SIGHUP
	if !waitChan(returned, 3*time.Second) {
		t.Fatal("Watcher kehrte nicht zurück")
	}
	// returned wurde nach dem Watcher geschlossen: Lesen von called/sizeAtSignal ist sicher
	if !called {
		t.Fatal("onSignal wurde nicht aufgerufen")
	}
	if sizeAtSignal != 0 {
		t.Errorf("Logdatei hatte bei onSignal schon %d Bytes: onSignal läuft erst NACH dem ersten log.Info", sizeAtSignal)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Logdatei fehlt: %v (der Logeintrag zum Signal wurde nie geschrieben)", err)
	}
	if !strings.Contains(string(data), "Signal") {
		t.Errorf("Logdatei enthält keinen Eintrag zum Signal: %q", data)
	}
}

// =============================================================================
// Build 89, Runde 2 (Fable-Review, Fund 1): zweites Restore als LETZTER Schritt
// =============================================================================
//
// Festlegungen (vom Lead offen gelassen, hier verbindlich für specialist-windows):
//
//	var emergencyTerminalReset func(seq string)
//	    Funktionszeiger in src/exit_signals.go (auf ALLEN Plattformen deklariert,
//	    damit die Tests auch unter Windows übersetzen). Wird beim ZWEITEN Restore
//	    (allDone kam nicht rechtzeitig) EINMAL mit der ganzen Folge
//	    emergencyResetSequence (= resetSequence unten) aufgerufen: Bracketed
//	    Paste aus, Alt-Screen verlassen, Maus-Modi 1000/1002/1003/1006 aus
//	    (Runde 3), Cursor zeigen als Letztes. Der Standard schreibt auf das
//	    Terminal (stdout). Unter Windows wird er nie aufgerufen.
//	    runEmergency ruft ihn in einer eigenen Goroutine mit EIGENEM Zeitlimit
//	    (höchstens emergencyResetWriteMax = 500 ms, empfohlen 200 bis 250 ms) auf:
//	    ein blockierender Write hält Rückkehr und exit nicht länger auf.
//	    Reihenfolge von Restore und Write untereinander ist nicht festgelegt;
//	    beide sind die letzten Ereignisse vor exit(0).
//	Der Logeintrag "ssh-easy beendet (Notfall-Ablauf)" steht VOR dem zweiten
//	Restore; zwischen dem zweiten Restore (und dem Write) und exit(0) wird nichts
//	mehr geloggt (das Schreiben ins Log kann hängen).

const (
	// resetSequence ist die erwartete Folge für das Terminal: Bracketed Paste
	// aus, Alt-Screen verlassen, Maus-Modi aus (Klick, Ziehen, alle Bewegungen,
	// SGR-Format), Cursor zeigen
	resetSequence = "\x1b[?2004l\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h"
	// emergencyResetWriteMax - größte Zusatzzeit durch einen hängenden Write
	emergencyResetWriteMax = 500 * time.Millisecond
	// emergencyDoneLine - Logeintrag am Ende des Notfall-Ablaufs
	emergencyDoneLine = "ssh-easy beendet (Notfall-Ablauf)"
)

// resetWrites zeichnet die Aufrufe des Funktionszeigers emergencyTerminalReset auf.
type resetWrites struct {
	mu    sync.Mutex
	calls []string
}

// list liefert die übergebenen Folgen.
func (w *resetWrites) list() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

// resetHandler ist der gerade aktive Test-Handler für emergencyTerminalReset.
// Der Funktionszeiger selbst wird nur EINMAL in init() gesetzt und ruft diesen
// Handler auf: So gibt es kein Rennen zwischen dem Umschalten im Test und einer
// Goroutine des Notfall-Ablaufs, die nach der Rückkehr noch liest (ein
// hängender Write läuft nach Ablauf des Zeitlimits weiter).
var (
	resetHandlerMu sync.Mutex
	resetHandler   func(seq string)
)

// setResetHandler setzt den Handler für die Testdauer (danach wieder keiner).
func setResetHandler(t *testing.T, fn func(seq string)) {
	t.Helper()
	resetHandlerMu.Lock()
	resetHandler = fn
	resetHandlerMu.Unlock()
	t.Cleanup(func() {
		resetHandlerMu.Lock()
		resetHandler = nil
		resetHandlerMu.Unlock()
	})
}

// stubTerminalReset lässt jeden Aufruf von emergencyTerminalReset aufzeichnen, in
// rec (falls nicht nil) als Ereignis "write" vermerken und blockieren, solange
// block (falls nicht nil) offen ist.
func stubTerminalReset(t *testing.T, rec *exitRecorder, block <-chan struct{}) *resetWrites {
	t.Helper()
	w := &resetWrites{}
	setResetHandler(t, func(seq string) {
		w.mu.Lock()
		w.calls = append(w.calls, seq)
		w.mu.Unlock()
		if rec != nil {
			rec.step("write")()
		}
		if block != nil {
			<-block
		}
	})
	return w
}

// init setzt den Funktionszeiger einmalig auf den Verteiler zum aktuellen
// Handler. Ohne Handler tut er nichts: Sonst schrieben die Notfall-Tests die
// Terminalfolge in die Testausgabe.
func init() {
	emergencyTerminalReset = func(seq string) {
		resetHandlerMu.Lock()
		fn := resetHandler
		resetHandlerMu.Unlock()
		if fn != nil {
			fn(seq)
		}
	}
}

// wantResetCalls liefert die erwartete Zahl der Aufrufe: unter Windows nie,
// sonst genau einen (wenn das zweite Restore läuft).
func wantResetCalls() int {
	if runtime.GOOS == "windows" {
		return 0
	}
	return 1
}

// TestExitTasksEmergencyWritesTerminalResetSequence prüft Punkt 1 (Unix): Beim
// zweiten Restore (allDone kam nicht) wird die Folge "Bracketed Paste aus,
// Alt-Screen verlassen, Cursor zeigen" genau EINMAL über den Funktionszeiger
// geschrieben - als Teil des letzten Schritts, nach allen Notfall-Schritten.
// Unter Windows wird nichts geschrieben. Randfall: Die Folge steht in dieser
// Reihenfolge (erst Paste aus, dann Alt-Screen, zuletzt Cursor), sonst bliebe
// der Cursor nach dem Verlassen des Alt-Screens unsichtbar.
func TestExitTasksEmergencyWritesTerminalResetSequence(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	w := stubTerminalReset(t, rec, nil)
	rec.tasks().runEmergency(time.Second, nil)

	calls := w.list()
	if len(calls) != wantResetCalls() {
		t.Fatalf("Aufrufe der Terminalfolge = %d (%q), erwartet %d", len(calls), calls, wantResetCalls())
	}
	events := rec.list()
	if runtime.GOOS == "windows" {
		if !reflect.DeepEqual(events, restoreOrder) {
			t.Errorf("Reihenfolge = %v, erwartet %v", events, restoreOrder)
		}
		return
	}
	if calls[0] != resetSequence {
		t.Errorf("Folge = %q, erwartet %q", calls[0], resetSequence)
	}
	assertResetSequenceOrder(t, calls[0])
	// Erst alle Schritte, dann (in beliebiger Reihenfolge) zweites Restore und Write
	if len(events) != 7 || !reflect.DeepEqual(events[:5], restoreOrder[:5]) {
		t.Fatalf("Ereignisse = %v, erwartet die fünf Schritte %v und danach restore und write", events, restoreOrder[:5])
	}
	last := []string{events[5], events[6]}
	if !(reflect.DeepEqual(last, []string{"restore", "write"}) || reflect.DeepEqual(last, []string{"write", "restore"})) {
		t.Errorf("letzte Ereignisse = %v, erwartet restore und write", last)
	}
}

// assertResetSequenceOrder prüft Inhalt und Reihenfolge der Folge: jeder Teil
// genau einmal; Bracketed Paste aus, Alt-Screen verlassen und alle Maus-Modi
// aus stehen VOR "Cursor zeigen", und "Cursor zeigen" ist das Letzte (sonst
// bliebe der Cursor nach dem Verlassen des Alt-Screens unsichtbar). Alt-Screen
// vor den Maus-Modi ist zulässig, ihre Reihenfolge untereinander ist egal.
func assertResetSequenceOrder(t *testing.T, seq string) {
	t.Helper()
	parts := map[string]string{
		"Bracketed Paste aus":        "\x1b[?2004l",
		"Alt-Screen verlassen":       "\x1b[?1049l",
		"Maus Klick aus (1000)":      "\x1b[?1000l",
		"Maus Ziehen aus (1002)":     "\x1b[?1002l",
		"Maus Bewegung aus (1003)":   "\x1b[?1003l",
		"Maus SGR-Format aus (1006)": "\x1b[?1006l",
	}
	const cursorOn = "\x1b[?25h"
	if strings.Count(seq, cursorOn) != 1 || !strings.HasSuffix(seq, cursorOn) {
		t.Errorf("Folge %q: \"Cursor zeigen\" muss genau einmal und ganz am Ende stehen", seq)
	}
	cursorAt := strings.Index(seq, cursorOn)
	for name, part := range parts {
		at := strings.Index(seq, part)
		switch {
		case at < 0:
			t.Errorf("Folge %q: %s (%q) fehlt", seq, name, part)
		case strings.Count(seq, part) != 1:
			t.Errorf("Folge %q: %s kommt mehrfach vor", seq, name)
		case cursorAt >= 0 && at > cursorAt:
			t.Errorf("Folge %q: %s steht hinter \"Cursor zeigen\"", seq, name)
		}
	}
}

// TestEmergencyResetSequenceConstant prüft die Folge des Produktivcodes selbst
// (emergencyResetSequence), unabhängig vom Ablauf: Inhalt, Reihenfolge und die
// Maus-Modi. Ohne das Abschalten der Maus-Modi meldete ein Terminal nach dem
// Notfall weiter Mausereignisse als Zeichenmüll in die Shell.
func TestEmergencyResetSequenceConstant(t *testing.T) {
	if emergencyResetSequence != resetSequence {
		t.Errorf("emergencyResetSequence = %q, erwartet %q", emergencyResetSequence, resetSequence)
	}
	assertResetSequenceOrder(t, emergencyResetSequence)
}

// TestExitTasksEmergencyNoResetSequenceWhenAllDoneInTime: Kam allDone rechtzeitig
// (vorher geschlossen, oder durch disconnect ausgelöst), hat Bubbletea das
// Terminal selbst wiederhergestellt: kein zweites Restore und KEINE Folge - sie
// würde Bubbleteas Zustand (z. B. den Alt-Screen eines neuen Programms) zerstören.
func TestExitTasksEmergencyNoResetSequenceWhenAllDoneInTime(t *testing.T) {
	t.Run("vorher geschlossen", func(t *testing.T) {
		rec := &exitRecorder{}
		w := stubTerminalReset(t, rec, nil)
		allDone := make(chan struct{})
		close(allDone)
		rec.tasks().runEmergency(time.Second, allDone)
		if got := w.list(); len(got) != 0 {
			t.Errorf("Folge geschrieben, obwohl allDone rechtzeitig kam: %q", got)
		}
	})
	t.Run("von disconnect ausgelöst", func(t *testing.T) {
		rec := &exitRecorder{}
		w := stubTerminalReset(t, rec, nil)
		allDone := make(chan struct{})
		tasks := rec.tasks()
		inner := rec.step("disconnect")
		tasks.disconnect = func() {
			inner()
			go func() { time.Sleep(30 * time.Millisecond); close(allDone) }()
		}
		tasks.runEmergency(2*time.Second, allDone)
		if got := w.list(); len(got) != 0 {
			t.Errorf("Folge geschrieben, obwohl allDone rechtzeitig kam: %q", got)
		}
	})
}

// TestExitTasksEmergencyResetSequenceWithoutAllDone: Die Folge kommt auch dann,
// wenn gar kein allDone da ist (nil), allDone nie geschlossen wird oder disconnect
// hängt - jeweils genau einmal (Unix) bzw. nie (Windows).
func TestExitTasksEmergencyResetSequenceWithoutAllDone(t *testing.T) {
	cases := []struct {
		name    string
		allDone func() <-chan struct{}
		hang    bool
	}{
		{"nil", func() <-chan struct{} { return nil }, false},
		{"nie geschlossen", func() <-chan struct{} { return make(chan struct{}) }, false},
		{"disconnect hängt", func() <-chan struct{} { return make(chan struct{}) }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &exitRecorder{}
			w := stubTerminalReset(t, rec, nil)
			release := make(chan struct{})
			defer close(release)
			tasks := rec.tasks()
			if c.hang {
				tasks.disconnect = func() { <-release }
			}
			tasks.runEmergency(150*time.Millisecond, c.allDone())
			if got := w.list(); len(got) != wantResetCalls() {
				t.Errorf("Aufrufe = %d (%q), erwartet %d", len(got), got, wantResetCalls())
			}
		})
	}
}

// TestExitTasksEmergencyBlockingResetWriteDoesNotHoldReturn: Der Write auf ein totes
// Terminal kann ewig blockieren (SIGHUP: die Terminal-Gegenstelle ist weg). Er hat
// ein eigenes Zeitlimit: runEmergency kehrt trotzdem zurück - höchstens
// emergencyResetWriteMax nach der deadline, bei schnellen Schritten ohne allDone
// sogar nach dieser Zeit statt nach der deadline. Das zweite Restore lief trotzdem.
func TestExitTasksEmergencyBlockingResetWriteDoesNotHoldReturn(t *testing.T) {
	cases := []struct {
		name    string
		limit   time.Duration
		allDone func() <-chan struct{}
		// maxElapsed - späteste Rückkehr
		maxElapsed time.Duration
	}{
		{"nil, schnelle Schritte", 5 * time.Second, func() <-chan struct{} { return nil }, emergencyResetWriteMax + emergencyTolerance},
		{"allDone offen, deadline 200 ms", 200 * time.Millisecond, func() <-chan struct{} { return make(chan struct{}) }, 200*time.Millisecond + emergencyResetWriteMax + emergencyTolerance},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &exitRecorder{}
			t.Cleanup(registerEmergencyRestore(rec.step("restore")))
			block := make(chan struct{})
			t.Cleanup(func() { close(block) })
			w := stubTerminalReset(t, rec, block)
			returned := make(chan struct{})
			start := time.Now()
			go func() { defer close(returned); rec.tasks().runEmergency(c.limit, c.allDone()) }()
			if !waitChan(returned, 5*time.Second) {
				t.Fatal("runEmergency kehrte nicht zurück (hängender Write hält auf)")
			}
			if elapsed := time.Since(start); elapsed > c.maxElapsed {
				t.Errorf("Rückkehr nach %v, erlaubt %v", elapsed, c.maxElapsed)
			}
			if rec.count("restore") != 2 {
				t.Errorf("restore lief %d mal, erwartet 2: %v", rec.count("restore"), rec.list())
			}
			if len(w.list()) != wantResetCalls() {
				t.Errorf("Aufrufe der Folge = %d, erwartet %d", len(w.list()), wantResetCalls())
			}
		})
	}
}

// TestEmergencyTotalBudgetWithResetWrite: Auch mit dem ECHTEN Zeitlimit des Writes
// (emergencyResetWriteTimeout aus dem Produktivcode, nicht der testeigenen
// Obergrenze emergencyResetWriteMax) bleibt die Gesamtzeit unter 3 s; das echte
// Limit darf die Obergrenze der Tests nicht überschreiten (Windows beendet nach ca. 5 s hart; unter
// Windows wird nichts geschrieben, die Rechnung gilt dort trotzdem).
func TestEmergencyTotalBudgetWithResetWrite(t *testing.T) {
	if emergencyResetWriteTimeout <= 0 || emergencyResetWriteTimeout > emergencyResetWriteMax {
		t.Errorf("emergencyResetWriteTimeout = %v, erwartet größer 0 und höchstens %v", emergencyResetWriteTimeout, emergencyResetWriteMax)
	}
	if total := closeGracePeriod + emergencyStepsTimeout + emergencyResetWriteTimeout; total >= 3*time.Second {
		t.Errorf("Frist %v + Schritte %v + Write-Limit %v = %v >= 3 s", closeGracePeriod, emergencyStepsTimeout, emergencyResetWriteTimeout, total)
	}
}

// TestWatchCloseSignalsBlockingResetWriteStillExits: Im Watcher mit hängendem Write
// und hängendem disconnect kommt exit(0) trotzdem rechtzeitig - spätestens nach
// grace + emergencyStepsTimeout + emergencyResetWriteMax (plus Toleranz), also
// deutlich unter Windows' 5-Sekunden-Grenze.
func TestWatchCloseSignalsBlockingResetWriteStillExits(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stubTerminalReset(t, nil, block)
	const grace = 100 * time.Millisecond
	h := startWatcher(grace, func(h *watchHarness) { <-block }, func(h *watchHarness) *exitTasks {
		tasks := h.rec.tasks()
		tasks.disconnect = func() { <-block }
		return tasks
	})
	h.sigs <- syscall.SIGHUP
	select {
	case code := <-h.exitCodes:
		if code != 0 {
			t.Errorf("exit-Code = %d, erwartet 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kein exit(0) bei hängendem Write")
	}
	if elapsed, max := time.Since(h.started), grace+emergencyStepsTimeout+emergencyResetWriteMax+emergencyTolerance; elapsed > max {
		t.Errorf("exit nach %v, erlaubt %v", elapsed, max)
	}
}

// TestWatchCloseSignalsRestoreIsLastBeforeExit prüft Punkt 1 (Reihenfolge): Das
// zweite Restore (bei Unix zusammen mit der Terminalfolge) ist der LETZTE
// Schritt vor exit(0). Der Logeintrag "ssh-easy beendet (Notfall-Ablauf)" steht
// VOR dem zweiten Restore, und zwischen Restore, Write und exit(0) kommt kein
// Logeintrag mehr: Das Schreiben ins Log kann auf einem langsamen oder toten
// Laufwerk hängen, und das Terminal bliebe in dieser Zeit im Raw-Modus.
// Gemessen wird mit einer echten Logdatei: An jedem der letzten Ereignisse wird
// die Größe der Datei festgehalten. Es dürfen mehrere Ereignisse zusammen am
// Ende stehen (Restore, Write); alle sehen den Logeintrag schon und dieselbe Größe.
func TestWatchCloseSignalsRestoreIsLastBeforeExit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "watch.log")
	logger := &Logger{enabled: true, filePath: logPath}
	logState := func() (string, int) {
		data, _ := os.ReadFile(logPath)
		return string(data), len(data)
	}

	type event struct {
		name    string
		hasDone bool
		size    int
	}
	var mu sync.Mutex
	var events []event
	record := func(name string) {
		text, size := logState()
		mu.Lock()
		events = append(events, event{name, strings.Contains(text, emergencyDoneLine), size})
		mu.Unlock()
	}
	t.Cleanup(registerEmergencyRestore(func() { record("restore") }))
	setResetHandler(t, func(string) { record("write") })

	rec := &exitRecorder{}
	exited := make(chan struct{})
	exit := func(int) { record("exit"); close(exited) }
	sigs := make(chan os.Signal, 1)
	allDone := make(chan struct{}) // bleibt offen: Notfall-Ablauf mit zweitem Restore
	go watchCloseSignals(sigs, nil, nil, allDone, rec.tasks(), 80*time.Millisecond, exit, logger)
	sigs <- syscall.SIGHUP
	if !waitChan(exited, 5*time.Second) {
		t.Fatal("kein exit im Notfall")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || events[len(events)-1].name != "exit" {
		t.Fatalf("Ereignisse = %+v, erwartet exit als letztes", events)
	}
	exitEv := events[len(events)-1]
	restores, writes, lastRestore := 0, 0, -1
	for i, ev := range events {
		switch ev.name {
		case "restore":
			restores++
			lastRestore = i
		case "write":
			writes++
		}
	}
	if restores != 2 {
		t.Errorf("restore lief %d mal, erwartet 2: %+v", restores, events)
	}
	if writes != wantResetCalls() {
		t.Errorf("Terminalfolge %d mal geschrieben, erwartet %d: %+v", writes, wantResetCalls(), events)
	}
	if lastRestore < 0 {
		t.Fatalf("kein Restore: %+v", events)
	}
	// Das zweite Restore sieht den Logeintrag schon (er steht davor) ...
	if !events[lastRestore].hasDone {
		t.Errorf("zweites Restore: Logeintrag %q fehlt noch, er muss VOR dem Restore stehen: %+v", emergencyDoneLine, events)
	}
	// ... und nach ihm (bis exit) steht nur noch der Write, es wird nichts mehr geloggt
	for _, ev := range events[lastRestore+1 : len(events)-1] {
		if ev.name != "write" {
			t.Errorf("zwischen zweitem Restore und exit(0) steht %q: %+v", ev.name, events)
		}
		if !ev.hasDone {
			t.Errorf("Write ohne Logeintrag davor: %+v", events)
		}
		if ev.size != exitEv.size {
			t.Errorf("Write sah %d Bytes im Log, bei exit sind es %d: dazwischen wurde geloggt", ev.size, exitEv.size)
		}
	}
	if events[lastRestore].size != exitEv.size {
		t.Errorf("zweites Restore sah %d Bytes im Log, bei exit sind es %d: zwischen Restore und exit(0) wurde geloggt: %+v", events[lastRestore].size, exitEv.size, events)
	}
	// Unter Unix gehört der Write zum letzten Schritt: er darf nicht VOR dem
	// Logeintrag stehen
	for _, ev := range events {
		if ev.name == "write" && !ev.hasDone {
			t.Errorf("Write vor dem Logeintrag %q: %+v", emergencyDoneLine, events)
		}
	}
}
