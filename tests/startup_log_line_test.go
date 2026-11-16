// Tests für die Start- und Ende-Zeile im Log (~/.ssh-easy/ssh-easy.log).
//
// Hintergrund: Die Startzeile enthielt bisher nur Betriebssystem und
// Architektur. Für die Fehlersuche stehen jetzt Version (v0.<Build>) und
// Prozess-ID drin, die Endezeile trägt dieselben Angaben. So lassen sich
// Start und Ende eines Laufs im Log eindeutig zuordnen.
//
// Prüft:
//   - formatStartLogLine: exaktes Format, Build-Trimmen, Fallback "dev"
//   - formatExitLogLine: exaktes Format, Build-Trimmen, Fallback "dev"
//   - Aufruf mit den echten Werten (readBuildNumber, os.Getpid)
//   - Quelltext von main.go ruft beide Funktionen mit buildNumber auf,
//     die alten Logzeilen sind weg und buildNumber wird VOR der
//     Startzeile gelesen (sonst wäre die Nummer beim Loggen noch leer)
//   - Gegenprobe: checkMainLogLines schlägt beim Altstand an
//
// @author Kurt Ingwer
// @date   2026-11-16 16:45

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestFormatStartLogLineTable: Tabelle mit exakten Erwartungen.
// Prüft normale Nummer, Zeilenumbruch/Leerzeichen um die Nummer (build.txt
// endet oft mit "\n"), Fallback "dev" bei leerem/Whitespace-Build sowie
// PID 0 und eine große PID.
func TestFormatStartLogLineTable(t *testing.T) {
	tests := []struct {
		name   string
		build  string
		goos   string
		goarch string
		pid    int
		want   string
	}{
		{"normal windows", "91", "windows", "amd64", 1234,
			"ssh-easy v0.91 gestartet (windows/amd64, PID 1234)"},
		{"build mit Zeilenumbruch", " 91\n", "windows", "amd64", 1234,
			"ssh-easy v0.91 gestartet (windows/amd64, PID 1234)"},
		{"build mit CRLF (Windows-Editor)", "91\r\n", "linux", "arm64", 5,
			"ssh-easy v0.91 gestartet (linux/arm64, PID 5)"},
		{"build dev", "dev", "linux", "arm64", 7,
			"ssh-easy Build dev gestartet (linux/arm64, PID 7)"},
		{"build leer", "", "linux", "arm64", 7,
			"ssh-easy Build dev gestartet (linux/arm64, PID 7)"},
		{"build nur Leerzeichen", "  \t\n", "linux", "arm64", 7,
			"ssh-easy Build dev gestartet (linux/arm64, PID 7)"},
		{"PID 0", "91", "linux", "amd64", 0,
			"ssh-easy v0.91 gestartet (linux/amd64, PID 0)"},
		{"große PID", "91", "linux", "amd64", 99999,
			"ssh-easy v0.91 gestartet (linux/amd64, PID 99999)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatStartLogLine(tc.build, tc.goos, tc.goarch, tc.pid)
			if got != tc.want {
				t.Errorf("formatStartLogLine(%q, %q, %q, %d) = %q, erwartet %q",
					tc.build, tc.goos, tc.goarch, tc.pid, got, tc.want)
			}
			// Eine Logzeile darf nie über mehrere Zeilen laufen
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("Startzeile enthält Zeilenumbruch: %q", got)
			}
		})
	}
}

// TestFormatStartLogLineRealValues: Mit den echten Laufzeitwerten muss die
// Zeile die eingebettete Build-Nummer als "v0.<Build>" und die eigene PID
// enthalten (außer beim Fallback "dev", dann steht "Build dev" da).
func TestFormatStartLogLineRealValues(t *testing.T) {
	build := readBuildNumber()
	line := formatStartLogLine(build, "linux", "amd64", os.Getpid())
	if build == "dev" {
		if !strings.Contains(line, "Build dev") {
			t.Errorf("Startzeile ohne 'Build dev': %q", line)
		}
	} else if !strings.Contains(line, "v0."+build) {
		t.Errorf("Startzeile ohne 'v0.%s': %q", build, line)
	}
	if want := fmt.Sprintf("PID %d", os.Getpid()); !strings.Contains(line, want) {
		t.Errorf("Startzeile ohne %q: %q", want, line)
	}
	if !strings.HasPrefix(line, "ssh-easy ") || !strings.Contains(line, " gestartet ") {
		t.Errorf("Startzeile hat unerwartetes Format: %q", line)
	}
}

// TestFormatExitLogLineTable: Tabelle mit exakten Erwartungen für die
// Endezeile; gleiche Randfälle wie bei der Startzeile (Zeilenumbruch,
// leerer Build, PID 0, große PID).
func TestFormatExitLogLineTable(t *testing.T) {
	tests := []struct {
		name  string
		build string
		pid   int
		want  string
	}{
		{"normal", "91", 1234, "ssh-easy v0.91 beendet (PID 1234)"},
		{"build mit Zeilenumbruch", " 91\n", 1234, "ssh-easy v0.91 beendet (PID 1234)"},
		{"build mit CRLF", "91\r\n", 5, "ssh-easy v0.91 beendet (PID 5)"},
		{"build dev", "dev", 7, "ssh-easy Build dev beendet (PID 7)"},
		{"build leer", "", 7, "ssh-easy Build dev beendet (PID 7)"},
		{"build nur Leerzeichen", "   ", 7, "ssh-easy Build dev beendet (PID 7)"},
		{"PID 0", "91", 0, "ssh-easy v0.91 beendet (PID 0)"},
		{"große PID", "91", 99999, "ssh-easy v0.91 beendet (PID 99999)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatExitLogLine(tc.build, tc.pid)
			if got != tc.want {
				t.Errorf("formatExitLogLine(%q, %d) = %q, erwartet %q",
					tc.build, tc.pid, got, tc.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("Endezeile enthält Zeilenumbruch: %q", got)
			}
		})
	}
}

// TestFormatExitLogLineRealValues: Mit den echten Laufzeitwerten muss die
// Endezeile Build und PID enthalten und zur Startzeile desselben Laufs
// passen (gleicher Build-Teil, gleiche PID), sonst ließen sich beide im
// Log nicht zuordnen.
func TestFormatExitLogLineRealValues(t *testing.T) {
	build := readBuildNumber()
	pid := os.Getpid()
	exit := formatExitLogLine(build, pid)
	start := formatStartLogLine(build, "linux", "amd64", pid)
	if build != "dev" && !strings.Contains(exit, "v0."+build) {
		t.Errorf("Endezeile ohne 'v0.%s': %q", build, exit)
	}
	if want := fmt.Sprintf("PID %d", pid); !strings.Contains(exit, want) {
		t.Errorf("Endezeile ohne %q: %q", want, exit)
	}
	// Gemeinsamer Anfang "ssh-easy <Version>" bei Start und Ende
	prefix := strings.SplitN(exit, " beendet", 2)[0]
	if !strings.HasPrefix(start, prefix+" gestartet") {
		t.Errorf("Start- und Endezeile passen nicht zusammen: %q / %q", start, exit)
	}
}

// oldMainLogSnippet ist ein Ausschnitt aus main.go vor Build 91 (Stand
// Build 90): alte Logzeilen, Build-Nummer erst NACH der Startzeile.
// Dient als dauerhafte Gegenprobe für checkMainLogLines.
const oldMainLogSnippet = `
	logger := NewLogger()
	logger.Info("ssh-easy gestartet (%s/%s)", runtime.GOOS, runtime.GOARCH)

	// Build-Nummer lesen
	buildNumber := readBuildNumber()
	...
	logger.Info("ssh-easy beendet")
`

// checkMainLogLines prüft den Quelltext von main.go auf die neuen Logzeilen
// und liefert eine Liste von Fehlern (leer = alles in Ordnung).
//
// @param src - Quelltext von main.go
// @return []string - Beschreibung jedes gefundenen Verstoßes
// @date   2026-11-16 16:45
func checkMainLogLines(src string) []string {
	var problems []string
	const startCall = `formatStartLogLine(buildNumber, runtime.GOOS, runtime.GOARCH, os.Getpid())`
	const exitCall = `formatExitLogLine(buildNumber, os.Getpid())`
	const readCall = `buildNumber := readBuildNumber()`

	startIdx := strings.Index(src, startCall)
	if startIdx < 0 {
		problems = append(problems, "Aufruf "+startCall+" fehlt")
	}
	if !strings.Contains(src, exitCall) {
		problems = append(problems, "Aufruf "+exitCall+" fehlt")
	}
	if strings.Contains(src, `"ssh-easy gestartet (%s/%s)"`) {
		problems = append(problems, "alte Startzeile \"ssh-easy gestartet (%s/%s)\" noch vorhanden")
	}
	if strings.Contains(src, `logger.Info("ssh-easy beendet")`) {
		problems = append(problems, "alte Endezeile logger.Info(\"ssh-easy beendet\") noch vorhanden")
	}
	readIdx := strings.Index(src, readCall)
	if readIdx < 0 {
		problems = append(problems, "Zeile "+readCall+" fehlt")
	} else if startIdx >= 0 && readIdx > startIdx {
		problems = append(problems, "buildNumber wird erst NACH der Startzeile gelesen")
	}
	return problems
}

// TestMainLogLinesSource: Der echte Quelltext von main.go (im Testlauf liegt
// er im Arbeitsverzeichnis) muss die neuen Aufrufe enthalten, die alten
// Logzeilen dürfen nicht mehr vorkommen, und buildNumber muss vor der
// Startzeile gelesen werden.
func TestMainLogLinesSource(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("main.go nicht lesbar: %v", err)
	}
	for _, p := range checkMainLogLines(string(data)) {
		t.Error(p)
	}
}

// TestMainLogLinesSourceCounterCheck: Gegenprobe. Der Altstand (alte
// Logzeilen, buildNumber nach der Startzeile) muss von checkMainLogLines
// beanstandet werden - sonst würde TestMainLogLinesSource nichts prüfen.
// Ein Mischzustand (neue Aufrufe, aber falsche Reihenfolge) wird ebenfalls
// erkannt.
func TestMainLogLinesSourceCounterCheck(t *testing.T) {
	problems := checkMainLogLines(oldMainLogSnippet)
	// Altstand: beide Aufrufe fehlen, beide alten Zeilen vorhanden
	if len(problems) != 4 {
		t.Errorf("Altstand: 4 Verstöße erwartet, %d gefunden: %v", len(problems), problems)
	}

	// Neue Aufrufe, aber buildNumber erst danach gelesen
	wrongOrder := `
	logger.Info("%s", formatStartLogLine(buildNumber, runtime.GOOS, runtime.GOARCH, os.Getpid()))
	buildNumber := readBuildNumber()
	logger.Info("%s", formatExitLogLine(buildNumber, os.Getpid()))
`
	problems = checkMainLogLines(wrongOrder)
	if len(problems) != 1 || !strings.Contains(problems[0], "NACH") {
		t.Errorf("Falsche Reihenfolge nicht (allein) erkannt: %v", problems)
	}

	// Korrekter Zustand: keine Beanstandung
	good := `
	buildNumber := readBuildNumber()
	logger.Info("%s", formatStartLogLine(buildNumber, runtime.GOOS, runtime.GOARCH, os.Getpid()))
	logger.Info("%s", formatExitLogLine(buildNumber, os.Getpid()))
`
	if problems = checkMainLogLines(good); len(problems) != 0 {
		t.Errorf("Korrekter Zustand beanstandet: %v", problems)
	}
}
