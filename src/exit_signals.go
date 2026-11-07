// Paket main - Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87, ergänzt in Build 88 und 89)
//
// Wer das Fenster schließt, bekommt unter Linux SIGHUP (Terminal weg) und
// unter Windows CTRL_CLOSE_EVENT (in Go: SIGTERM). Bis Build 86 lief dann
// der Code nach p.Run() nie: Verbindungen blieben offen, und ein
// heruntergeladenes Update wurde nicht installiert.
//
// Ablauf:
//   - main registriert closeSignals() und startet watchCloseSignals.
//   - Beim ersten Signal ruft der Watcher onSignal SYNCHRON auf (main setzt
//     dort closeRequested) und bittet erst danach Bubbletea um ein normales
//     Ende (quit). Endet das Programm innerhalb von closeGracePeriod,
//     erledigt main die Aufgaben wie bei q (finishRun -> runNormal).
//   - Sonst (z. B. Bubbletea verwirft das Signal während tea.Exec) erledigt
//     der Watcher die Aufgaben selbst (runEmergency) und beendet den
//     Prozess. Windows beendet nach ca. 5 s hart, daher liegt die Summe aus
//     Frist und Zeitlimit unter 3 s.
//   - Im Notfall stellt runEmergency ZUERST das Terminal wieder her (die
//     SSH-Sitzung meldet dafür term.Restore über registerEmergencyRestore an),
//     dann installiert es das Update, danach räumt es auf, speichert und
//     trennt ZULETZT die Verbindungen (Build 89). Das Trennen beendet die
//     SSH-Sitzung; Bubbletea kehrt dann aus tea.Exec zurück, erledigt das
//     schon angeforderte Quit und stellt das Terminal selbst her. Darauf
//     wartet runEmergency (allDone) im selben Zeitbudget. Kommt allDone nicht,
//     setzt es das Terminal vor der Rückkehr ein zweites Mal zurück. Unter
//     Linux/macOS schreibt es dabei zusätzlich eine Folge, die Bracketed
//     Paste ausschaltet, den Alt-Screen verlässt, die Maus-Modi abschaltet
//     und den Cursor zeigt (Build 89, Runde 2 und 3) - term.Restore stellt
//     nur den Terminal-Modus her.
//   - Der Logeintrag „ssh-easy beendet (Notfall-Ablauf)“ steht VOR diesem
//     letzten Zurücksetzen; danach wird bis exit(0) nichts mehr geloggt.
//
// Jede Aufgabe läuft über beide Wege hinweg genau einmal (sync.Once).
//
// @author Kurt Ingwer
// @date   2026-11-07 11:49
package main

import (
	"os"
	"sync"
	"time"
)

// closeGracePeriod - so lange wartet der Watcher nach dem Signal auf das
// normale Programmende, bevor er den Notfall-Ablauf startet.
const closeGracePeriod = 1500 * time.Millisecond

// emergencyStepsTimeout - gemeinsames Zeitbudget im Notfall nach dem Update:
// cleanup, persist und disconnect ZUSAMMEN und danach das Warten auf allDone
// (Build 89). Eine hängende Verbindung oder ein blockierter Konsolenaufruf
// darf das Beenden nicht aufhalten. Bis Build 87 hieß die Konstante
// emergencyPersistTimeout und galt nur für persist.
//
// closeGracePeriod + emergencyStepsTimeout (2,25 s) muss unter 3 s bleiben:
// Windows beendet den Prozess nach CTRL_CLOSE_EVENT nach ca. 5 s hart.
const emergencyStepsTimeout = 750 * time.Millisecond

// emergencyResetWriteTimeout - eigenes Zeitlimit für das Schreiben der
// Terminalfolge beim letzten Zurücksetzen (Build 89, Runde 2). Nach SIGHUP
// ist die Gegenstelle des Terminals womöglich weg, ein Write kann dann
// blockieren; er darf exit(0) nicht aufhalten.
//
// Gesamtbudget: closeGracePeriod + emergencyStepsTimeout +
// emergencyResetWriteTimeout = 2,5 s, also unter 3 s.
const emergencyResetWriteTimeout = 250 * time.Millisecond

// emergencyResetSequence - Folge für das letzte Zurücksetzen, in dieser
// Reihenfolge: Bracketed Paste aus (2004), Alt-Screen verlassen (1049),
// Maus-Modi aus (1000 Klicks, 1002 Ziehen, 1003 jede Bewegung, 1006
// SGR-Format; Build 89, Runde 3 - sonst schreibt nach einem Notfall mit mc
// in der toten Sitzung jede Mausbewegung Escape-Müll in die lokale Shell),
// Cursor zeigen (25). Der Cursor kommt zuletzt, sonst bliebe er nach dem
// Verlassen des Alt-Screens womöglich unsichtbar.
const emergencyResetSequence = "\x1b[?2004l\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h"

// emergencyTerminalReset schreibt eine Steuerfolge auf das Terminal
// (Funktionszeiger, damit Tests ihn ersetzen können). Auf allen Plattformen
// deklariert; unter Windows ruft runEmergency ihn nie auf
// (emergencyTerminalResetEnabled). Der Standard schreibt nach stdout.
var emergencyTerminalReset func(seq string) = writeTerminalResetToStdout

// writeTerminalResetToStdout - Standard für emergencyTerminalReset: schreibt
// seq nach stdout. Fehler werden ignoriert (der Prozess endet gleich).
//
// @param seq - Steuerfolge
// @date   2026-11-07 14:00
func writeTerminalResetToStdout(seq string) {
	_, _ = os.Stdout.WriteString(seq)
}

// exitTasks bündelt die Aufgaben beim Programmende. Ein nil-Schritt wird
// übersprungen; der Nullwert ist benutzbar. Jeder Schritt hat ein eigenes
// sync.Once: Laufen runNormal und runEmergency gleichzeitig, wartet der
// zweite Aufruf, bis der Schritt fertig ist - er läuft nie doppelt (sonst
// würde z. B. das Setup zweimal gestartet).
type exitTasks struct {
	// disconnect trennt alle SSH-Verbindungen
	disconnect func()
	// apply installiert ein heruntergeladenes Update
	apply func()
	// cleanup löscht lokale Kopien dieser Sitzung (F9 "Öffnen")
	cleanup func()
	// persist speichert Schrift- und Fenstergröße
	persist func()

	// je Schritt genau ein Durchlauf
	disconnectOnce sync.Once
	applyOnce      sync.Once
	cleanupOnce    sync.Once
	persistOnce    sync.Once
}

// runStep führt einen Schritt höchstens einmal aus. nil wird übersprungen.
//
// @param once - Once des Schritts
// @param step - Schritt (nil erlaubt)
// @date   2026-10-31 14:40
func runStep(once *sync.Once, step func()) {
	once.Do(func() {
		if step != nil {
			step()
		}
	})
}

// runNormal erledigt die Aufgaben beim normalen Ende (q, Ende von p.Run)
// in der bisherigen Reihenfolge: trennen, aufräumen, speichern, ZULETZT das
// Update installieren.
//
// @date   2026-10-31 14:40
func (t *exitTasks) runNormal() {
	runStep(&t.disconnectOnce, t.disconnect)
	runStep(&t.cleanupOnce, t.cleanup)
	runStep(&t.persistOnce, t.persist)
	runStep(&t.applyOnce, t.apply)
}

// runEmergency erledigt die Aufgaben, wenn das normale Ende nicht
// rechtzeitig kommt (Build 88, neue Reihenfolge seit Build 89). Windows
// beendet den Prozess bald hart, daher:
//  1. Momentaufnahme der angemeldeten Wiederherstellungen, dann jede davon
//     aufrufen: Terminal zurücksetzen (nur term.Restore, ms)
//  2. apply SYNCHRON: Update installieren (Windows: Helfer starten, Linux:
//     Datei umbenennen - beides ms). Kein anderer Schritt kann es aufhalten.
//  3. cleanup, persist, disconnect in dieser Reihenfolge in EINER Goroutine.
//     disconnect kommt ZULETZT: Hängt eine tote Verbindung, sind Aufräumen
//     und Speichern schon erledigt.
//  4. Im Budget derselben deadline erst auf Schritt 3 warten, danach auf
//     allDone. Das Trennen beendet die SSH-Sitzung, Bubbletea kehrt aus
//     tea.Exec zurück, erledigt das angeforderte Quit und stellt das Terminal
//     selbst her; main schließt dann allDone. allDone == nil: nicht warten.
//  5. Kam allDone nicht rechtzeitig, laufen die Funktionen aus der
//     Momentaufnahme von Schritt 1 ein ZWEITES Mal. Die Sitzung hat sich
//     inzwischen womöglich im Defer abgemeldet, und Bubbletea hat eventuell
//     den Raw-Modus wieder eingeschaltet - darum die alte Momentaufnahme,
//     nicht der aktuelle Stand. Kam allDone rechtzeitig, unterbleibt das
//     (ein altes Restore würde Bubbleteas Wiederherstellung überschreiben).
//     Unter Linux/macOS kommt dazu die Terminalfolge (finalTerminalReset):
//     Bubbletea hat eventuell auch Alt-Screen und versteckten Cursor wieder
//     eingeschaltet, mc in der Sitzung die Mausverfolgung - das stellt
//     term.Restore nicht zurück.
//
// Jeder Restore-Aufruf läuft über callRecovered: Eine Panik stoppt weder die
// anderen Funktionen noch die Schritte.
//
// Die sync.Once-Felder gelten weiter: Läuft runNormal parallel, läuft kein
// Schritt doppelt. Hält runNormal einen Schritt gerade fest, wartet die
// Goroutine darauf - höchstens bis deadline, dann kehrt runEmergency zurück.
//
// @param deadline - gemeinsames Budget für Schritt 3 und das Warten auf allDone
// @param allDone - wird geschlossen, wenn main fertig ist (nil = nicht warten)
// @date   2026-11-07 14:00
func (t *exitTasks) runEmergency(deadline time.Duration, allDone <-chan struct{}) {
	t.runEmergencyThen(deadline, allDone, nil)
}

// runEmergencyThen ist runEmergency mit einem Haken direkt VOR dem letzten
// Zurücksetzen (Schritt 5). Der Watcher schreibt dort den Logeintrag zum
// Ende, damit zwischen dem letzten Zurücksetzen und exit(0) nichts mehr
// geloggt wird (Build 89, Runde 2).
//
// @param deadline - gemeinsames Budget für Schritt 3 und das Warten auf allDone
// @param allDone - wird geschlossen, wenn main fertig ist (nil = nicht warten)
// @param beforeFinalReset - läuft nur vor Schritt 5 (nil erlaubt, Panik abgefangen)
// @return bool - true, wenn Schritt 5 lief (allDone kam nicht rechtzeitig)
// @date   2026-11-07 14:00
func (t *exitTasks) runEmergencyThen(deadline time.Duration, allDone <-chan struct{}, beforeFinalReset func()) bool {
	// 1. Momentaufnahme festhalten und das Terminal wiederherstellen
	restores := emergencyRestores.snapshot()
	callAllRecovered(restores)

	// 2. Update zuerst und synchron
	runStep(&t.applyOnce, t.apply)

	// 3. Rest in einer Goroutine, disconnect zuletzt
	done := make(chan struct{})
	go func() {
		defer close(done)
		runStep(&t.cleanupOnce, t.cleanup)
		runStep(&t.persistOnce, t.persist)
		runStep(&t.disconnectOnce, t.disconnect)
	}()

	// 4. EIN Zeitgeber für Schritte und Warten zusammen
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	if waitStepsThenAllDone(done, allDone, timer.C) {
		// Bubbletea hat das Terminal selbst wiederhergestellt
		return false
	}

	// 5. allDone kam nicht: erst der Haken (Logeintrag), dann das Terminal
	// ein zweites Mal zurücksetzen - als letzter Schritt
	callRecovered(beforeFinalReset)
	finalTerminalReset(restores)
	return true
}

// finalTerminalReset ist das letzte Zurücksetzen vor exit(0): erst die
// Funktionen aus restores (term.Restore, synchron, ms), danach unter
// Linux/macOS die Terminalfolge emergencyResetSequence mit eigenem
// Zeitlimit emergencyResetWriteTimeout (Build 89, Runde 2).
//
// @param restores - Momentaufnahme der Wiederherstellungen
// @date   2026-11-07 14:00
func finalTerminalReset(restores []func()) {
	callAllRecovered(restores)
	if !emergencyTerminalResetEnabled {
		// Windows: kein Write, die Konsole wird mit dem Prozess abgebaut
		return
	}
	// Den Zeiger HIER lesen, nicht in der Goroutine: sonst ein Datenrennen
	// mit Tests, die ihn umsetzen
	writeLimited(emergencyTerminalReset, emergencyResetSequence, emergencyResetWriteTimeout)
}

// writeLimited ruft write(seq) in einer eigenen Goroutine auf und wartet
// höchstens limit. Ein blockierender Write läuft danach im Hintergrund
// weiter (der Prozess endet ohnehin gleich), hält den Aufrufer aber nicht
// länger auf. Eine Panik in write wird abgefangen.
//
// @param write - Schreibfunktion (nil = nichts tun)
// @param seq - zu schreibende Folge
// @param limit - längste Wartezeit
// @return bool - true, wenn write rechtzeitig fertig war
// @date   2026-11-07 14:00
func writeLimited(write func(string), seq string, limit time.Duration) bool {
	if write == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		callRecovered(func() { write(seq) })
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// waitStepsThenAllDone wartet erst auf done (Notfall-Schritte fertig), danach
// auf allDone - beides zusammen höchstens bis expired feuert.
//
// Läuft die Zeit schon während der Schritte ab, wird nicht mehr gewartet;
// ein bereits geschlossenes allDone zählt trotzdem (main war fertig).
//
// @param done - wird geschlossen, wenn die Schritte fertig sind
// @param allDone - wird geschlossen, wenn main fertig ist (nil = nicht warten)
// @param expired - feuert, wenn das gemeinsame Budget aufgebraucht ist
// @return bool - true, wenn allDone rechtzeitig geschlossen wurde
// @date   2026-11-07 11:49
func waitStepsThenAllDone(done <-chan struct{}, allDone <-chan struct{}, expired <-chan time.Time) bool {
	select {
	case <-done:
		// alle Schritte rechtzeitig fertig
	case <-expired:
		// ein Schritt hängt - nur noch nachsehen, ob main schon fertig ist
		return isClosed(allDone)
	}
	if allDone == nil {
		// keine Rückmeldung von main vorgesehen: nicht warten
		return false
	}
	select {
	case <-allDone:
		return true
	case <-expired:
		return false
	}
}

// isClosed meldet, ob der Kanal schon geschlossen ist (ohne zu blockieren).
//
// @param ch - Kanal (nil = nie geschlossen)
// @return bool - true, wenn geschlossen
// @date   2026-11-07 11:49
func isClosed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// emergencyRestoreEntry ist eine angemeldete Wiederherstellung mit einer
// eindeutigen Nummer (damit unregister genau diesen Eintrag entfernt).
type emergencyRestoreEntry struct {
	// id - fortlaufende Nummer der Anmeldung
	id uint64
	// fn - Wiederherstellung (nie nil, nil wird gar nicht erst angemeldet)
	fn func()
}

// emergencyRestoreRegistry hält die angemeldeten Wiederherstellungen.
// Nebenläufig benutzt von der Terminal-Sitzung (an-/abmelden) und vom
// Signal-Watcher (aufrufen), daher mit Mutex.
type emergencyRestoreRegistry struct {
	// mu schützt nextID und entries
	mu sync.Mutex
	// nextID - Nummer für die nächste Anmeldung
	nextID uint64
	// entries - angemeldete Funktionen in Anmelde-Reihenfolge
	entries []emergencyRestoreEntry
}

// emergencyRestores - die eine Registry des Programms
var emergencyRestores emergencyRestoreRegistry

// register meldet fn an und liefert die Abmeldefunktion. Eine nil-Funktion
// wird nicht angemeldet; die Abmeldefunktion ist trotzdem gültig (tut
// nichts). Die Abmeldefunktion darf beliebig oft gerufen werden.
//
// @param fn - Wiederherstellung (nil erlaubt)
// @return func() - meldet fn wieder ab
// @date   2026-11-01 10:37
func (r *emergencyRestoreRegistry) register(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.entries = append(r.entries, emergencyRestoreEntry{id: id, fn: fn})
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { r.unregister(id) })
	}
}

// unregister entfernt den Eintrag mit der Nummer id. Unbekannte Nummern
// werden ignoriert.
//
// @param id - Nummer aus register
// @date   2026-11-01 10:37
func (r *emergencyRestoreRegistry) unregister(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries {
		if e.id == id {
			// Neue Slice bauen statt in place zu verschieben: eine laufende
			// Momentaufnahme (snapshot) bleibt so unverändert.
			kept := make([]emergencyRestoreEntry, 0, len(r.entries)-1)
			kept = append(kept, r.entries[:i]...)
			kept = append(kept, r.entries[i+1:]...)
			r.entries = kept
			return
		}
	}
}

// snapshot liefert eine Kopie der angemeldeten Funktionen. Aufgerufen wird
// außerhalb der Sperre, damit eine Wiederherstellung, die selbst an- oder
// abmeldet, keinen Deadlock auslöst.
//
// @return []func() - angemeldete Funktionen in Anmelde-Reihenfolge
// @date   2026-11-01 10:37
func (r *emergencyRestoreRegistry) snapshot() []func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	fns := make([]func(), 0, len(r.entries))
	for _, e := range r.entries {
		fns = append(fns, e.fn)
	}
	return fns
}

// callRecovered ruft fn auf und fängt eine Panik ab. Im Beenden-Pfad darf
// nichts den Prozess abstürzen lassen.
//
// @param fn - aufzurufende Funktion (nil wird übersprungen)
// @return bool - false wenn fn paniert hat
// @date   2026-11-01 10:37
func callRecovered(fn func()) (ok bool) {
	if fn == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	fn()
	return true
}

// registerEmergencyRestore meldet eine Wiederherstellung für den Notfall an
// (Build 88). Die SSH-Sitzung meldet nach term.MakeRaw term.Restore an und
// beim normalen Ende wieder ab. Kommt in der Zwischenzeit der Notfall-Ablauf,
// setzt runEmergency das Terminal so zurück - sonst bliebe die Konsole im
// Raw-Modus.
//
// @param fn - Wiederherstellung (nil erlaubt, wird ignoriert)
// @return unregister - meldet fn wieder ab; mehrfacher Aufruf ist harmlos
// @date   2026-11-01 10:37
func registerEmergencyRestore(fn func()) (unregister func()) {
	return emergencyRestores.register(fn)
}

// runEmergencyRestore ruft alle angemeldeten Wiederherstellungen auf. Jede
// läuft mit eigenem recover: Paniert eine, laufen die anderen trotzdem.
// Nebenläufigkeitssicher. Nur für den Notfall-Ablauf - beim normalen Ende
// setzt die Sitzung das Terminal selbst zurück.
//
// @date   2026-11-07 11:49
func runEmergencyRestore() {
	callAllRecovered(emergencyRestores.snapshot())
}

// callAllRecovered ruft alle Funktionen der Reihe nach über callRecovered
// auf. Eine Panik stoppt die übrigen nicht.
//
// @param fns - Funktionen (nil-Einträge werden übersprungen)
// @date   2026-11-07 11:49
func callAllRecovered(fns []func()) {
	for _, fn := range fns {
		callRecovered(fn)
	}
}

// watchCloseSignals überwacht das Schließen des Fensters. BLOCKIEREND -
// main startet die Funktion mit "go".
//
// Kehrt zurück, wenn allDone geschlossen wird, bevor ein Signal kam, oder
// nachdem das erste Signal behandelt ist:
//  1. ALS ALLERERSTES onSignal SYNCHRON (main setzt closeRequested), noch vor
//     dem ersten Logeintrag (Build 89): Das Schreiben ins Log kann auf einem
//     langsamen Laufwerk hängen. So sieht main den Merker auch dann, wenn
//     p.Run() nach quit sofort zurückkehrt.
//  2. erst danach ins Log schreiben
//  3. quit in eigener Goroutine (p.Quit() blockiert während tea.Exec)
//  4. bis grace auf allDone warten - kommt es, endet main selbst
//  5. sonst den Notfall-Ablauf (runEmergencyThen mit emergencyStepsTimeout,
//     wartet im Budget noch auf allDone, Build 89) und exit(0). Der
//     Logeintrag „ssh-easy beendet (Notfall-Ablauf)“ steht VOR dem letzten
//     Zurücksetzen des Terminals; danach wird bis exit(0) nichts mehr
//     geloggt (Build 89, Runde 2) - ein hängender Log-Write darf das
//     Terminal nicht zwischen Zurücksetzen und Ende kaputt lassen.
//
// Weitere Signale lösen keine zweite Behandlung aus (onSignal und quit laufen
// je einmal): Nach der Rückkehr liest niemand mehr den Kanal, signal.Notify
// verwirft sie dann.
//
// @param sigs - Kanal aus signal.Notify(…, closeSignals()...)
// @param onSignal - läuft beim ersten Signal synchron vor quit (nil erlaubt)
// @param quit - bittet die Oberfläche um ein normales Ende (nil erlaubt)
// @param allDone - wird geschlossen, wenn main die Aufgaben erledigt hat
// @param tasks - Aufgaben beim Beenden (nil = keine)
// @param grace - Frist für das normale Ende
// @param exit - beendet den Prozess (os.Exit; Funktionszeiger für Tests)
// @param log - Logger (nil erlaubt)
// @date   2026-11-07 14:00
func watchCloseSignals(sigs <-chan os.Signal, onSignal func(), quit func(), allDone <-chan struct{}, tasks *exitTasks, grace time.Duration, exit func(int), log *Logger) {
	var sig os.Signal
	select {
	case <-allDone:
		// Normal beendet, ohne dass das Fenster geschlossen wurde
		return
	case sig = <-sigs:
	}
	// onSignal läuft als Allererstes, also sicher VOR dem Log und VOR quit -
	// und nur einmal, weil der Kanal danach nicht mehr gelesen wird. Eine
	// Panik darf den Watcher nicht beenden, sonst fehlt der Notfall-Ablauf.
	signalOK := callRecovered(onSignal)
	log.Info("Signal %v: Fenster wird geschlossen, normales Ende angefordert", sig)
	if !signalOK {
		log.Error("onSignal nach Signal %v ist abgestürzt", sig)
	}
	if quit != nil {
		go quit()
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-allDone:
		return
	case <-timer.C:
	}

	log.Warn("Kein normales Ende binnen %v nach Signal %v - Notfall-Ablauf", grace, sig)
	logDone := func() { log.Info("ssh-easy beendet (Notfall-Ablauf)") }
	if tasks != nil {
		if !tasks.runEmergencyThen(emergencyStepsTimeout, allDone, logDone) {
			// allDone kam rechtzeitig: Bubbletea hat das Terminal selbst
			// hergestellt, kein letztes Zurücksetzen - Logeintrag hier
			logDone()
		}
	} else {
		// Auch ohne Aufgaben das Terminal nicht kaputt zurücklassen;
		// Logeintrag vorher, danach nichts mehr bis exit(0)
		logDone()
		finalTerminalReset(emergencyRestores.snapshot())
	}
	exit(0)
}

// exitCodeFor liefert den Exit-Code nach p.Run(). Ein Fehler zählt nicht,
// wenn gerade das Fenster geschlossen wurde - Bubbletea meldet dann mitunter
// einen Fehler, der kein Programmfehler ist.
//
// @param err - Fehler aus p.Run()
// @param closeRequested - true wenn ein Schließ-Signal kam
// @return int - 0 oder 1
// @date   2026-10-31 14:40
func exitCodeFor(err error, closeRequested bool) int {
	if err != nil && !closeRequested {
		return 1
	}
	return 0
}

// finishRun ist der Abschnitt nach p.Run() in main: den Fehler nur
// protokollieren, ALLE Aufgaben erledigen (auch bei Fehler - früher ging es
// mit os.Exit(1) sofort raus, ohne Update), DANACH allDone schließen, damit
// der Watcher keinen Notfall mehr auslöst. Genau einmal aufrufbar.
//
// @param runErr - Fehler aus p.Run()
// @param closeRequested - true wenn ein Schließ-Signal kam
// @param tasks - Aufgaben beim Beenden (nil = keine)
// @param allDone - wird geschlossen (nil erlaubt)
// @param log - Logger (nil erlaubt)
// @return int - Exit-Code (exitCodeFor)
// @date   2026-10-31 14:40
func finishRun(runErr error, closeRequested bool, tasks *exitTasks, allDone chan struct{}, log *Logger) int {
	if runErr != nil {
		log.Error("Fehler in der TUI: %v", runErr)
	}
	if tasks != nil {
		tasks.runNormal()
	}
	if allDone != nil {
		close(allDone)
	}
	return exitCodeFor(runErr, closeRequested)
}
