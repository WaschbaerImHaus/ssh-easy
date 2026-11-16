// Paket main - Tests: Schnellweg je Verbindung abschalten, hängendes ListTree (Build 90)
//
// Prüft den Teil von filemanager_fastscan.go, der den find-Schnellweg nach
// Leerlauf, Zeitlimit oder einem hängenden Kanal für DIESE Verbindung
// (Instanz) abschaltet und das Erfassen sofort per SFTP fortsetzt:
//   - hängt schon ListTree (Server beantwortet das Öffnen des Exec-Kanals nie,
//     Kontext wird ignoriert), geht es nach Leerlaufgrenze bzw. Zeitlimit per
//     SFTP weiter: Ergebnis wie die Referenz, KEIN AbortTreeList, kein
//     errConnectionLost (es gibt keinen Kanal, die SFTP-Sitzung lebt)
//   - fastScanSwitch: nach Leerlauf, Zeitlimit und hängendem Kanal ist der
//     Schnellweg für die Instanz aus; ein zweiter Lauf ruft ListTree nicht mehr,
//     eine andere Instanz schon
//   - Esc allein schaltet nie ab; Hängen nach Esc schon
//   - inheritFastScanOff überträgt den Zustand auf die neue Verbindung
//     (Neu-Verbinden über fmReadPanelCmd)
//
// Wiederverwendet: fastFS/fastSource/fastHangFS/fastAbortCountFS und die
// Läufer aus filemanager_fastscan_test.go, reconnFS/openReconnectManager aus
// filemanager_reconnect_test.go.
//
// @author Kurt Ingwer
// @date   2026-11-16 12:22
package main

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Fakes mit fastScanSwitch ---

// fastSwitchSrc ist eine Quelle, deren Schnellweg abschaltbar ist und die
// ListTree-Aufrufe zählt.
type fastSwitchSrc interface {
	remoteFS
	fastScanSwitch
	// listCount zählt die ListTree-Aufrufe
	listCount() int
	// shutdown gibt Hängendes frei (Aufräumen)
	shutdown()
}

// fastSwitchNormal ist ein normal antwortender fastFS (Leser endet bei ctx und
// Close) mit fastScanSwitch.
type fastSwitchNormal struct {
	*fastFS
	off atomic.Bool
}

func (s *fastSwitchNormal) disableFastScanForSession() { s.off.Store(true) }
func (s *fastSwitchNormal) fastScanDisabled() bool     { return s.off.Load() }
func (s *fastSwitchNormal) listCount() int             { l, _, _ := s.counters(); return l }
func (s *fastSwitchNormal) shutdown()                  {}

// fastSwitchAbort ist ein normal antwortender fastFS mit AbortTreeList-Zähler
// (fastAbortCountFS) und fastScanSwitch.
type fastSwitchAbort struct {
	*fastAbortCountFS
	off   atomic.Bool
	lists atomic.Int32
}

func (s *fastSwitchAbort) disableFastScanForSession() { s.off.Store(true) }
func (s *fastSwitchAbort) fastScanDisabled() bool     { return s.off.Load() }
func (s *fastSwitchAbort) listCount() int             { return int(s.lists.Load()) }
func (s *fastSwitchAbort) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	s.lists.Add(1)
	return s.fastAbortCountFS.ListTree(ctx, starts, maxDepth)
}

// fastSwitchHang ist ein fastHangFS (ignoriert Kontext und Close) mit
// fastScanSwitch.
type fastSwitchHang struct {
	*fastHangFS
	off   atomic.Bool
	lists atomic.Int32
}

func (s *fastSwitchHang) disableFastScanForSession() { s.off.Store(true) }
func (s *fastSwitchHang) fastScanDisabled() bool     { return s.off.Load() }
func (s *fastSwitchHang) listCount() int             { return int(s.lists.Load()) }
func (s *fastSwitchHang) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	s.lists.Add(1)
	return s.fastHangFS.ListTree(ctx, starts, maxDepth)
}

// fastRefOutcome liefert die Referenz: derselbe Lauf ganz ohne Schnellweg.
func fastRefOutcome(t *testing.T) fastOutcome {
	t.Helper()
	was := fastScanEnabled
	fastScanEnabled = false
	ref := fastRun(t, context.Background(), fastSource(), false, []string{"projekt"})
	fastScanEnabled = was
	if len(ref.snap) == 0 || len(ref.failures) != 0 {
		t.Fatalf("Referenz unbrauchbar: %d Einträge, Fehler %v", len(ref.snap), ref.failures)
	}
	return ref
}

// fastLogsContain meldet, ob eine Protokollzeile den Text enthält.
func fastLogsContain(o fastOutcome, text string) bool {
	return strings.Contains(strings.Join(o.logs, "\n"), text)
}

// fastHasConnectionLost meldet, ob ein Fehler errConnectionLost ist.
func fastHasConnectionLost(o fastOutcome) bool {
	for _, e := range o.failErrs {
		if errors.Is(e, errConnectionLost) {
			return true
		}
	}
	return false
}

// --- A. ListTree hängt ---

// TestFastScanHangingListTreeFallsBack - Schon das Öffnen des Exec-Kanals
// hängt (ListTree kehrt nie zurück und ignoriert den Kontext): Der
// Leerlauf-Wächter (läuft VOR ListTree) bzw. das Zeitlimit beendet das
// Warten, und es geht binnen etwa einer Sekunde per SFTP weiter. Ergebnis
// vollständig gleich der Referenz, KEIN AbortTreeList (kein Kanal, die
// SFTP-Sitzung lebt), kein errConnectionLost, nicht als abgebrochen gemeldet.
// Das Protokoll nennt die Ursache (idle timeout bzw. timeout). Ein Wächter,
// der erst nach ListTree liefe, hinge hier bis zum 10-Minuten-Limit.
func TestFastScanHangingListTreeFallsBack(t *testing.T) {
	cases := []struct {
		name  string
		vars  func()
		cause string
		not   string
	}{
		{"Leerlauf", func() { fastScanIdleTimeout = 80 * time.Millisecond }, "tree listing idle timeout", ""},
		{"Zeitlimit", func() { fastScanTimeout = 120 * time.Millisecond; fastScanIdleTimeout = time.Hour }, "tree listing timeout", "idle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			ref := fastRefOutcome(t)
			fastScanAbortGrace = 200 * time.Millisecond
			c.vars()
			src := newFastHangFS(hangInList)
			defer src.shutdown()
			start := time.Now()
			got := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("Rückfall dauerte %v (erwartet etwa 1 s)", elapsed)
			}
			fastWantPath(t, c.name, got, "SFTP")
			fastAssertSame(t, c.name, got, ref)
			if len(got.failures) != 0 || fastHasConnectionLost(got) {
				t.Errorf("Fehler trotz Rückfall: %q", got.failures)
			}
			if got.cancelled {
				t.Error("als abgebrochen gemeldet, es war kein Esc")
			}
			if n := fastWaitAborts(&src.aborts, 1, 500*time.Millisecond); n != 0 {
				t.Errorf("AbortTreeList %d-mal, erwartet 0 (es gibt keinen Kanal)", n)
			}
			if !fastLogsContain(got, c.cause) {
				t.Errorf("Protokoll nennt die Ursache %q nicht: %q", c.cause, got.logs)
			}
			if c.not != "" && fastLogsContain(got, c.not) {
				t.Errorf("Protokoll nennt fälschlich %q: %q", c.not, got.logs)
			}
		})
	}
}

// --- B. Schnellweg je Instanz abschalten ---

// TestFastScanDisablesPerInstance - Nach Leerlauf, Zeitlimit und hängendem
// Kanal gilt für DIESE Quelle fastScanDisabled(), das Protokoll nennt "SFTP ab
// jetzt für diese Verbindung". Ein zweiter Lauf mit derselben Instanz ruft
// ListTree nicht mehr, nennt "Schnellweg für diese Verbindung abgeschaltet",
// liefert das vollständige Ergebnis und ist schnell (< 1 s - sonst kostete
// jedes weitere F5/F6 wieder die Leerlaufgrenze). Ein dritter Lauf mit einer
// ANDEREN, frischen Instanz nutzt find wieder.
func TestFastScanDisablesPerInstance(t *testing.T) {
	cases := []struct {
		name string
		vars func()
		make func() fastSwitchSrc
		// Der erste Lauf endet mit errConnectionLost (getrennter Kanal)
		lost bool
	}{
		{"Leerlauf", func() { fastScanIdleTimeout = 60 * time.Millisecond },
			func() fastSwitchSrc { s := &fastSwitchNormal{fastFS: fastSource()}; s.delay = time.Minute; return s }, false},
		{"Zeitlimit", func() { fastScanTimeout = 100 * time.Millisecond; fastScanIdleTimeout = time.Hour },
			func() fastSwitchSrc { s := &fastSwitchNormal{fastFS: fastSource()}; s.delay = time.Minute; return s }, false},
		{"ListTree hängt (Leerlauf)", func() { fastScanIdleTimeout = 60 * time.Millisecond },
			func() fastSwitchSrc { return &fastSwitchHang{fastHangFS: newFastHangFS(hangInList)} }, false},
		{"ListTree hängt (Zeitlimit)", func() { fastScanTimeout = 100 * time.Millisecond; fastScanIdleTimeout = time.Hour },
			func() fastSwitchSrc { return &fastSwitchHang{fastHangFS: newFastHangFS(hangInList)} }, false},
		{"Leser hängt (Kanal getrennt)", func() { fastScanIdleTimeout = 60 * time.Millisecond },
			func() fastSwitchSrc { return &fastSwitchHang{fastHangFS: newFastHangFS(hangInRead)} }, true},
		{"wait hängt (Kanal getrennt)", func() { fastScanIdleTimeout = 60 * time.Millisecond },
			func() fastSwitchSrc { return &fastSwitchHang{fastHangFS: newFastHangFS(hangInWait)} }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			ref := fastRefOutcome(t)
			fastScanAbortGrace = 200 * time.Millisecond
			c.vars()
			src := c.make()
			defer src.shutdown()
			if src.fastScanDisabled() {
				t.Fatal("frische Quelle schon abgeschaltet")
			}
			first := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
			if !src.fastScanDisabled() {
				t.Errorf("Schnellweg nach dem Fehlschlag nicht abgeschaltet; Protokoll %q", first.logs)
			}
			if !fastLogsContain(first, "SFTP ab jetzt für diese Verbindung") {
				t.Errorf("Protokoll meldet das Abschalten nicht: %q", first.logs)
			}
			if got := fastHasConnectionLost(first); got != c.lost {
				t.Errorf("errConnectionLost = %v, erwartet %v (%q)", got, c.lost, first.failures)
			}
			if src.listCount() != 1 {
				t.Fatalf("ListTree im ersten Lauf %d-mal, erwartet 1", src.listCount())
			}

			// Zweiter Lauf, dieselbe Instanz: kein ListTree, vollständig, schnell
			start := time.Now()
			second := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("zweiter Lauf dauerte %v (erwartet < 1 s)", elapsed)
			}
			if src.listCount() != 1 {
				t.Errorf("ListTree im zweiten Lauf erneut gerufen (%d Aufrufe)", src.listCount())
			}
			fastWantPath(t, c.name+", zweiter Lauf", second, "SFTP")
			if !fastLogsContain(second, "Schnellweg für diese Verbindung abgeschaltet") {
				t.Errorf("Protokoll des zweiten Laufs: %q", second.logs)
			}
			fastAssertSame(t, c.name+", zweiter Lauf", second, ref)
			if len(second.failures) != 0 || second.cancelled {
				t.Errorf("zweiter Lauf: Fehler %q, cancelled %v", second.failures, second.cancelled)
			}

			// Dritter Lauf, andere Instanz: find wird wieder versucht
			fresh := &fastSwitchNormal{fastFS: fastSource()}
			third := fastRunReq(t, context.Background(), fresh, false, []string{"projekt"}, false, 5*time.Second)
			if fresh.listCount() != 1 {
				t.Errorf("frische Instanz: ListTree %d-mal, erwartet 1", fresh.listCount())
			}
			fastWantPath(t, c.name+", frische Instanz", third, "find")
			fastAssertSame(t, c.name+", frische Instanz", third, ref)
			if fresh.fastScanDisabled() {
				t.Error("frische Instanz ohne Anlass abgeschaltet")
			}
		})
	}
}

// TestFastScanEscNeverDisablesWithNormalServer - Esc bei einem normal
// antwortenden Server (Leser endet bei ctx, Kanal schließt sofort): Esc allein
// schaltet den Schnellweg NIE ab, auch nicht nach der Schonfrist im
// Hintergrund. Derselbe Server nutzt danach wieder find (ListTree zum zweiten
// Mal, Weg "find"). Esc kommt abwechselnd genau beim Start von find und nach
// 30 ms; mehrfach wiederholt, damit Flattern auffällt.
func TestFastScanEscNeverDisablesWithNormalServer(t *testing.T) {
	defer fastKeepVars()()
	ref := fastRefOutcome(t)
	fastScanAbortGrace = 150 * time.Millisecond
	for i := 0; i < 6; i++ {
		src := &fastSwitchAbort{fastAbortCountFS: newFastAbortCountFS()}
		src.delay = time.Minute
		ctx, cancel := context.WithCancel(context.Background())
		if i%2 == 0 {
			src.onList = cancel
		} else {
			time.AfterFunc(30*time.Millisecond, cancel)
		}
		start := time.Now()
		o := fastRunReq(t, ctx, src, false, []string{"projekt"}, false, 3*time.Second)
		cancel()
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Lauf %d: Esc wirkte erst nach %v", i, elapsed)
		}
		if !o.cancelled {
			t.Errorf("Lauf %d: Abbruch nicht gemeldet", i)
		}
		// Die Schonfrist ablaufen lassen: der Hintergrund darf nichts abschalten
		time.Sleep(fastScanAbortGrace + 100*time.Millisecond)
		if src.fastScanDisabled() {
			t.Fatalf("Lauf %d: Esc allein hat den Schnellweg abgeschaltet; Protokoll %q", i, o.logs)
		}
		if n := src.aborts.Load(); n != 0 {
			t.Errorf("Lauf %d: AbortTreeList %d-mal, erwartet 0", i, n)
		}
		if fastLogsContain(o, "SFTP ab jetzt") {
			t.Errorf("Lauf %d: Protokoll meldet Abschalten: %q", i, o.logs)
		}
		src.shutdown()
		// Danach: dieselbe Instanz, ohne Esc und ohne Verzögerung, nutzt find
		src.delay = 0
		src.onList = nil
		again := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
		fastWantPath(t, "nach Esc", again, "find")
		fastAssertSame(t, "nach Esc", again, ref)
		if src.listCount() != 2 {
			t.Errorf("Lauf %d: ListTree %d-mal, erwartet 2 (find wieder genutzt)", i, src.listCount())
		}
	}
}

// TestFastScanEscThenHangDisables - Esc, während find hängt (ListTree
// ignoriert den Kontext): Nicht Esc, sondern das Hängen nach der Schonfrist
// schaltet den Schnellweg ab. Es gibt keinen Kanal, also KEIN AbortTreeList.
// Hängt dagegen der Kanal (Leser), trennt der Hintergrund wie bisher genau
// einmal UND schaltet ab. Esc selbst wirkt binnen einer Sekunde.
func TestFastScanEscThenHangDisables(t *testing.T) {
	cases := []struct {
		name       string
		mode       fastHangMode
		wantAborts int32
	}{
		{"ListTree hängt", hangInList, 0},
		{"Leser hängt", hangInRead, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			fastScanAbortGrace = 200 * time.Millisecond
			src := &fastSwitchHang{fastHangFS: newFastHangFS(c.mode)}
			defer src.shutdown()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(50*time.Millisecond, cancel)
			start := time.Now()
			o := fastRunReq(t, ctx, src, false, []string{"projekt"}, false, 3*time.Second)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("Esc wirkte erst nach %v", elapsed)
			}
			if !o.cancelled {
				t.Error("Abbruch nicht gemeldet")
			}
			// Nach der Schonfrist (200 ms) schaltet der Hintergrund ab
			deadline := time.Now().Add(2 * time.Second)
			for !src.fastScanDisabled() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !src.fastScanDisabled() {
				t.Error("hängender find nach Esc: Schnellweg nicht abgeschaltet")
			}
			if n := fastWaitAborts(&src.aborts, 1, 500*time.Millisecond); n != c.wantAborts {
				t.Errorf("AbortTreeList %d-mal, erwartet %d", n, c.wantAborts)
			}
		})
	}
}

// --- inheritFastScanOff ---

// fastPlainFS ist ein Dateisystem ohne fastScanSwitch.
type fastPlainFS struct{ *memFS }

// TestInheritFastScanOff - Der Abschaltzustand wandert von der alten auf die
// neue Verbindung (Neu-Verbinden): alt aus -> neu aus; alt nicht aus -> neu
// nicht aus (nie ohne Anlass abschalten). Randfälle ohne Absturz: alte oder
// neue Verbindung nil, eine Seite ohne fastScanSwitch, beide ohne; ein
// fremdes Dateisystem wird nie verändert. Auch mit dem echten sftpFS.
func TestInheritFastScanOff(t *testing.T) {
	t.Run("alt aus, neu an", func(t *testing.T) {
		oldFS, newFS := &fastSwitchNormal{fastFS: newFastFS()}, &fastSwitchNormal{fastFS: newFastFS()}
		oldFS.disableFastScanForSession()
		inheritFastScanOff(oldFS, newFS)
		if !newFS.fastScanDisabled() {
			t.Error("neue Verbindung nicht abgeschaltet")
		}
		if !oldFS.fastScanDisabled() {
			t.Error("alte Verbindung verändert")
		}
	})
	t.Run("alt nicht aus", func(t *testing.T) {
		oldFS, newFS := &fastSwitchNormal{fastFS: newFastFS()}, &fastSwitchNormal{fastFS: newFastFS()}
		inheritFastScanOff(oldFS, newFS)
		if newFS.fastScanDisabled() || oldFS.fastScanDisabled() {
			t.Error("ohne Anlass abgeschaltet")
		}
	})
	t.Run("neu war schon aus, alt nicht", func(t *testing.T) {
		oldFS, newFS := &fastSwitchNormal{fastFS: newFastFS()}, &fastSwitchNormal{fastFS: newFastFS()}
		newFS.disableFastScanForSession()
		inheritFastScanOff(oldFS, newFS)
		if !newFS.fastScanDisabled() {
			t.Error("bestehendes Abschalten der neuen Verbindung zurückgenommen")
		}
	})
	t.Run("echter sftpFS", func(t *testing.T) {
		oldFS, newFS := &sftpFS{}, &sftpFS{}
		inheritFastScanOff(oldFS, newFS)
		if newFS.fastScanDisabled() {
			t.Error("ohne Anlass abgeschaltet")
		}
		oldFS.disableFastScanForSession()
		inheritFastScanOff(oldFS, newFS)
		if !newFS.fastScanDisabled() {
			t.Error("sftpFS: Zustand nicht übernommen")
		}
	})
	t.Run("nil und ohne Schalter", func(t *testing.T) {
		on := &fastSwitchNormal{fastFS: newFastFS()}
		off := &fastSwitchNormal{fastFS: newFastFS()}
		off.disableFastScanForSession()
		plain := &fastPlainFS{memFS: newMemFS("/")}
		// kein Absturz, niemand wird verändert
		inheritFastScanOff(nil, nil)
		inheritFastScanOff(nil, on)
		inheritFastScanOff(off, nil)
		inheritFastScanOff(plain, on)
		inheritFastScanOff(off, plain)
		inheritFastScanOff(plain, plain)
		if on.fastScanDisabled() {
			t.Error("Verbindung ohne Anlass abgeschaltet")
		}
	})
}

// reconnSwitchFS ist ein reconnFS (verloren schaltbar) mit fastScanSwitch.
type reconnSwitchFS struct {
	*reconnFS
	off atomic.Bool
}

func (s *reconnSwitchFS) disableFastScanForSession() { s.off.Store(true) }
func (s *reconnSwitchFS) fastScanDisabled() bool     { return s.off.Load() }

// TestReconnectInheritsFastScanOff - Neu-Verbinden über fmReadPanelCmd: Eine
// verlorene Alt-Verbindung mit abgeschaltetem Schnellweg liefert eine neue
// Verbindung, die ebenfalls abgeschaltet ist (sonst hinge das nächste F5/F6
// wieder bis zur Leerlaufgrenze und trennte erneut). Eine Alt-Verbindung ohne
// Abschalten vererbt nichts. Ganzer Ablauf über Strg+R: das neue Dateisystem
// im Fenster trägt den Zustand.
func TestReconnectInheritsFastScanOff(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		name := "alt nicht abgeschaltet"
		if disabled {
			name = "alt abgeschaltet"
		}
		t.Run(name, func(t *testing.T) {
			m, c := openReconnectManager(t)
			old := &reconnSwitchFS{reconnFS: panelRecon(t, m, 0)}
			old.lost.Store(true)
			if disabled {
				old.disableFastScanForSession()
			}
			m.fm.panels[0].fs = old
			var fresh *reconnSwitchFS
			c.setScript(func(n int, conn Connection, password string) (remoteFS, error) {
				fresh = &reconnSwitchFS{reconnFS: newReconnFS()}
				return fresh, nil
			})
			// Einzelschritt: die Nachricht von fmReadPanelCmd
			msg, ok := m.fmReadPanelCmd(0, m.fm.panels[0].path, "", 0, nil, "")().(fmDirLoadedMsg)
			if !ok || msg.newFS == nil {
				t.Fatalf("keine neue Verbindung in der Nachricht: %+v (ok=%v)", msg, ok)
			}
			got, isSwitch := msg.newFS.(*reconnSwitchFS)
			if !isSwitch || got != fresh {
				t.Fatalf("newFS = %T, erwartet die frische Verbindung", msg.newFS)
			}
			if got.fastScanDisabled() != disabled {
				t.Errorf("neue Verbindung fastScanDisabled = %v, erwartet %v", got.fastScanDisabled(), disabled)
			}
			if old.fastScanDisabled() != disabled {
				t.Errorf("alte Verbindung verändert: %v", old.fastScanDisabled())
			}

			// Ganzer Ablauf über Strg+R
			m, c = openReconnectManager(t)
			old = &reconnSwitchFS{reconnFS: panelRecon(t, m, 1)}
			old.lost.Store(true)
			if disabled {
				old.disableFastScanForSession()
			}
			m.fm.panels[1].fs = old
			c.setScript(func(n int, conn Connection, password string) (remoteFS, error) {
				fresh = &reconnSwitchFS{reconnFS: newReconnFS()}
				return fresh, nil
			})
			m.fm.active = 1
			m = pressRun(t, m, "ctrl+r")
			adopted, ok := m.fm.panels[1].fs.(*reconnSwitchFS)
			if !ok || adopted != fresh {
				t.Fatalf("Fenster hat %T statt der frischen Verbindung", m.fm.panels[1].fs)
			}
			if adopted.fastScanDisabled() != disabled {
				t.Errorf("im Fenster: fastScanDisabled = %v, erwartet %v", adopted.fastScanDisabled(), disabled)
			}
		})
	}
}

// --- Der done-Zweig von runTreeList ---

// errFastSpinFailed - der Fehler, mit dem fastSwitchSpin ListTree beendet.
var errFastSpinFailed = errors.New("spin: exec abgelehnt")

// fastSwitchSpin ist eine Quelle, deren ListTree RECHNET (ohne zu blockieren
// oder abzugeben), bis die gewünschte Zeit vorbei ist, und dann sofort mit
// Fehler zurückkehrt. Mit GOMAXPROCS(1) erreicht das den done-Zweig von
// runTreeList (siehe fastSpinRun): Die Goroutine von ListTree hält den einzigen
// Prozessor, ein Zeitgeber (Zeitlimit, Leerlauf-Ticker, Esc) ist in dieser Zeit
// fällig, kann aber nicht laufen. Sie sendet done, der Hauptlauf ist damit
// schon für den done-Fall bereit, und erst NACH ihrem Ende läuft der Zeitgeber
// und beendet den Kontext - noch bevor der Hauptlauf wieder dran ist.
type fastSwitchSpin struct {
	*fastFS
	off   atomic.Bool
	lists atomic.Int32
	// start wird als Erstes in ListTree gerufen (z. B. Esc-Zeitgeber anlegen)
	start func()
	// yield gibt den Prozessor einmal ab (damit der Leerlauf-Wächter anläuft)
	yield bool
	// until liefert, bis wann gerechnet wird
	until func(ctx context.Context) time.Time
}

func (s *fastSwitchSpin) disableFastScanForSession() { s.off.Store(true) }
func (s *fastSwitchSpin) fastScanDisabled() bool     { return s.off.Load() }
func (s *fastSwitchSpin) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	s.lists.Add(1)
	if s.start != nil {
		s.start()
	}
	if s.yield {
		runtime.Gosched()
	}
	for end := s.until(ctx); time.Now().Before(end); {
	}
	return nil, nil, errFastSpinFailed
}

// fastSpinMargin - so lange rechnet fastSwitchSpin über den Zeitpunkt des
// Zeitgebers hinaus (muss ihn sicher überschreiten)
const fastSpinMargin = 1500 * time.Microsecond

// fastSpinAttempts - so oft wiederholt ein Test den Lauf mit festgelegter
// Scheduler-Reihenfolge, wenn sie einmal nicht eintrat
const fastSpinAttempts = 60

// fastSpinRuns - so viele Läufe mit der erwarteten Reihenfolge müssen
// bestanden werden. Mehrere, weil sich von außen nicht sehen lässt, ob der
// done-Zweig oder der ctx.Done-Zweig die Arbeit tat: Eine Gegenprobe (disableFastScan
// im done-Zweig entfernt) scheiterte in 600 von 600 Läufen, also in einem Lauf mit
// einer Wahrscheinlichkeit q >= 0,995 (95 %); bei 5 Läufen übersieht sie eine
// Entfernung höchstens mit (1-q)^5 = 3e-12.
const fastSpinRuns = 5

// fastSpinOrderMissed meldet, ob ein Lauf mit fastSwitchSpin die erwartete
// Reihenfolge verfehlt hat: ListTree kehrte zurück, bevor der Zeitgeber den
// Kontext beendete - runTreeList gibt dann den rohen Fehler weiter (kein
// Abschalten, ganz richtig). Dann gilt der Lauf nicht.
func fastSpinOrderMissed(o fastOutcome) bool {
	return fastLogsContain(o, errFastSpinFailed.Error())
}

// TestFastScanDoneBranchDisablesAfterTimeoutOrIdle - Der erste Fall von
// select in runTreeList (done): ListTree kehrt mit Fehler zurück, und
// ctx (Zeitlimit bzw. Leerlauf) ist beim Auswerten schon beendet. Auch DORT muss
// der Schnellweg für die Verbindung abgeschaltet werden (disableFastScan) und der
// Lauf per SFTP mit vollständigem Ergebnis weitergehen. Bisher blieb jede
// Entfernung von disableFastScan in diesem Zweig unbemerkt, weil der ctx.Done-Zweig
// dasselbe tut: Welcher Fall zählt, entscheidet sonst der Zufall von select.
// Deshalb läuft der Test mit GOMAXPROCS(1) und einem rechnenden ListTree
// (fastSwitchSpin): Dann legt der Scheduler die Reihenfolge fest (Zeitgeber
// laufen erst nach dem Ende der rechnenden Goroutine, und ihre neue Goroutine
// überholt den schon bereiten Hauptlauf). In rund 3 von 100 Läufen fällt sie
// trotzdem anders (ListTree-Fehler bei noch lebendem ctx): Solche Läufe zählen
// nicht (fastSpinOrderMissed) und werden wiederholt;
// fastSpinRuns (5) gelungene Läufe sind nötig, bis zu fastSpinAttempts (60) werden versucht. Zusätzlich:
// ein zweiter Lauf ruft ListTree nicht mehr.
func TestFastScanDoneBranchDisablesAfterTimeoutOrIdle(t *testing.T) {
	cases := []struct {
		name  string
		vars  func()
		yield bool
		until func(ctx context.Context) time.Time
		cause string
	}{
		{"Zeitlimit", func() { fastScanTimeout = 3 * time.Millisecond; fastScanIdleTimeout = time.Hour }, false,
			func(ctx context.Context) time.Time { dl, _ := ctx.Deadline(); return dl.Add(fastSpinMargin) },
			"tree listing timeout"},
		{"Leerlauf", func() { fastScanIdleTimeout = 4 * time.Millisecond }, true,
			func(ctx context.Context) time.Time {
				return time.Now().Add(4*time.Millisecond + 2*time.Millisecond + fastSpinMargin)
			},
			"tree listing idle timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
			ref := fastRefOutcome(t)
			c.vars()
			lucky := 0
			for attempt := 0; attempt < fastSpinAttempts && lucky < fastSpinRuns; attempt++ {
				src := &fastSwitchSpin{fastFS: fastSource(), yield: c.yield, until: c.until}
				got := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
				if fastSpinOrderMissed(got) {
					continue
				}
				if !src.fastScanDisabled() {
					t.Errorf("Schnellweg nach ListTree-Fehler bei beendetem ctx nicht abgeschaltet; Protokoll %q", got.logs)
				}
				if !fastLogsContain(got, "SFTP ab jetzt für diese Verbindung") || !fastLogsContain(got, c.cause) {
					t.Errorf("Protokoll meldet Abschalten/Ursache %q nicht: %q", c.cause, got.logs)
				}
				fastWantPath(t, c.name, got, "SFTP")
				fastAssertSame(t, c.name, got, ref)
				if len(got.failures) != 0 || got.cancelled || fastHasConnectionLost(got) {
					t.Errorf("Fehler trotz Rückfall: %q (cancelled %v)", got.failures, got.cancelled)
				}
				if src.lists.Load() != 1 {
					t.Fatalf("ListTree im ersten Lauf %d-mal, erwartet 1", src.lists.Load())
				}
				// Zweiter Lauf: kein ListTree mehr
				second := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
				if src.lists.Load() != 1 {
					t.Errorf("ListTree im zweiten Lauf erneut gerufen (%d Aufrufe)", src.lists.Load())
				}
				fastAssertSame(t, c.name+", zweiter Lauf", second, ref)
				lucky++
			}
			if lucky < fastSpinRuns {
				t.Fatalf("Scheduler-Reihenfolge nur in %d von %d Läufen erreicht (nötig %d): Der Test prüft den done-Zweig nicht mehr", lucky, fastSpinAttempts, fastSpinRuns)
			}
		})
	}
}

// TestFastScanDoneBranchEscNeverDisables - Gegenstück zum Test davor: ListTree
// kehrt mit Fehler zurück, und im selben Augenblick ist der Kontext des Laufs
// beendet (Esc, ein Zeitgeber ruft cancel). Dann ist es Esc und nicht der
// Server: Der done-Zweig darf den Schnellweg NICHT abschalten (r.ctx.Err() == nil
// ist die Bedingung), der Lauf meldet den Abbruch. Gleiche Anordnung und gleiche
// Wiederholung wie dort (die Reihenfolge fällt selten anders: dann gibt
// runTreeList den rohen Fehler weiter und Esc kommt erst später).
func TestFastScanDoneBranchEscNeverDisables(t *testing.T) {
	defer fastKeepVars()()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	lucky := 0
	for attempt := 0; attempt < fastSpinAttempts && lucky < fastSpinRuns; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		var escAt time.Time
		src := &fastSwitchSpin{fastFS: fastSource()}
		src.start = func() {
			escAt = time.Now().Add(3 * time.Millisecond)
			time.AfterFunc(3*time.Millisecond, cancel)
		}
		src.until = func(context.Context) time.Time { return escAt.Add(fastSpinMargin) }
		got := fastRunReq(t, ctx, src, false, []string{"projekt"}, false, 5*time.Second)
		cancel()
		if fastSpinOrderMissed(got) {
			continue
		}
		if src.lists.Load() != 1 {
			t.Fatalf("ListTree %d-mal gerufen, erwartet 1 (Esc kam vor find?)", src.lists.Load())
		}
		if src.fastScanDisabled() {
			t.Errorf("Esc allein hat den Schnellweg abgeschaltet; Protokoll %q", got.logs)
		}
		if fastLogsContain(got, "SFTP ab jetzt") {
			t.Errorf("Protokoll meldet Abschalten nach Esc: %q", got.logs)
		}
		if !got.cancelled {
			t.Error("Abbruch nicht gemeldet")
		}
		lucky++
	}
	if lucky < fastSpinRuns {
		t.Fatalf("Scheduler-Reihenfolge nur in %d von %d Läufen erreicht (nötig %d): Der Test prüft den done-Zweig nicht mehr", lucky, fastSpinAttempts, fastSpinRuns)
	}
}
