// Paket main - Tests für das Windows-Setup-Skript (ssh-easy-setup.nsi)
//
// Das Setup selbst läuft nur unter Windows. Diese Tests prüfen deshalb das
// NSIS-Skript als Text auf Fehler, die bereits aufgetreten sind:
//   - Build 82: Lief die alte ssh-easy.exe noch, blieb sie liegen. Das neue
//     Programm landete als ssh-easy-windows-amd64.exe daneben, das Setup
//     meldete trotzdem Erfolg, und der Nutzer hing auf Build 81 fest.
//   - build.sh übergab -DVERSION/-DBUILD, das Skript las aber
//     PRODUCT_VERSION/PRODUCT_BUILD: Windows zeigte „0.15.0 (Build 15)“.
//   - Der alte Deinstaller lief ohne _?= - NSIS kopiert ihn dann in ein
//     Temp-Verzeichnis und ExecWait wartet nicht auf ihn. Er konnte so die
//     gerade installierte Version wieder löschen.
//
// Der echte Ablauf unter Wine: debugging/installer_wine_check.sh
//
// @author Kurt Ingwer
// @date   2026-10-27 15:30
package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// readInstallerScript liest das NSIS-Skript aus dem Projektverzeichnis.
// Die Tests laufen als Kopie in src/, das Skript liegt eine Ebene höher.
//
// @param t - Testkontext
// @return string - Inhalt von ssh-easy-setup.nsi
// @date   2026-10-27 15:30
func readInstallerScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../ssh-easy-setup.nsi")
	if err != nil {
		t.Fatalf("ssh-easy-setup.nsi nicht lesbar: %v", err)
	}
	return string(data)
}

// nsisCodeLines liefert die Zeilen des Skripts ohne Kommentare (; und #),
// damit Erklärungen im Kommentar keine Prüfung erfüllen oder verletzen.
//
// @param script - Skriptinhalt
// @return []string - Codezeilen (getrimmt, ohne Leerzeilen)
// @date   2026-10-27 15:30
func nsisCodeLines(script string) []string {
	var lines []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// TestInstallerExtractsUnderFinalName prüft, dass das Setup die Programmdatei
// direkt als ssh-easy.exe entpackt. Ein späteres Rename scheitert still, wenn
// die alte ssh-easy.exe noch existiert (z.B. weil sie läuft) - dann bleibt die
// alte Version installiert.
//
// @date 2026-10-27 15:30
func TestInstallerExtractsUnderFinalName(t *testing.T) {
	lines := nsisCodeLines(readInstallerScript(t))
	extracts := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "File ") {
			if !strings.Contains(line, `/oname=$INSTDIR\ssh-easy.exe`) {
				t.Errorf("File muss direkt nach $INSTDIR\\ssh-easy.exe entpacken: %s", line)
			}
			extracts++
		}
		if strings.HasPrefix(line, "Rename ") && strings.Contains(line, "ssh-easy-windows-") {
			t.Errorf("kein Umbenennen der entpackten Binary (scheitert still bei vorhandener ssh-easy.exe): %s", line)
		}
	}
	if extracts == 0 {
		t.Fatal("keine File-Anweisung für die Programmdatei gefunden")
	}
}

// TestInstallerHandlesRunningProgram prüft, dass das Setup eine noch
// laufende (gesperrte) ssh-easy.exe beiseite räumt statt sie stehen zu
// lassen: Windows erlaubt das Umbenennen einer laufenden EXE. Gelingt auch
// das nicht, muss das Setup mit Fehler abbrechen statt Erfolg zu melden.
//
// @date 2026-10-27 15:30
func TestInstallerHandlesRunningProgram(t *testing.T) {
	code := strings.Join(nsisCodeLines(readInstallerScript(t)), "\n")
	if !strings.Contains(code, `Rename "$INSTDIR\ssh-easy.exe" `) {
		t.Error("gesperrte ssh-easy.exe muss beiseite umbenannt werden (Windows erlaubt das bei laufenden EXEs)")
	}
	// Fester Ausweichname scheitert, wenn eine früher beiseite gelegte EXE
	// noch läuft (zwei Instanzen, Review-Fund Build 83): Name mit Zähler
	if strings.Contains(code, `Rename "$INSTDIR\ssh-easy.exe" "$INSTDIR\ssh-easy.exe.old"`) {
		t.Error("fester Ausweichname ssh-easy.exe.old - scheitert, wenn die dort liegende alte EXE noch läuft")
	}
	if !strings.Contains(code, `Delete "$INSTDIR\ssh-easy.exe.old*"`) {
		t.Error("beiseite gelegte EXEs (ssh-easy.exe.old*) werden nicht per Platzhalter aufgeräumt")
	}
	if !strings.Contains(code, "SetErrorLevel") || !strings.Contains(code, "Abort") {
		t.Error("lässt sich die alte EXE weder löschen noch umbenennen, muss das Setup mit Fehlercode abbrechen")
	}
	// Der Ausweg im stillen Modus (Auto-Update) darf keine Rückfrage blockieren
	if !regexp.MustCompile(`MessageBox [^\n]*/SD ID`).MatchString(code) {
		t.Error("Rückfragen brauchen eine /SD-Vorgabe für den stillen Modus (/S)")
	}
	// Reste früherer, gescheiterter Installationen werden entfernt
	for _, stray := range []string{`$INSTDIR\ssh-easy-windows-amd64.exe`, `$INSTDIR\ssh-easy-windows-arm64.exe`} {
		if !strings.Contains(code, `Delete "`+stray+`"`) {
			t.Errorf("Rest einer gescheiterten Installation wird nicht entfernt: %s", stray)
		}
	}
}

// TestInstallerUsesBuildShDefines prüft, dass jedes -D, das build.sh an
// makensis übergibt, im Skript auch ausgewertet wird. Sonst greifen die
// Vorgaben im Skript (früher „0.15.0 (Build 15)“ in „Apps & Features“).
//
// @date 2026-10-27 15:30
func TestInstallerUsesBuildShDefines(t *testing.T) {
	buildSh, err := os.ReadFile("../build.sh")
	if err != nil {
		t.Fatalf("build.sh nicht lesbar: %v", err)
	}
	script := readInstallerScript(t)
	names := map[string]bool{}
	for _, line := range strings.Split(string(buildSh), "\n") {
		if !strings.Contains(line, "makensis") {
			continue
		}
		for _, m := range regexp.MustCompile(`-D([A-Z_]+)=`).FindAllStringSubmatch(line, -1) {
			names[m[1]] = true
		}
	}
	if len(names) == 0 {
		t.Fatal("keine makensis-Aufrufe mit -D in build.sh gefunden")
	}
	for name := range names {
		if !strings.Contains(script, "${"+name+"}") {
			t.Errorf("build.sh übergibt -D%s, das Skript verwendet ${%s} aber nie", name, name)
		}
	}
}

// TestInstallerWaitsForOldUninstaller prüft, dass der alte Deinstaller mit
// _?= aufgerufen wird. Ohne _?= kopiert NSIS ihn in ein Temp-Verzeichnis und
// beendet den ersten Prozess sofort - ExecWait wartet dann nicht, und der
// Deinstaller kann die frisch installierten Dateien wieder löschen.
//
// @date 2026-10-27 15:30
func TestInstallerWaitsForOldUninstaller(t *testing.T) {
	found := false
	for _, line := range nsisCodeLines(readInstallerScript(t)) {
		if !strings.HasPrefix(line, "ExecWait") {
			continue
		}
		found = true
		if !strings.Contains(line, "_?=") {
			t.Errorf("Deinstaller ohne _?= - ExecWait wartet nicht auf ihn: %s", line)
		}
	}
	if !found {
		t.Fatal("kein Aufruf des alten Deinstallers (ExecWait) gefunden")
	}
}

// TestInstallerUninstallsOnlyAfterCheck prüft die Reihenfolge in der
// Installations-Section: erst prüfen, ob sich die alte EXE ersetzen lässt
// (Abbruch hier ist folgenlos), dann den alten Deinstaller, dann entpacken.
// Lief der Deinstaller schon in .onInit, blieb nach einem Abbruch eine
// Installation ohne Startmenü, Registry und Uninstall.exe zurück
// (Review-Fund Build 83). Auch Abbrechen auf der Willkommensseite
// deinstallierte bisher schon die alte Version.
//
// @date 2026-10-27 16:10
func TestInstallerUninstallsOnlyAfterCheck(t *testing.T) {
	lines := nsisCodeLines(readInstallerScript(t))
	// Funktionsrümpfe sammeln: Name -> Codezeilen
	bodies := map[string][]string{}
	current := ""
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Function "):
			current = strings.TrimSpace(strings.TrimPrefix(line, "Function "))
		case line == "FunctionEnd":
			current = ""
		case current != "":
			bodies[current] = append(bodies[current], line)
		}
	}
	for _, line := range bodies[".onInit"] {
		if strings.HasPrefix(line, "ExecWait") {
			t.Errorf(".onInit darf den alten Deinstaller nicht starten (vor jeder Prüfung): %s", line)
		}
	}

	// Section SecMain: Reihenfolge der Schritte
	inSection := false
	order := []string{}
	for _, line := range lines {
		if strings.HasPrefix(line, "Section ") && strings.Contains(line, "SecMain") {
			inSection = true
			continue
		}
		if line == "SectionEnd" {
			inSection = false
		}
		if !inSection {
			continue
		}
		switch {
		case line == "Call RemoveOldBinary":
			order = append(order, "check")
		case strings.HasPrefix(line, "Call "):
			name := strings.TrimPrefix(line, "Call ")
			for _, body := range bodies[name] {
				if strings.HasPrefix(body, "ExecWait") {
					order = append(order, "uninstall")
					break
				}
			}
		case strings.HasPrefix(line, "ExecWait"):
			order = append(order, "uninstall")
		case strings.HasPrefix(line, "File "):
			order = append(order, "extract")
		}
	}
	if got := strings.Join(order, ","); got != "check,uninstall,extract" {
		t.Errorf("Reihenfolge in SecMain = %q, erwartet \"check,uninstall,extract\"", got)
	}
}
