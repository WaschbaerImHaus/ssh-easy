// Paket main - Tests: deployKeyScript und removeKeyScript unter verschiedenen Login-Shells
//
// Hintergrund: sshd führt jeden Befehl als "<Login-Shell> -c "<Befehl>"" aus.
// Die Login-Shell des Servers kann sh, dash, bash, zsh, ksh, fish, tcsh oder csh
// sein. Beide Skripte stehen deshalb in "sh -c '...'": Die Login-Shell sieht nur
// einen einzigen, einfach angeführten Block. Dabei gibt es zwei bekannte Fallen:
//   - csh/tcsh expandieren "!x" (History) auch in einfachen Anführungszeichen
//     und brechen bei Zeilenumbrüchen innerhalb von Quotes ab ("Unmatched '").
//   - fish macht innerhalb einfacher Anführungszeichen aus "\\" ein "\" und
//     aus "\'" ein "'" (Backslash-Paare werden halbiert).
//
// Diese Tests starten statt /bin/sh die jeweilige Login-Shell mit "-c" und
// prüfen das Ergebnis auf dem Dateisystem. Fehlt eine Shell auf dem Rechner
// (exec.LookPath), wird der Teilfall übersprungen; unter Windows entfällt alles.
// Die gleichnamigen Gegenproben (Mutanten des echten Skripts) beweisen, dass
// der Runner die Fallen wirklich erkennt, und dass der Test nicht nur "grün"
// ist, weil die Shell die Fallen zufällig nicht hat.
//
// Wiederverwendet aus key_scripts_locale_test.go: ownKeyB64, ownKeyLine.
//
// @author Kurt Ingwer
// @date   2026-11-12 20:30
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// loginShellNames sind die Shells, unter denen die Skripte laufen müssen.
var loginShellNames = []string{"sh", "dash", "bash", "zsh", "ksh", "mksh", "fish", "tcsh", "csh"}

// loginShellPath liefert den Pfad der Shell oder überspringt den Test, wenn
// die Shell fehlt oder das System Windows ist.
func loginShellPath(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Login-Shell-Tests nur unter Unix")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("Shell %s nicht installiert", name)
	}
	return path
}

// runInLoginShell führt script wie sshd aus: "<shell> -c <script>". Die
// Umgebung besteht nur aus HOME, PATH und LANG=C.UTF-8, das Arbeitsverzeichnis
// ist home, stdin kommt aus dem Argument. Bei Fehlern wird die Ausgabe
// protokolliert.
//
// @return int - Exit-Code (127, wenn die Shell nicht startete)
func runInLoginShell(t *testing.T, shell, script, home string, stdin []byte) int {
	t.Helper()
	cmd := exec.Command(shell, "-c", script)
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	t.Logf("%s: Skriptausgabe: %q (%v)", filepath.Base(shell), out, err)
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 127
}

// loginHome legt ein leeres Home an. Mit dirMode >= 0 entsteht ~/.ssh mit
// genau diesen Rechten (chmod, damit die umask nichts verfälscht); mit
// content != nil zusätzlich authorized_keys mit Inhalt und Rechten fileMode.
func loginHome(t *testing.T, dirMode, fileMode os.FileMode, withDir bool, content *string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	dir := filepath.Join(home, ".ssh")
	path = filepath.Join(dir, "authorized_keys")
	if withDir {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, dirMode); err != nil {
			t.Fatal(err)
		}
	}
	if content != nil {
		if err := os.WriteFile(path, []byte(*content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	return home, path
}

// wantFileContent liest path und vergleicht byte-genau mit want.
func wantFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("authorized_keys nicht lesbar: %v", err)
	}
	if string(got) != want {
		t.Errorf("Inhalt von authorized_keys falsch\n got: %q\nwant: %q", got, want)
	}
}

// wantMode prüft die Rechte einer Datei oder eines Verzeichnisses.
func wantMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s nicht lesbar: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s hat Rechte %o, erwartet %o", filepath.Base(path), got, want)
	}
}

// wantExitZero meldet einen Fehler, wenn das Skript nicht mit 0 endete.
func wantExitZero(t *testing.T, code int) {
	t.Helper()
	if code != 0 {
		t.Fatalf("Exit-Code %d, erwartet 0", code)
	}
}

// wantAbsent prüft, dass es den Pfad nicht gibt.
func wantAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s existiert, sollte es nicht", path)
	}
}

// loginShellCase ist ein Fall der Tabelle: Name und Ablauf für eine Shell.
type loginShellCase struct {
	name string
	run  func(t *testing.T, shell string)
}

// loginShellCases liefert die Fälle D1-D7 (Deploy) und R1-R4 (Remove).
func loginShellCases() []loginShellCase {
	const foreign = "ssh-rsa AAAAfremdeZeile fremd@pc\n"
	return []loginShellCase{
		{"D1 neu ohne .ssh", func(t *testing.T, shell string) {
			// Randfall: weder ~/.ssh noch authorized_keys vorhanden
			home, path := loginHome(t, 0, 0, false, nil)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, ownKeyLine+"\n")
			wantMode(t, filepath.Dir(path), 0o700)
			wantMode(t, path, 0o600)
		}},
		{"D2 schon vorhanden", func(t *testing.T, shell string) {
			// Gleicher Base64-Teil, anderer Kommentar: nichts ändern
			content := foreign + "ssh-ed25519 " + ownKeyB64 + " anderer@pc\n"
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, content)
		}},
		{"D3 nur auskommentiert", func(t *testing.T, shell string) {
			// Ein auskommentierter Schlüssel gilt nicht als vorhanden
			content := "# ssh-ed25519 " + ownKeyB64 + " alt\n" + " \t#" + ownKeyB64 + "\n"
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, content+ownKeyLine+"\n")
			if n := strings.Count(content+ownKeyLine+"\n", "\n"); n != 3 {
				t.Fatalf("Testaufbau: %d Zeilen, erwartet 3", n)
			}
		}},
		{"D4 ohne Zeilenende", func(t *testing.T, shell string) {
			// Fremde letzte Zeile ohne \n: der Schlüssel darf nicht angeklebt werden
			content := "fremd"
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, "fremd\n"+ownKeyLine+"\n")
		}},
		{"D5 zu offene Rechte", func(t *testing.T, shell string) {
			// .ssh 0777 und Datei 0666 müssen auf 0700/0600 verschärft werden
			content := foreign
			home, path := loginHome(t, 0o777, 0o666, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, foreign+ownKeyLine+"\n")
			wantMode(t, filepath.Dir(path), 0o700)
			wantMode(t, path, 0o600)
		}},
		{"D6 Injektion über Kommentar", func(t *testing.T, shell string) {
			// Angriff: Der Kommentar enthält alle Zeichen, die in einer Shell
			// etwas auslösen können. Er darf weder ausgeführt noch verändert werden.
			home, path := loginHome(t, 0, 0, false, nil)
			comment := `'"$(touch ` + home + `/PWN)` + "`touch " + home + "/PWN2`" +
				`;!x\\\'%s %n*`
			line := "ssh-ed25519 " + ownKeyB64 + " " + comment
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(line+"\n")))
			wantFileContent(t, path, line+"\n")
			wantAbsent(t, filepath.Join(home, "PWN"))
			wantAbsent(t, filepath.Join(home, "PWN2"))
		}},
		{"D7 Latin-1 und NUL", func(t *testing.T, shell string) {
			// Fremde Zeilen mit Byte 0xFC und NUL, eigener Schlüssel vorhanden:
			// grep darf die Datei nicht für binär halten, nichts wird angehängt.
			content := "ssh-rsa AAAAfremdeEins M\xfcller\n" + ownKeyLine + "\n" +
				"ssh-rsa AAAAfremdeZwei nul\x00byte\n"
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, deployKeyScript, home, []byte(ownKeyLine+"\n")))
			wantFileContent(t, path, content)
		}},
		{"R1 einziger Eintrag", func(t *testing.T, shell string) {
			// stdin ist nur der Base64-Teil ohne Zeilenende (wie RemoveDeployedKey)
			content := ownKeyLine + "\n"
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, removeKeyScript, home, []byte(ownKeyB64)))
			wantFileContent(t, path, "")
			wantMode(t, path, 0o600)
			wantAbsent(t, filepath.Join(home, ".ssh", ".authorized_keys_tmp"))
		}},
		{"R2 mehrere Zeilen", func(t *testing.T, shell string) {
			// Beide eigenen Zeilen (auch mit anderem Kommentar) verschwinden,
			// fremde Zeile und Kommentarzeile bleiben; Rechte vorher 0644.
			content := foreign + ownKeyLine + "\n# nur ein Kommentar\n" +
				"ssh-ed25519 " + ownKeyB64 + " anderer@pc\n"
			home, path := loginHome(t, 0o700, 0o644, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, removeKeyScript, home, []byte(ownKeyB64)))
			wantFileContent(t, path, foreign+"# nur ein Kommentar\n")
			wantMode(t, path, 0o600)
			wantAbsent(t, filepath.Join(home, ".ssh", ".authorized_keys_tmp"))
		}},
		{"R3 Datei fehlt", func(t *testing.T, shell string) {
			// Nichts zu entfernen ist ein Erfolg; die Datei wird nicht angelegt
			home, path := loginHome(t, 0o700, 0, true, nil)
			wantExitZero(t, runInLoginShell(t, shell, removeKeyScript, home, []byte(ownKeyB64)))
			wantAbsent(t, path)
			wantAbsent(t, filepath.Join(home, ".ssh", ".authorized_keys_tmp"))
		}},
		{"R4 Latin-1 und NUL", func(t *testing.T, shell string) {
			// Beim Entfernen bleiben Zeilen mit 0xFC und NUL byte-genau erhalten
			before := "ssh-rsa AAAAfremdeEins M\xfcller\n"
			after := "ssh-rsa AAAAfremdeZwei nul\x00byte\n"
			content := before + ownKeyLine + "\n" + after
			home, path := loginHome(t, 0o700, 0o600, true, &content)
			wantExitZero(t, runInLoginShell(t, shell, removeKeyScript, home, []byte(ownKeyB64)))
			wantFileContent(t, path, before+after)
			wantAbsent(t, filepath.Join(home, ".ssh", ".authorized_keys_tmp"))
		}},
	}
}

// TestKeyScriptsRunUnderLoginShells führt beide Skripte als Befehl jeder
// installierten Login-Shell aus (wie sshd) und prüft Deploy (D1-D7) und
// Entfernen (R1-R4) am Dateisystem. Randfälle: History-Expansion (csh),
// Backslash-Halbierung (fish), Injektion über den Kommentar, Latin-1/NUL,
// zu offene Rechte, Datei ohne Zeilenende. Fehlende Shells werden übersprungen.
func TestKeyScriptsRunUnderLoginShells(t *testing.T) {
	for _, name := range loginShellNames {
		t.Run(name, func(t *testing.T) {
			shell := loginShellPath(t, name)
			for _, c := range loginShellCases() {
				t.Run(c.name, func(t *testing.T) { c.run(t, shell) })
			}
		})
	}
}

// runDeployMutant führt script (ein Mutant von deployKeyScript) im Fall D1
// aus: kein ~/.ssh, stdin = ownKeyLine. Ergebnis: Exit-Code und Inhalt von
// authorized_keys (ok=false, wenn die Datei fehlt).
func runDeployMutant(t *testing.T, shell, script string) (code int, content string, ok bool) {
	t.Helper()
	home, path := loginHome(t, 0, 0, false, nil)
	code = runInLoginShell(t, shell, script, home, []byte(ownKeyLine+"\n"))
	data, err := os.ReadFile(path)
	return code, string(data), err == nil
}

// mutateOnce ersetzt genau eine Stelle im echten Skript und bricht ab, wenn
// die Stelle fehlt (sonst wäre die Gegenprobe wirkungslos).
func mutateOnce(t *testing.T, script, from, to string) string {
	t.Helper()
	if !strings.Contains(script, from) {
		t.Fatalf("Skript enthält %q nicht mehr, Gegenprobe veraltet", from)
	}
	return strings.Replace(script, from, to, 1)
}

// wantDeployFails prüft, dass der Mutant unter der Shell scheitert: Exit-Code
// ungleich 0 ODER Datei fehlt ODER Inhalt falsch.
func wantDeployFails(t *testing.T, shell, script string) {
	t.Helper()
	code, content, ok := runDeployMutant(t, shell, script)
	if code == 0 && ok && content == ownKeyLine+"\n" {
		t.Errorf("Mutant lief unter %s fehlerfrei durch, der Runner erkennt die Falle nicht", filepath.Base(shell))
	}
}

// wantDeployWorks prüft, dass das Skript unter der Shell korrekt arbeitet.
func wantDeployWorks(t *testing.T, shell, script string) {
	t.Helper()
	code, content, ok := runDeployMutant(t, shell, script)
	if code != 0 || !ok || content != ownKeyLine+"\n" {
		t.Errorf("Skript sollte unter %s laufen: Exit %d, Datei da=%v, Inhalt %q", filepath.Base(shell), code, ok, content)
	}
}

// TestKeyScriptsLoginShellCounterProbe ist die Gegenprobe zu
// TestKeyScriptsRunUnderLoginShells: Es baut Mutanten von deployKeyScript, die
// genau die bekannten Fallen enthalten, und verlangt, dass der Runner sie
// bemerkt. Ohne diese Probe könnte ein Runner, der die Fallen nie auslöst,
// alle Tests dauerhaft grün halten.
//   - "!x" im Skript: csh und tcsh MÜSSEN scheitern (History-Expansion),
//     dash läuft weiter.
//   - echter Zeilenumbruch: csh und tcsh MÜSSEN scheitern ("Unmatched '").
//   - vier Backslashes vor n: fish halbiert sie, dash nicht - die Dateien
//     unterscheiden sich.
func TestKeyScriptsLoginShellCounterProbe(t *testing.T) {
	bang := mutateOnce(t, deployKeyScript, `[ -n "$2" ]`, `[ -n "$2!x" ]`)
	newline := mutateOnce(t, deployKeyScript, "rc=$?; ", "rc=$?;\n")

	for _, name := range []string{"csh", "tcsh"} {
		t.Run("History-Expansion "+name, func(t *testing.T) {
			shell := loginShellPath(t, name)
			wantDeployWorks(t, shell, deployKeyScript) // Kontrolle: echtes Skript läuft
			wantDeployFails(t, shell, bang)
		})
		t.Run("Zeilenumbruch "+name, func(t *testing.T) {
			shell := loginShellPath(t, name)
			wantDeployWorks(t, shell, deployKeyScript)
			wantDeployFails(t, shell, newline)
		})
	}

	t.Run("Mutanten unter dash unschädlich", func(t *testing.T) {
		// Kontrolle: Für eine POSIX-Shell sind beide Mutanten gleichwertig
		shell := loginShellPath(t, "dash")
		wantDeployWorks(t, shell, bang)
		wantDeployWorks(t, shell, newline)
	})

	t.Run("Backslash-Paare fish gegen dash", func(t *testing.T) {
		fish := loginShellPath(t, "fish")
		dash := loginShellPath(t, "dash")
		pairs := mutateOnce(t, deployKeyScript, `printf "%s\n"`, `printf "%s\\\\n"`)
		_, dashContent, dashOK := runDeployMutant(t, dash, pairs)
		_, fishContent, fishOK := runDeployMutant(t, fish, pairs)
		if !dashOK || !fishOK {
			t.Fatalf("authorized_keys fehlt: dash=%v fish=%v", dashOK, fishOK)
		}
		if dashContent == fishContent {
			t.Errorf("fish und dash lieferten dasselbe (%q): der Runner erkennt die Backslash-Halbierung nicht", fishContent)
		}
		// dash behält vier Backslashes -> druckt \n wörtlich; fish halbiert -> echter Zeilenumbruch
		if want := ownKeyLine + `\n`; dashContent != want {
			t.Errorf("dash: %q, erwartet %q", dashContent, want)
		}
		if want := ownKeyLine + "\n"; fishContent != want {
			t.Errorf("fish: %q, erwartet %q", fishContent, want)
		}
	})
}

// fishBackslashProblem prüft den inneren Text eines "sh -c '...'"-Skripts auf
// Backslash-Muster, die fish verändert: "\\" (Paar) und jeder Backslash, auf
// den kein "n" folgt (nur "%s\n" ist erlaubt). Leer = in Ordnung.
func fishBackslashProblem(inner string) string {
	if strings.Contains(inner, `\\`) {
		return `enthält "\\" (fish halbiert es)`
	}
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && (i+1 >= len(inner) || inner[i+1] != 'n') {
			return "Backslash ohne folgendes n an Stelle " + strings.TrimSpace(inner[max(0, i-10):min(len(inner), i+10)])
		}
	}
	return ""
}

// TestKeyScriptsFreeOfFishBackslashPairs prüft den Skripttext beider Skripte:
// Im Inneren (zwischen "sh -c '" und dem letzten "'") steht kein "\\" und
// jeder Backslash ist von "n" gefolgt. Grund: fish macht in einfachen
// Anführungszeichen aus "\\" ein "\" und aus "\'" ein "'"; das verändert das
// Skript oder beendet den Block zu früh. Zusätzlich prüft der Test die
// Prüffunktion selbst an bekannten schlechten und guten Beispielen.
func TestKeyScriptsFreeOfFishBackslashPairs(t *testing.T) {
	t.Run("Prüffunktion erkennt Muster", func(t *testing.T) {
		for _, bad := range []string{`a\\n`, `a\'b`, `a\`, `a\x`, `\\`} {
			if fishBackslashProblem(bad) == "" {
				t.Errorf("%q wurde nicht beanstandet", bad)
			}
		}
		for _, good := range []string{``, `printf "%s\n" x`, `a\nb\n`} {
			if p := fishBackslashProblem(good); p != "" {
				t.Errorf("%q fälschlich beanstandet: %s", good, p)
			}
		}
	})
	for name, script := range map[string]string{"deployKeyScript": deployKeyScript, "removeKeyScript": removeKeyScript} {
		t.Run(name, func(t *testing.T) {
			const prefix = "sh -c '"
			if !strings.HasPrefix(script, prefix) || !strings.HasSuffix(script, "'") {
				t.Fatalf("Form sh -c '...' erwartet")
			}
			inner := script[len(prefix) : len(script)-1]
			if p := fishBackslashProblem(inner); p != "" {
				t.Errorf("%s: %s", name, p)
			}
		})
	}
}
