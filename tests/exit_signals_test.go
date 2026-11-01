// Paket main - Tests für das Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87, ergänzt in Build 88)
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
//	const emergencyStepsTimeout = 750 * time.Millisecond   // Zeitlimit für die Notfall-Schritte (ab Build 88; früher emergencyPersistTimeout)
//	    Beide zusammen müssen unter 3 s liegen (Windows beendet nach ca. 5 s).
//
//	func watchCloseSignals(sigs <-chan os.Signal, quit func(), allDone <-chan struct{},
//	    tasks *exitTasks, grace time.Duration, exit func(int), log *Logger)
//	    BLOCKIEREND (main startet sie mit "go"). Kehrt zurück, wenn
//	    (a) allDone geschlossen wird, bevor ein Signal kam, oder
//	    (b) das erste Signal behandelt ist: "go quit()", dann auf allDone bis grace
//	        warten; kommt allDone rechtzeitig -> zurück ohne Notfall und ohne exit;
//	        sonst tasks.runEmergency(emergencyStepsTimeout), exit(0), zurück.
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
// ÄNDERUNGEN BUILD 88 (Spezifikation spec88.md, Abschnitte N1, N4, N5):
//
//	func (t *exitTasks) runEmergency(deadline time.Duration)
//	    Reihenfolge jetzt: (N5) runEmergencyRestore(), dann apply (synchron), danach
//	    disconnect, cleanup, persist in EINER Goroutine; gewartet wird höchstens
//	    deadline (gilt für alle drei zusammen). Hängende Schritte werden aufgegeben.
//	const emergencyStepsTimeout = 750 * time.Millisecond   // früher emergencyPersistTimeout
//	func watchCloseSignals(sigs <-chan os.Signal, onSignal func(), quit func(),
//	    allDone <-chan struct{}, tasks *exitTasks, grace time.Duration,
//	    exit func(int), log *Logger)
//	    onSignal (nil erlaubt) läuft beim ersten Signal SYNCHRON, BEVOR "go quit()" startet.
//	func registerEmergencyRestore(fn func()) (unregister func())
//	func runEmergencyRestore()
//	    Registrierte Funktionen (Terminal wiederherstellen) werden von
//	    runEmergencyRestore aufgerufen; Panik in einer stoppt die anderen nicht;
//	    nebenläufigkeitssicher; unregister darf mehrfach gerufen werden.
//
// VON MIR FESTGELEGT (Spezifikation ließ es offen):
//   - Die Tests prüfen nicht, in welcher Reihenfolge mehrere Restore-Funktionen
//     laufen und ob eine Funktion bei zwei Aufrufen von runEmergencyRestore
//     zweimal läuft; sie rufen runEmergencyRestore je Fall genau einmal.
//   - Die Quelltext-Tests lesen main.go und ssh_terminal.go (Tests laufen als
//     Kopie in src/).
//
// @author Kurt Ingwer
// @date   2026-11-01 10:00
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
	tasks.runEmergency(100 * time.Millisecond)
	if got := rec.list(); len(got) != 4 {
		t.Errorf("Schritte = %v, erwartet je einen (4)", got)
	}
}

// TestExitTasksRunEmergencyOrder prüft den Notfall (Build 88, N1): das Update
// kommt ALS ERSTES, noch vor dem Trennen der Verbindungen - ein hängendes
// disconnect darf das Update nicht verhindern, weil Windows nach rund 5 s hart
// beendet. Danach folgen disconnect, cleanup, persist in dieser Reihenfolge.
func TestExitTasksRunEmergencyOrder(t *testing.T) {
	rec := &exitRecorder{}
	rec.tasks().runEmergency(time.Second)
	want := []string{"apply", "disconnect", "cleanup", "persist"}
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
// - nicht früher - laufen die Aufgaben im Notfall-Ablauf (apply zuerst), dann
// exit(0).
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
	want := []string{"apply", "disconnect", "cleanup", "persist"}
	if got := h.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Schritte = %v, erwartet Notfall-Reihenfolge %v (apply zuerst)", got, want)
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

// TestExitTasksEmergencyDisconnectHangs prüft den Kernfall von N1: disconnect
// hängt (z.B. tote Verbindung ohne Antwort). Früher stand disconnect VOR apply
// und blockierte das Update. Jetzt muss apply trotzdem gelaufen sein, und
// runEmergency kehrt nach etwa deadline zurück statt zu hängen. Nach dem
// Freigeben läuft der Rest (cleanup, persist) in der abgegebenen Goroutine zu
// Ende - jeder Schritt genau einmal.
func TestExitTasksEmergencyDisconnectHangs(t *testing.T) {
	rec := &exitRecorder{}
	release := make(chan struct{})
	tasks := rec.tasks()
	tasks.disconnect = func() { <-release }

	const limit = 150 * time.Millisecond
	start := time.Now()
	returned := make(chan struct{})
	go func() { defer close(returned); tasks.runEmergency(limit) }()
	if !waitChan(returned, 3*time.Second) {
		close(release)
		t.Fatal("runEmergency kehrte nicht zurück (hängt an disconnect)")
	}
	elapsed := time.Since(start)
	if elapsed < limit-20*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als das Zeitlimit %v", elapsed, limit)
	}
	if elapsed > limit+700*time.Millisecond {
		t.Errorf("Rückkehr erst nach %v, Zeitlimit war %v (hängt an disconnect)", elapsed, limit)
	}
	if rec.count("apply") != 1 {
		t.Errorf("apply lief %d mal, erwartet 1 (muss VOR dem hängenden disconnect laufen)", rec.count("apply"))
	}
	close(release)
	if !waitCount(rec, "persist", 1, 2*time.Second) {
		t.Fatalf("nach dem Freigeben lief persist nicht: %v", rec.list())
	}
	want := []string{"apply", "cleanup", "persist"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Schritte = %v, erwartet %v", got, want)
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
	tasks.runEmergency(limit)
	elapsed := time.Since(start)
	if elapsed < limit-30*time.Millisecond {
		t.Errorf("Rückkehr nach %v, früher als das Zeitlimit %v", elapsed, limit)
	}
	if elapsed > limit+250*time.Millisecond {
		t.Errorf("Rückkehr nach %v, das gemeinsame Zeitlimit %v wurde überschritten", elapsed, limit)
	}
	// Aufräumen: die abgegebene Goroutine läuft zu Ende (keine Zusatzprüfung nötig)
	waitCount(rec, "persist", 1, 2*time.Second)
}

// TestExitTasksEmergencyFastStepsDoNotWait prüft den Normalfall: Laufen alle
// Schritte schnell durch, kehrt runEmergency sofort zurück (nicht erst nach
// dem Zeitlimit) und alle vier Schritte sind fertig.
func TestExitTasksEmergencyFastStepsDoNotWait(t *testing.T) {
	rec := &exitRecorder{}
	start := time.Now()
	rec.tasks().runEmergency(5 * time.Second)
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

// TestRunEmergencyRestoresTerminalFirst prüft N5: runEmergency stellt das
// Terminal ALS ALLERERSTES wieder her, noch vor apply (nur term.Restore, ms) -
// auch dann, wenn die Wiederherstellung paniert, laufen die Schritte danach.
func TestRunEmergencyRestoresTerminalFirst(t *testing.T) {
	rec := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(rec.step("restore")))
	rec.tasks().runEmergency(time.Second)
	want := []string{"restore", "apply", "disconnect", "cleanup", "persist"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, want)
	}

	rec2 := &exitRecorder{}
	t.Cleanup(registerEmergencyRestore(func() { panic("Handle ungültig") }))
	rec2.tasks().runEmergency(time.Second)
	if rec2.count("apply") != 1 || rec2.count("persist") != 1 {
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
	if got := rec.list(); len(got) == 0 || got[0] != "restore" {
		t.Errorf("Reihenfolge = %v, erwartet restore zuerst", got)
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

// TestSSHTerminalRegistersEmergencyRestore prüft den Quelltext von
// ssh_terminal.go: Nach term.MakeRaw wird die Wiederherstellung mit
// registerEmergencyRestore angemeldet (term.Restore mit dem alten Zustand) und
// beim normalen Ende wieder abgemeldet. Die Sitzung selbst lässt sich ohne
// echtes Terminal nicht starten.
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
	reg := strings.Index(rest, "registerEmergencyRestore(")
	if reg < 0 {
		t.Fatal("registerEmergencyRestore( fehlt hinter term.MakeRaw in ssh_terminal.go")
	}
	after := rest[reg:]
	if !strings.Contains(after, "term.Restore(stdinFd") {
		t.Error("term.Restore(stdinFd, ...) fehlt hinter registerEmergencyRestore")
	}
	if !strings.Contains(after, "unregister()") {
		t.Error("unregister() fehlt: die Wiederherstellung wird am Ende nicht abgemeldet")
	}
}
