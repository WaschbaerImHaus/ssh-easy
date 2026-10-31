// Paket main - Beenden über Schließkreuz, Alt+F4 und SIGHUP (Build 87)
//
// Wer das Fenster schließt, bekommt unter Linux SIGHUP (Terminal weg) und
// unter Windows CTRL_CLOSE_EVENT (in Go: SIGTERM). Bis Build 86 lief dann
// der Code nach p.Run() nie: Verbindungen blieben offen, und ein
// heruntergeladenes Update wurde nicht installiert.
//
// Ablauf:
//   - main registriert closeSignals() und startet watchCloseSignals.
//   - Beim ersten Signal bittet der Watcher Bubbletea um ein normales Ende
//     (quit). Endet das Programm innerhalb von closeGracePeriod, erledigt
//     main die Aufgaben wie bei q (finishRun -> runNormal).
//   - Sonst (z. B. Bubbletea verwirft das Signal während tea.Exec) erledigt
//     der Watcher die Aufgaben selbst (runEmergency) und beendet den
//     Prozess. Windows beendet nach ca. 5 s hart, daher liegt die Summe aus
//     Frist und Zeitlimit unter 3 s.
//
// Jede Aufgabe läuft über beide Wege hinweg genau einmal (sync.Once).
//
// @author Kurt Ingwer
// @date   2026-10-31 14:40
package main

import (
	"os"
	"sync"
	"time"
)

// closeGracePeriod - so lange wartet der Watcher nach dem Signal auf das
// normale Programmende, bevor er den Notfall-Ablauf startet.
const closeGracePeriod = 1500 * time.Millisecond

// emergencyPersistTimeout - Zeitlimit für das Speichern von Schrift- und
// Fenstergröße im Notfall-Ablauf (ein hängender Konsolenaufruf darf das
// Beenden nicht aufhalten).
const emergencyPersistTimeout = 750 * time.Millisecond

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
// rechtzeitig kommt. Das Update kommt hier VOR Aufräumen und Speichern,
// weil Windows den Prozess bald hart beendet. persist läuft in einer eigenen
// Goroutine und wird nach persistTimeout aufgegeben.
//
// @param persistTimeout - Zeitlimit für persist
// @date   2026-10-31 14:40
func (t *exitTasks) runEmergency(persistTimeout time.Duration) {
	runStep(&t.disconnectOnce, t.disconnect)
	runStep(&t.applyOnce, t.apply)
	runStep(&t.cleanupOnce, t.cleanup)

	done := make(chan struct{})
	go func() {
		defer close(done)
		runStep(&t.persistOnce, t.persist)
	}()
	timer := time.NewTimer(persistTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		// persist hängt - aufgeben, der Prozess endet ohnehin gleich
	}
}

// watchCloseSignals überwacht das Schließen des Fensters. BLOCKIEREND -
// main startet die Funktion mit "go".
//
// Kehrt zurück, wenn allDone geschlossen wird, bevor ein Signal kam, oder
// nachdem das erste Signal behandelt ist:
//  1. quit in eigener Goroutine (p.Quit() blockiert während tea.Exec)
//  2. bis grace auf allDone warten - kommt es, endet main selbst
//  3. sonst tasks.runEmergency und exit(0)
//
// Weitere Signale lösen keine zweite Behandlung aus: Nach der Rückkehr liest
// niemand mehr den Kanal, signal.Notify verwirft sie dann.
//
// @param sigs - Kanal aus signal.Notify(…, closeSignals()...)
// @param quit - bittet die Oberfläche um ein normales Ende
// @param allDone - wird geschlossen, wenn main die Aufgaben erledigt hat
// @param tasks - Aufgaben beim Beenden (nil = keine)
// @param grace - Frist für das normale Ende
// @param exit - beendet den Prozess (os.Exit; Funktionszeiger für Tests)
// @param log - Logger (nil erlaubt)
// @date   2026-10-31 14:40
func watchCloseSignals(sigs <-chan os.Signal, quit func(), allDone <-chan struct{}, tasks *exitTasks, grace time.Duration, exit func(int), log *Logger) {
	var sig os.Signal
	select {
	case <-allDone:
		// Normal beendet, ohne dass das Fenster geschlossen wurde
		return
	case sig = <-sigs:
	}
	log.Info("Signal %v: Fenster wird geschlossen, normales Ende angefordert", sig)
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
		tasks.runEmergency(emergencyPersistTimeout)
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
