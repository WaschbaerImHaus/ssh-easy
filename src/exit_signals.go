// Paket main - Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87, ergänzt in Build 88)
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
//     dann installiert es das Update, erst danach kommt der Rest.
//
// Jede Aufgabe läuft über beide Wege hinweg genau einmal (sync.Once).
//
// @author Kurt Ingwer
// @date   2026-11-01 10:37
package main

import (
	"os"
	"sync"
	"time"
)

// closeGracePeriod - so lange wartet der Watcher nach dem Signal auf das
// normale Programmende, bevor er den Notfall-Ablauf startet.
const closeGracePeriod = 1500 * time.Millisecond

// emergencyStepsTimeout - gemeinsames Zeitlimit für die Notfall-Schritte nach
// dem Update (disconnect, cleanup, persist ZUSAMMEN). Eine hängende
// Verbindung oder ein blockierter Konsolenaufruf darf das Beenden nicht
// aufhalten. Bis Build 87 hieß die Konstante emergencyPersistTimeout und galt
// nur für persist.
const emergencyStepsTimeout = 750 * time.Millisecond

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
// rechtzeitig kommt (Build 88). Windows beendet den Prozess bald hart, daher:
//  1. runEmergencyRestore: Terminal zurücksetzen (nur term.Restore, ms)
//  2. apply SYNCHRON: Update installieren (Windows: Helfer starten, Linux:
//     Datei umbenennen - beides ms). Ein hängendes disconnect kann das
//     Update so nicht mehr verhindern.
//  3. disconnect, cleanup, persist in dieser Reihenfolge in EINER Goroutine;
//     gewartet wird höchstens deadline (für alle drei zusammen). Hängt ein
//     Schritt, wird der Rest aufgegeben - der Prozess endet ohnehin gleich.
//
// Die sync.Once-Felder gelten weiter: Läuft runNormal parallel, läuft kein
// Schritt doppelt. Hält runNormal einen Schritt gerade fest, wartet die
// Goroutine darauf - höchstens bis deadline, dann kehrt runEmergency zurück.
//
// @param deadline - gemeinsames Zeitlimit für disconnect, cleanup und persist
// @date   2026-11-01 10:37
func (t *exitTasks) runEmergency(deadline time.Duration) {
	// 1. Terminal wiederherstellen (Paniken fängt runEmergencyRestore ab)
	runEmergencyRestore()

	// 2. Update zuerst und synchron
	runStep(&t.applyOnce, t.apply)

	// 3. Rest in einer Goroutine mit gemeinsamem Zeitlimit
	done := make(chan struct{})
	go func() {
		defer close(done)
		runStep(&t.disconnectOnce, t.disconnect)
		runStep(&t.cleanupOnce, t.cleanup)
		runStep(&t.persistOnce, t.persist)
	}()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case <-done:
		// alle Schritte rechtzeitig fertig
	case <-timer.C:
		// ein Schritt hängt - aufgeben, der Prozess endet ohnehin gleich
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
// @date   2026-11-01 10:37
func runEmergencyRestore() {
	for _, fn := range emergencyRestores.snapshot() {
		callRecovered(fn)
	}
}

// watchCloseSignals überwacht das Schließen des Fensters. BLOCKIEREND -
// main startet die Funktion mit "go".
//
// Kehrt zurück, wenn allDone geschlossen wird, bevor ein Signal kam, oder
// nachdem das erste Signal behandelt ist:
//  1. onSignal SYNCHRON (main setzt closeRequested) - so sieht main den
//     Merker auch dann, wenn p.Run() nach quit sofort zurückkehrt
//  2. quit in eigener Goroutine (p.Quit() blockiert während tea.Exec)
//  3. bis grace auf allDone warten - kommt es, endet main selbst
//  4. sonst tasks.runEmergency(emergencyStepsTimeout) und exit(0)
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
// @date   2026-11-01 10:37
func watchCloseSignals(sigs <-chan os.Signal, onSignal func(), quit func(), allDone <-chan struct{}, tasks *exitTasks, grace time.Duration, exit func(int), log *Logger) {
	var sig os.Signal
	select {
	case <-allDone:
		// Normal beendet, ohne dass das Fenster geschlossen wurde
		return
	case sig = <-sigs:
	}
	log.Info("Signal %v: Fenster wird geschlossen, normales Ende angefordert", sig)
	// onSignal läuft hier, also sicher VOR quit - und nur einmal, weil der
	// Kanal danach nicht mehr gelesen wird. Eine Panik darf den Watcher
	// nicht beenden, sonst fehlt der Notfall-Ablauf.
	if !callRecovered(onSignal) {
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
	if tasks != nil {
		tasks.runEmergency(emergencyStepsTimeout)
	} else {
		// Auch ohne Aufgaben das Terminal nicht im Raw-Modus zurücklassen
		runEmergencyRestore()
	}
	log.Info("ssh-easy beendet (Notfall-Ablauf)")
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
