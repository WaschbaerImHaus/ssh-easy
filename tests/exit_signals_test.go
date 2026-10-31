// Paket main - Tests für das Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87)
//
// Hintergrund: Wer das Fenster schließt, bekommt unter Linux SIGHUP und unter
// Windows CTRL_CLOSE_EVENT (in Go: SIGTERM). Früher lief dann der Code nach
// p.Run() nie, und ein heruntergeladenes Update wurde nicht installiert.
// Spezifikation: Scratchpad spec_exit.md (Abschnitte A bis D).
//
// FESTGELEGTE SIGNATUREN (src/exit_signals.go, die Specialists übernehmen sie):
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
//	func (t *exitTasks) runEmergency(persistTimeout time.Duration)
//	    Reihenfolge: disconnect, apply, cleanup, persist; persist läuft in eigener
//	    Goroutine und wird nach persistTimeout aufgegeben (Rückkehr trotzdem).
//	    Jeder Schritt läuft über alle Aufrufe beider Methoden genau einmal
//	    (je Schritt ein sync.Once; die zweite Methode wartet, bis der Schritt
//	    fertig ist, oder überspringt ihn - er läuft aber nie doppelt).
//
//	const closeGracePeriod = 1500 * time.Millisecond        // Wartezeit auf normales Ende
//	const emergencyPersistTimeout = 750 * time.Millisecond   // Zeitlimit für persist im Notfall
//	    Beide zusammen müssen unter 3 s liegen (Windows beendet nach ca. 5 s).
//
//	func watchCloseSignals(sigs <-chan os.Signal, quit func(), allDone <-chan struct{},
//	    tasks *exitTasks, grace time.Duration, exit func(int), log *Logger)
//	    BLOCKIEREND (main startet sie mit "go"). Kehrt zurück, wenn
//	    (a) allDone geschlossen wird, bevor ein Signal kam, oder
//	    (b) das erste Signal behandelt ist: "go quit()", dann auf allDone bis grace
//	        warten; kommt allDone rechtzeitig -> zurück ohne Notfall und ohne exit;
//	        sonst tasks.runEmergency(emergencyPersistTimeout), exit(0), zurück.
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
// @author Kurt Ingwer
// @date   2026-10-31 02:20
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
	h.started = time.Now()
	go func() {
		defer close(h.returned)
		watchCloseSignals(h.sigs, quit, h.allDone, h.tasks, grace, exit, &Logger{})
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
	tasks.runEmergency(100 * time.Millisecond)
	if got := rec.list(); len(got) != 4 {
		t.Errorf("Schritte = %v, erwartet je einen (4)", got)
	}
}

// TestExitTasksRunEmergencyOrder prüft den Notfall: das Update kommt VOR dem
// Aufräumen und dem Speichern, weil Windows nach rund 5 s hart beendet.
func TestExitTasksRunEmergencyOrder(t *testing.T) {
	rec := &exitRecorder{}
	rec.tasks().runEmergency(time.Second)
	want := []string{"disconnect", "apply", "cleanup", "persist"}
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
	tasks.runEmergency(limit)
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
	(&exitTasks{apply: rec.step("apply2")}).runEmergency(50 * time.Millisecond)
	(&exitTasks{}).runNormal()
	(&exitTasks{}).runEmergency(50 * time.Millisecond)
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
		go func() { defer wg.Done(); <-start; tasks.runEmergency(200 * time.Millisecond) }()
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
// - nicht früher - laufen die Aufgaben im Notfall-Ablauf, dann exit(0).
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
	want := []string{"disconnect", "apply", "cleanup", "persist"}
	if got := h.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Schritte = %v, erwartet Notfall-Reihenfolge %v", got, want)
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
	if closeGracePeriod+emergencyPersistTimeout >= 3*time.Second {
		t.Errorf("Frist %v + persist-Limit %v >= 3 s", closeGracePeriod, emergencyPersistTimeout)
	}
	if closeGracePeriod <= 0 || emergencyPersistTimeout <= 0 {
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
	if elapsed := time.Since(h.started); elapsed > grace+emergencyPersistTimeout+time.Second {
		t.Errorf("Notfall dauerte %v, erlaubt %v", elapsed, grace+emergencyPersistTimeout)
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
