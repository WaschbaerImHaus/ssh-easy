// Paket main - Tests: grep in deployKeyScript und removeKeyScript mit LC_ALL=C (Build 89)
//
// Hintergrund: GNU grep hält eine Datei für binär, wenn sie in der laufenden
// Locale ungültige Zeichen enthält (Latin-1-Kommentar "Müller" = Byte 0xFC unter
// UTF-8) oder ein NUL-Byte. Bei "binär" gibt grep statt der Zeilen nur die
// Meldung "binary file matches" aus (auf stderr), also keine Zeilen auf stdout:
//   - removeKeyScript: "grep -vF" schreibt nichts in die Temp-Datei, der
//     Zeilenabgleich (grep -c gegen wc -l) schlägt an, das Skript bricht mit
//     Exit 2 ab - der Schlüssel bleibt auf dem Server stehen.
//   - deployKeyScript: Das Prüf-grep findet einen schon vorhandenen Schlüssel
//     nicht und hängt ihn ein zweites Mal an.
//
// Die Skripte sollen deshalb jedes grep mit LC_ALL=C aufrufen.
//
// Die Tests führen die Skripte lokal mit /bin/sh aus (wie runShellCommand, aber
// mit wählbarer Locale-Umgebung). Fehlt eine Locale auf dem Rechner, wird der
// Fall übersprungen - sonst fiele grep auf die C-Locale zurück und der Test
// bewiese nichts. Ohne GNU grep (BusyBox) bestehen die Fälle ohnehin.
//
// ACHTUNG NUL-Byte: Auch in der C-Locale hält GNU grep eine Datei mit NUL-Byte
// für binär. LC_ALL=C allein behebt nur den Latin-1-Fall; für NUL braucht
// jedes grep zusätzlich -a (--text). Die Fälle "NUL" und "beides" prüfen das
// bewusst mit (siehe TestKeyScriptsHandleBinaryAuthorizedKeys).
//
// @author Kurt Ingwer
// @date   2026-11-07 10:00
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// localeAvailable prüft mit "locale -a", ob die Locale installiert ist (Schreib-
// weise egal: "C.UTF-8" = "C.utf8"). "C" und "POSIX" gibt es immer. Ohne das
// Programm locale gilt jede andere Locale als nicht verfügbar.
func localeAvailable(name string) bool {
	if name == "C" || name == "POSIX" {
		return true
	}
	out, err := exec.Command("locale", "-a").Output()
	if err != nil {
		return false
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "")) }
	for _, line := range strings.Fields(string(out)) {
		if norm(line) == norm(name) {
			return true
		}
	}
	return false
}

// runScriptWithEnv führt einen Skripttext ("sh -c '...'") wie sshd mit /bin/sh
// -c aus. Die Umgebung besteht nur aus HOME, PATH und extraEnv (z. B.
// "LC_ALL=C.UTF-8"). stdin kommt aus dem Argument.
//
// @return int - Exit-Code (127, wenn die Shell nicht startete)
func runScriptWithEnv(t *testing.T, script, home string, extraEnv []string, stdin []byte) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = home
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}, extraEnv...)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		t.Logf("Skriptausgabe: %q", out)
		return exitErr.ExitCode()
	}
	return 127
}

// TestKeyScriptsEveryGrepRunsWithLCAllC prüft den Skripttext: Vor JEDEM grep
// beider Skripte steht "LC_ALL=C " (auch in der Befehlsersetzung $(…) und hinter
// der Pipe). Mindestens zwei grep-Aufrufe je Skript (deploy: Kommentare
// ausfiltern + Suche; remove: Filter + Zeilenzählung), sonst wäre die Zählung
// kaputt.
func TestKeyScriptsEveryGrepRunsWithLCAllC(t *testing.T) {
	grepWord := regexp.MustCompile(`\bgrep\b`)
	for name, script := range map[string]string{"deployKeyScript": deployKeyScript, "removeKeyScript": removeKeyScript} {
		hits := grepWord.FindAllStringIndex(script, -1)
		if len(hits) < 2 {
			t.Errorf("%s: %d grep-Aufrufe gefunden, erwartet mindestens 2", name, len(hits))
		}
		for _, hit := range hits {
			if !strings.HasSuffix(script[:hit[0]], "LC_ALL=C ") {
				start := hit[0] - 25
				if start < 0 {
					start = 0
				}
				t.Errorf("%s: grep ohne vorangestelltes LC_ALL=C: ...%s", name, script[start:hit[1]+20])
			}
		}
	}
}

// TestKeyScriptsStillFreeOfSingleQuotes prüft, dass die Ergänzung die
// Schreibregeln der Skripte nicht verletzt: keine einfachen Anführungszeichen
// im Inneren (sie laufen in "sh -c '...'", Login-Shell kann fish/csh sein),
// kein "!", kein Zeilenumbruch.
func TestKeyScriptsStillFreeOfSingleQuotes(t *testing.T) {
	for name, script := range map[string]string{"deployKeyScript": deployKeyScript, "removeKeyScript": removeKeyScript} {
		const prefix = "sh -c '"
		if !strings.HasPrefix(script, prefix) || !strings.HasSuffix(script, "'") {
			t.Fatalf("%s: Form sh -c '...' erwartet", name)
		}
		inner := script[len(prefix) : len(script)-1]
		if strings.ContainsAny(inner, "'!\n") {
			t.Errorf("%s: enthält einfaches Anführungszeichen, ! oder Zeilenumbruch", name)
		}
	}
}

// binaryAuthCase ist ein Inhalt von authorized_keys mit einem eigenen
// Schlüssel (Base64-Teil ownKey) zwischen fremden Zeilen.
type binaryAuthCase struct {
	name string
	// before und after stehen vor und hinter der eigenen Zeile
	before, after string
}

const (
	ownKeyB64  = "AAAAC3NzaC1lZDI1NTE5AAAAIeigenerSchluessel"
	ownKeyLine = "ssh-ed25519 " + ownKeyB64 + " own@pc"
)

// binaryAuthCases liefert die Inhalte: Latin-1 (0xFC), NUL und beides. Die
// Bytes entstehen mit Escapes (\xfc, \x00), nie als sichtbare Zeichen.
func binaryAuthCases() []binaryAuthCase {
	return []binaryAuthCase{
		{
			name:   "Latin-1",
			before: "ssh-rsa AAAAforeignOne M\xfcller\n",
			after:  "ssh-ed25519 AAAAforeignTwo other\n",
		},
		{
			name:   "NUL",
			before: "ssh-rsa AAAAforeignOne plain\n",
			after:  "ssh-ed25519 AAAAforeignTwo nul\x00byte\n",
		},
		{
			name:   "Latin-1 und NUL",
			before: "ssh-rsa AAAAforeignOne M\xfcller\n",
			after:  "ssh-ed25519 AAAAforeignTwo nul\x00byte\n",
		},
	}
}

// localeEnv beschreibt eine Locale-Umgebung für den Skriptlauf.
type localeEnv struct {
	// label - Name im Testbericht
	label string
	// locale - Name der Locale (für die Verfügbarkeitsprüfung)
	locale string
	// env - Umgebungsvariablen für den Lauf
	env []string
}

// localeEnvs liefert die zu prüfenden Umgebungen. UTF-8-Locales über LC_ALL
// und über LANG (das Skript muss beides überstimmen); "LC_ALL=C" als Kontrolle
// für den NUL-Fall, der auch ohne UTF-8 auftritt.
func localeEnvs() []localeEnv {
	var list []localeEnv
	for _, loc := range []string{"C.UTF-8", "en_US.UTF-8"} {
		list = append(list,
			localeEnv{"LC_ALL=" + loc, loc, []string{"LC_ALL=" + loc}},
			localeEnv{"LANG=" + loc, loc, []string{"LANG=" + loc}},
		)
	}
	return append(list, localeEnv{"LC_ALL=C", "C", []string{"LC_ALL=C"}})
}

// prepareServerHome legt HOME/.ssh (0700) und authorized_keys (0600) mit dem
// Inhalt an und liefert Home und den Pfad der Datei.
func prepareServerHome(t *testing.T, content string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-Skripte nur unter Unix")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh nicht gefunden")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, path
}

// TestKeyScriptsHandleBinaryAuthorizedKeys führt beide Skripte lokal gegen eine
// authorized_keys mit Latin-1-Zeichen und/oder NUL-Byte aus, in mehreren
// Locale-Umgebungen. Erwartungen (Randfall: GNU grep hält die Datei sonst für
// binär):
//   - remove: Exit 0, die eigene Zeile ist weg, alle übrigen Zeilen sind
//     byte-genau erhalten (auch das 0xFC- und das NUL-Byte), Rechte 600, keine
//     Temp-Datei übrig.
//   - deploy, Schlüssel schon da: Exit 0, Datei unverändert (kein zweiter
//     Eintrag).
//   - deploy, Schlüssel neu: Exit 0, alte Zeilen unverändert, neue Zeile genau
//     einmal am Ende.
func TestKeyScriptsHandleBinaryAuthorizedKeys(t *testing.T) {
	const newKeyB64 = "AAAAC3NzaC1lZDI1NTE5AAAAIneuerSchluessel"
	const newKeyLine = "ssh-ed25519 " + newKeyB64 + " new@pc"
	for _, env := range localeEnvs() {
		for _, c := range binaryAuthCases() {
			name := env.label + "/" + c.name
			t.Run("entfernen/"+name, func(t *testing.T) {
				if !localeAvailable(env.locale) {
					t.Skipf("Locale %s nicht installiert", env.locale)
				}
				home, path := prepareServerHome(t, c.before+ownKeyLine+"\n"+c.after)
				code := runScriptWithEnv(t, removeKeyScript, home, env.env, []byte(ownKeyB64))
				if code != 0 {
					t.Errorf("Exit-Code %d, erwartet 0 (Schlüssel muss sich entfernen lassen)", code)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if want := c.before + c.after; string(got) != want {
					t.Errorf("authorized_keys nach dem Entfernen:\n got  %q\n want %q", got, want)
				}
				if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0o600 {
					t.Errorf("Rechte %v, erwartet 600", info.Mode().Perm())
				}
				if _, err := os.Stat(filepath.Join(home, ".ssh", ".authorized_keys_tmp")); err == nil {
					t.Error("Temp-Datei .authorized_keys_tmp blieb liegen")
				}
			})
			t.Run("hinterlegen-vorhanden/"+name, func(t *testing.T) {
				if !localeAvailable(env.locale) {
					t.Skipf("Locale %s nicht installiert", env.locale)
				}
				content := c.before + ownKeyLine + "\n" + c.after
				home, path := prepareServerHome(t, content)
				code := runScriptWithEnv(t, deployKeyScript, home, env.env, []byte(ownKeyLine+"\n"))
				if code != 0 {
					t.Errorf("Exit-Code %d, erwartet 0", code)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != content {
					t.Errorf("vorhandener Schlüssel nicht erkannt, Datei verändert:\n got  %q\n want %q", got, content)
				}
			})
			t.Run("hinterlegen-neu/"+name, func(t *testing.T) {
				if !localeAvailable(env.locale) {
					t.Skipf("Locale %s nicht installiert", env.locale)
				}
				content := c.before + ownKeyLine + "\n" + c.after
				home, path := prepareServerHome(t, content)
				code := runScriptWithEnv(t, deployKeyScript, home, env.env, []byte(newKeyLine+"\n"))
				if code != 0 {
					t.Errorf("Exit-Code %d, erwartet 0", code)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if want := content + newKeyLine + "\n"; string(got) != want {
					t.Errorf("authorized_keys nach dem Hinterlegen:\n got  %q\n want %q", got, want)
				}
			})
		}
	}
}
