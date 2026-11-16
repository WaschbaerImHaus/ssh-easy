// Paket main - Konsolen-Steuerereignisse unter Windows (Build 91)
//
// WARUM EIN EIGENER HANDLER?
// ==========================
// Go meldet seinen Konsolen-Handler genau einmal beim Start der Laufzeit an
// (runtime.goenvs, VOR allen init()-Funktionen). icon_windows.go ruft danach
// in init() AllocConsole auf. AllocConsole (ebenso AttachConsole und
// FreeConsole) setzt die Handler-Tabelle des Prozesses zurück; laut
// Microsoft müssen Handler danach neu angemeldet werden. Bis Build 90 tat das
// niemand: Das Schließkreuz löste CTRL_CLOSE_EVENT aus, der Standard-Handler
// von Windows rief sofort ExitProcess auf, und signal.Notify in main.go bekam
// nie ein SIGTERM. Es wurde weder getrennt, aufgeräumt, gespeichert noch das
// bereitliegende Update installiert.
//
// LÖSUNG
// ======
// icon_windows.go meldet nach AllocConsole einen eigenen Handler an. Er ruft
// handleConsoleCtrlEvent auf: CTRL_CLOSE_EVENT, CTRL_LOGOFF_EVENT und
// CTRL_SHUTDOWN_EVENT werden als SIGTERM ohne Blockieren in das Relay
// consoleCloseRelay gelegt. watchCloseSignals liest dieses Relay zusätzlich
// zum Kanal aus signal.Notify. Danach blockiert der Handler wie Go selbst,
// damit Windows den Prozess nicht sofort beendet; os.Exit im Watcher oder
// das normale Ende von main beendet ihn.
//
// Alles hier ist plattformneutral und unter Linux testbar. Nur das Anmelden
// beim Betriebssystem steht in icon_windows.go.
//
// @author Kurt Ingwer
// @date   2026-11-16 16:45

package main

import (
	"os"
	"syscall"
	"time"
)

// Werte der Windows-Konsolen-Steuerereignisse (wincon.h). Hier selbst
// definiert, damit die Logik auch unter Linux übersetzt und getestet werden
// kann (golang.org/x/sys/windows gibt es nur unter Windows).
const (
	// consoleCtrlCEvent - Strg+C
	consoleCtrlCEvent uint32 = 0
	// consoleCtrlBreakEvent - Strg+Pause
	consoleCtrlBreakEvent uint32 = 1
	// consoleCtrlCloseEvent - Fenster geschlossen (Kreuz, in Windows Terminal auch Alt+F4)
	consoleCtrlCloseEvent uint32 = 2
	// consoleCtrlLogoffEvent - Benutzer meldet sich ab
	consoleCtrlLogoffEvent uint32 = 5
	// consoleCtrlShutdownEvent - System fährt herunter
	consoleCtrlShutdownEvent uint32 = 6
)

// consoleCtrlHoldTimeout - so lange hält der Handler den Prozess nach einem
// Schließ-Ereignis höchstens am Leben. Windows beendet nach CTRL_CLOSE_EVENT
// ohnehin nach ca. 5 s hart; der Notfall-Ablauf des Watchers braucht höchstens
// 2,25 s und endet mit os.Exit. Der Wert muss also nur deutlich darüber liegen.
const consoleCtrlHoldTimeout = 10 * time.Second

// consoleCtrlEventSignal bildet ein Konsolen-Steuerereignis auf ein Signal ab.
//
// Schließen, Abmelden und Herunterfahren werden zu SIGTERM - wie in der
// Go-Laufzeit (runtime.ctrlHandler).
//
// GRENZE Abmelden/Herunterfahren: Laut Microsoft (SetConsoleCtrlHandler,
// HandlerRoutine) ruft Windows den Handler für CTRL_LOGOFF_EVENT und
// CTRL_SHUTDOWN_EVENT NICHT auf, wenn der Prozess user32.dll geladen hat.
// ssh-easy lädt user32.dll (icon_windows.go: Icon, Fokus). Die beiden
// Ereignisse sind hier deshalb nur der Vollständigkeit halber abgebildet und
// kommen vermutlich nie an. Für Abmelden und Herunterfahren gilt weiter die
// bekannte Grenze: Windows beendet ssh-easy ohne Aufräumen, ein
// bereitliegendes Update wird beim nächsten normalen Beenden installiert.
// Verlässlich ankommen sollte nur CTRL_CLOSE_EVENT (Schließkreuz).
//
// Strg+C und Strg+Pause bleiben BEWUSST
// unbehandelt (Rückgabe false): Seit AllocConsole fehlt Gos Handler, Windows
// beendet den Prozess dann wie bisher selbst. Ohnehin erzeugt die Konsole im
// Raw-Modus (Bubbletea, SSH-Sitzung) bei Strg+C kein Ereignis, sondern eine
// Taste.
//
// @param ev - Ereignis-Code von Windows
// @return os.Signal - syscall.SIGTERM oder nil
// @return bool - true, wenn das Ereignis behandelt wird
// @date   2026-11-16 17:06
func consoleCtrlEventSignal(ev uint32) (os.Signal, bool) {
	switch ev {
	case consoleCtrlCloseEvent, consoleCtrlLogoffEvent, consoleCtrlShutdownEvent:
		return syscall.SIGTERM, true
	default:
		// Strg+C, Strg+Pause und unbekannte Werte: nächster Handler (Windows-Standard)
		return nil, false
	}
}

// closeEventRelay reicht Schließ-Ereignisse vom Windows-Handler an den
// Watcher weiter. Der Kanal hat Puffer 1: Ein Ereignis, das ankommt, bevor der
// Watcher liest (z. B. noch während des Starts), bleibt liegen und wird beim
// ersten Lesen nachgeliefert. Weitere Ereignisse werden verworfen - der
// Watcher behandelt ohnehin nur das erste.
type closeEventRelay struct {
	// ch - gepufferter Kanal (Kapazität 1)
	ch chan os.Signal
}

// newCloseEventRelay erzeugt ein leeres Relay mit Puffer 1.
//
// @return *closeEventRelay - neues Relay
// @date   2026-11-16 16:45
func newCloseEventRelay() *closeEventRelay {
	return &closeEventRelay{ch: make(chan os.Signal, 1)}
}

// forward legt ein Signal im Relay ab. BLOCKIERT NIE - der Aufrufer ist der
// Konsolen-Thread von Windows.
//
// @param sig - weiterzuleitendes Signal
// @return bool - false nur ohne Relay (nil); true, wenn das Signal abgelegt wurde oder schon ein Ereignis wartet
// @date   2026-11-16 17:06
func (r *closeEventRelay) forward(sig os.Signal) bool {
	if r == nil || r.ch == nil {
		return false
	}
	select {
	case r.ch <- sig:
	default:
		// Puffer voll: Es wartet schon ein Ereignis, das genügt
	}
	return true
}

// events liefert den Kanal zum Lesen. Ohne Relay nil - ein nil-Kanal
// blockiert im select einfach und stört dort nicht.
//
// @return <-chan os.Signal - Kanal oder nil
// @date   2026-11-16 16:45
func (r *closeEventRelay) events() <-chan os.Signal {
	if r == nil {
		return nil
	}
	return r.ch
}

// consoleCloseRelay - das Relay des Prozesses. Der Windows-Handler schreibt
// hinein, watchCloseSignals liest daraus. Unter Linux/macOS bleibt es leer.
var consoleCloseRelay = newCloseEventRelay()

// handleConsoleCtrlEvent ist der reine Kern des Windows-Handlers.
//
// Nicht behandelte Ereignisse liefern 0 (Windows ruft dann den nächsten
// Handler, am Ende den Standard). Behandelte Ereignisse gehen ohne Blockieren
// ins Relay; danach wartet der Handler hold lang (sleep), damit Windows den
// Prozess nicht sofort nach der Rückkehr beendet, und liefert 1.
//
// @param ev - Ereignis-Code von Windows
// @param relay - Ziel der Weiterleitung (nil = nicht behandelt)
// @param hold - Wartezeit nach dem Weiterleiten
// @param sleep - Wartefunktion (time.Sleep; nil = nicht warten, für Tests)
// @return uintptr - 1 = behandelt, 0 = nicht behandelt
// @date   2026-11-16 16:45
func handleConsoleCtrlEvent(ev uint32, relay *closeEventRelay, hold time.Duration, sleep func(time.Duration)) uintptr {
	sig, ok := consoleCtrlEventSignal(ev)
	if !ok {
		return 0
	}
	if !relay.forward(sig) {
		return 0
	}
	if sleep != nil {
		sleep(hold)
	}
	return 1
}

// consoleCtrlRegistration beschreibt, ob init() den eigenen Handler
// anmelden konnte. init() läuft vor dem Logger; watchCloseSignals schreibt den
// Stand deshalb später einmal ins Log.
type consoleCtrlRegistration struct {
	// attempted - true, wenn init die Anmeldung versucht hat (nur Windows)
	attempted bool
	// err - Fehler der Anmeldung; nil = erfolgreich
	err error
}

// consoleCtrlRegistrationState - Ergebnis der Anmeldung aus init()
// (icon_windows.go). Wird nur dort vor main geschrieben und danach nur
// gelesen.
var consoleCtrlRegistrationState consoleCtrlRegistration

// logConsoleCtrlRegistration schreibt den Stand der Anmeldung ins Log.
// Ohne Versuch (Linux/macOS, Update-Helfer) schreibt sie nichts.
//
// @param log - Logger (nil erlaubt)
// @param reg - Ergebnis der Anmeldung
// @date   2026-11-16 16:45
func logConsoleCtrlRegistration(log *Logger, reg consoleCtrlRegistration) {
	if !reg.attempted {
		return
	}
	if reg.err != nil {
		log.Warn("Konsolen-Handler nicht registriert: %v - Schließen mit dem Kreuz beendet ohne Aufräumen und ohne Update", reg.err)
		return
	}
	log.Info("Konsolen-Handler registriert")
}
