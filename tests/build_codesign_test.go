// Paket main - Tests für das Codesign-Passwort in build.sh (Audit Build 90)
//
// Hintergrund: build.sh kann die Windows-EXEs per osslsigncode signieren.
// Das Zertifikatspasswort darf dabei nie in der Umgebung von Kindprozessen
// (go test, go build, makensis, osslsigncode), nie in deren Kommandozeile
// (Prozessliste) und nie in einer für andere lesbaren Datei landen.
// Geprüft wird das Skript mit "source" und SSH_EASY_BUILD_SOURCE_ONLY=1
// (lädt nur die Funktionen, baut nicht) gegen einen Stub-osslsigncode.
//
// Sicherheitsregeln dieser Tests:
//   - Der echte Build wird NIE gestartet: Das Skript wird in ein leeres
//     Testverzeichnis kopiert (SCRIPT_DIR zeigt dorthin), dort gibt es kein
//     src/. Ein versehentlich startender Build bräche sofort ab.
//   - Die bash-Umgebung wird explizit aufgebaut (keine SSH_EASY_*- und
//     CODESIGN_*-Variablen des Testlaufs), PATH beginnt mit dem Stub.
//   - Nur Linux mit bash (GNU stat im Stub); sonst t.Skip.
//
// Das Skript unter Test lässt sich mit SSH_EASY_BUILD_SH_UNDER_TEST
// überschreiben (Standard: ../build.sh, relativ zum Testordner).
//
// @author Kurt Ingwer
// @date   2026-11-15 15:10
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// codesignTestPassword enthält Leerzeichen, $, einfache und doppelte
// Anführungszeichen, Backtick, Backslash und ein Leerzeichen am Ende: Alles,
// was beim Weiterreichen durch eine Shell leicht verloren geht.
const codesignTestPassword = "pa ss$word'q\"d $HOME `x` \\ ü "

// codesignWorkspace ist die Testumgebung eines Laufs: Skriptkopie, Stub,
// Logordner, TMPDIR, eine .p12 und eine zu signierende Datei.
type codesignWorkspace struct {
	t      *testing.T
	root   string
	script string // Kopie von build.sh (SCRIPT_DIR = root)
	binDir string // enthält den Stub osslsigncode
	logDir string // Aufzeichnungen des Stubs
	tmpDir string // TMPDIR des Laufs (Ziel von mktemp)
	p12    string // Pfad der Pseudo-.p12
	target string // Datei, die signiert wird
}

// codesignResult ist das Ergebnis eines bash-Laufs.
type codesignResult struct {
	output   string // Standard- und Fehlerausgabe zusammen
	exitCode int
}

// codesignOptions steuert einen Lauf.
type codesignOptions struct {
	env      []string // zusätzliche Variablen "NAME=Wert" (über der Grundumgebung)
	umask    string   // z. B. "000"; leer = Standard
	signFail bool     // Stub bricht bei "sign" mit Exit 3 ab
	command  string   // bash-Befehle nach dem source (Standard: init + sign + DONE)
}

// requireCodesignEnvironment überspringt den Test außerhalb von Linux/bash.
func requireCodesignEnvironment(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("build.sh-Tests laufen nur unter Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash nicht gefunden")
	}
}

// newCodesignWorkspace legt Skriptkopie, Stub, Logordner und Dateien an.
// Die .p12 hat Modus 0600 (Tests ändern ihn bei Bedarf).
func newCodesignWorkspace(t *testing.T) *codesignWorkspace {
	t.Helper()
	requireCodesignEnvironment(t)
	scriptPath := os.Getenv("SSH_EASY_BUILD_SH_UNDER_TEST")
	if scriptPath == "" {
		scriptPath = "../build.sh"
	}
	scriptPath, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("build.sh nicht lesbar (%s): %v", scriptPath, err)
	}
	root := t.TempDir()
	ws := &codesignWorkspace{
		t:      t,
		root:   root,
		script: filepath.Join(root, "build.sh"),
		binDir: filepath.Join(root, "bin"),
		logDir: filepath.Join(root, "log"),
		tmpDir: filepath.Join(root, "tmp"),
		p12:    filepath.Join(root, "cert.p12"),
		target: filepath.Join(root, "app.exe"),
	}
	for _, dir := range []string{ws.binDir, ws.logDir, ws.tmpDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Kopie des Skripts: SCRIPT_DIR zeigt in das Testverzeichnis, ein
	// versehentlich startender Build findet dort kein src/ und bricht ab
	if err := os.WriteFile(ws.script, original, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{ws.p12, ws.target} {
		if err := os.WriteFile(f, []byte("attrappe"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws.logDir, "calls"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return ws
}

// writeStub schreibt den Stub osslsigncode. "sign" schreibt die -out-Datei
// und zeichnet Argumente, Umgebung sowie Pfad, Rechte und Inhalt der
// -readpass-Datei auf; "verify" endet mit Exit 0.
func (ws *codesignWorkspace) writeStub(signFail bool) {
	ws.t.Helper()
	failLine := ""
	if signFail {
		failLine = "exit 3"
	}
	script := fmt.Sprintf(`#!/bin/bash
LOG=%q
cmd="$1"
n=$(( $(wc -l < "$LOG/calls") + 1 ))
echo "$cmd" >> "$LOG/calls"
printf '%%s\n' "$@" > "$LOG/args.$n"
env > "$LOG/env.$n"
[ "$cmd" = "verify" ] && exit 0
out=""; pass=""; prev=""
for a in "$@"; do
	[ "$prev" = "-out" ] && out="$a"
	[ "$prev" = "-readpass" ] && pass="$a"
	prev="$a"
done
if [ -n "$pass" ]; then
	printf '%%s' "$pass" > "$LOG/pass.path.$n"
	stat -c %%a "$pass" > "$LOG/pass.mode.$n"
	cp "$pass" "$LOG/pass.content.$n"
fi
%s
[ -n "$out" ] && echo signiert > "$out"
exit 0
`, ws.logDir, failLine)
	if err := os.WriteFile(filepath.Join(ws.binDir, "osslsigncode"), []byte(script), 0o700); err != nil {
		ws.t.Fatal(err)
	}
}

// buildEnv baut die bash-Umgebung explizit: Der Testlauf-Rest ohne
// SSH_EASY_*, CODESIGN_*, XDG_RUNTIME_DIR, TMPDIR, BASH_ENV, ENV und PATH,
// dazu PATH mit dem Stub vorn, TMPDIR im Testordner und SOURCE_ONLY=1.
func (ws *codesignWorkspace) buildEnv(extra []string) []string {
	var env []string
	oldPath := ""
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		switch {
		case name == "PATH":
			oldPath = value
		case strings.HasPrefix(name, "SSH_EASY_"), strings.HasPrefix(name, "CODESIGN_"),
			name == "XDG_RUNTIME_DIR", name == "TMPDIR", name == "BASH_ENV", name == "ENV":
		default:
			env = append(env, entry)
		}
	}
	env = append(env,
		"PATH="+ws.binDir+string(os.PathListSeparator)+oldPath,
		"TMPDIR="+ws.tmpDir,
		"SSH_EASY_BUILD_SOURCE_ONLY=1",
	)
	return append(env, extra...)
}

// run führt "bash -c 'source "$1"; …' _ script file" aus (höchstens 20 s).
func (ws *codesignWorkspace) run(opts codesignOptions) codesignResult {
	ws.t.Helper()
	ws.writeStub(opts.signFail)
	command := opts.command
	if command == "" {
		command = `codesign_init; codesign_files "$2"; echo DONE`
	}
	prefix := ""
	if opts.umask != "" {
		prefix = "umask " + opts.umask + "; "
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", prefix+`source "$1"; `+command, "_", ws.script, ws.target)
	cmd.Env = ws.buildEnv(opts.env)
	cmd.Dir = ws.root
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	result := codesignResult{output: string(out)}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exitCode = exitErr.ExitCode()
	default:
		ws.t.Fatalf("bash nicht ausführbar: %v\n%s", err, out)
	}
	if ctx.Err() != nil {
		ws.t.Fatalf("bash-Lauf überschritt 20 s:\n%s", out)
	}
	return result
}

// logFile liest eine Aufzeichnung des Stubs ("" wenn sie fehlt).
func (ws *codesignWorkspace) logFile(name string) string {
	data, err := os.ReadFile(filepath.Join(ws.logDir, name))
	if err != nil {
		return ""
	}
	return string(data)
}

// calls liefert die Stub-Aufrufe in Reihenfolge ("sign", "verify").
func (ws *codesignWorkspace) calls() []string {
	return strings.Fields(ws.logFile("calls"))
}

// setMode setzt die Rechte einer Datei oder eines Ordners (umask-fest).
func (ws *codesignWorkspace) setMode(path string, mode os.FileMode) {
	ws.t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		ws.t.Fatal(err)
	}
}

// newRuntimeDir legt ein Verzeichnis mit den angegebenen Rechten an
// (als XDG_RUNTIME_DIR).
func (ws *codesignWorkspace) newRuntimeDir(name string, mode os.FileMode) string {
	ws.t.Helper()
	dir := filepath.Join(ws.root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		ws.t.Fatal(err)
	}
	ws.setMode(dir, mode)
	return dir
}

// dirEntries zählt die Einträge eines Ordners.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// assertPasswordHidden prüft, dass in der Aufzeichnung eines Stub-Aufrufs
// weder Passwortvariablen noch das Passwort in Umgebung oder Argumenten
// stehen.
func (ws *codesignWorkspace) assertPasswordHidden(call int, password string, extraSecrets ...string) {
	ws.t.Helper()
	envLog := ws.logFile(fmt.Sprintf("env.%d", call))
	if envLog == "" {
		ws.t.Fatalf("Aufruf %d: keine Umgebung aufgezeichnet", call)
	}
	for _, line := range strings.Split(envLog, "\n") {
		name, _, _ := strings.Cut(line, "=")
		switch name {
		case "SSH_EASY_CODESIGN_PASSWORD", "CODESIGN_PASSWORD", "CODESIGN_PASSWORD_SET":
			ws.t.Errorf("Aufruf %d: Passwortvariable %s steht in der Umgebung des Kindprozesses", call, name)
		}
	}
	args := ws.logFile(fmt.Sprintf("args.%d", call))
	for _, secret := range append([]string{password}, extraSecrets...) {
		if secret == "" {
			continue
		}
		if strings.Contains(envLog, secret) {
			ws.t.Errorf("Aufruf %d: %q steht in der Umgebung", call, secret)
		}
		if strings.Contains(args, secret) {
			ws.t.Errorf("Aufruf %d: %q steht in der Kommandozeile", call, secret)
		}
	}
}

// signCall liefert die Nummer des ersten "sign"-Aufrufs (0 = keiner).
func (ws *codesignWorkspace) signCall() int {
	for i, c := range ws.calls() {
		if c == "sign" {
			return i + 1
		}
	}
	return 0
}

// TestCodesignPasswordNeverReachesChildren prüft den Kern von Fund F6:
// Das Passwort aus SSH_EASY_CODESIGN_PASSWORD (mit Leerzeichen, $, ' und ")
// und ein fremdes, schon exportiertes CODESIGN_PASSWORD gelangen nie in
// Umgebung oder Argumente von osslsigncode. Das Passwort steht nur in einer
// 0600-Datei unter XDG_RUNTIME_DIR (Inhalt exakt gleich, auch bei umask 000),
// die nach dem Ende von bash gelöscht ist.
func TestCodesignPasswordNeverReachesChildren(t *testing.T) {
	for _, umask := range []string{"", "000"} {
		t.Run("umask="+umask, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			xdg := ws.newRuntimeDir("xdg", 0o700)
			ws.setMode(ws.p12, 0o600)
			// Zweite Datei: Die Passwortdatei muss für mehrere Aufrufe reichen
			second := filepath.Join(ws.root, "second.exe")
			if err := os.WriteFile(second, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			result := ws.run(codesignOptions{
				umask: umask,
				env: []string{
					"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword,
					"CODESIGN_PASSWORD=fremd",
					"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
					"SSH_EASY_CODESIGN_TSA=none",
					"XDG_RUNTIME_DIR=" + xdg,
				},
				command: `codesign_init; codesign_files "$2" "` + second + `"; echo DONE`,
			})
			if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
				t.Fatalf("Signieren schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
			}
			if got := strings.Join(ws.calls(), ","); got != "sign,verify,sign,verify" {
				t.Fatalf("Stub-Aufrufe = %q, erwartet sign,verify,sign,verify", got)
			}
			for _, call := range []int{1, 2, 3, 4} {
				ws.assertPasswordHidden(call, codesignTestPassword, "fremd")
			}
			// -readpass: Datei in XDG_RUNTIME_DIR, 0600, Inhalt exakt das Passwort
			passPath := ws.logFile("pass.path.1")
			if filepath.Dir(passPath) != xdg {
				t.Errorf("Passwortdatei %q liegt nicht in XDG_RUNTIME_DIR %q", passPath, xdg)
			}
			if mode := strings.TrimSpace(ws.logFile("pass.mode.1")); mode != "600" {
				t.Errorf("Passwortdatei hat Modus %s, erwartet 600", mode)
			}
			if got := ws.logFile("pass.content.1"); got != codesignTestPassword {
				t.Errorf("Inhalt der Passwortdatei = %q, erwartet %q", got, codesignTestPassword)
			}
			if ws.logFile("pass.path.3") != passPath {
				t.Error("zweiter sign-Aufruf nutzt eine andere Passwortdatei")
			}
			// Nach dem Ende von bash (EXIT-Trap) gelöscht
			if _, err := os.Stat(passPath); !os.IsNotExist(err) {
				t.Errorf("Passwortdatei %s nach dem Lauf noch vorhanden (%v)", passPath, err)
			}
			if left := dirEntries(t, xdg); len(left) != 0 {
				t.Errorf("XDG_RUNTIME_DIR nicht leer: %v", left)
			}
		})
	}
}

// TestCodesignPassfileFallsBackToMktemp prüft die Ausweichfälle: Ist
// XDG_RUNTIME_DIR leer, nicht gesetzt, kein Verzeichnis, ein Symlink oder für
// Gruppe/andere zugänglich (0755, 0750, 0705), landet die Passwortdatei per
// mktemp in TMPDIR - nie dort. Immer 0600, mit exaktem Inhalt, danach gelöscht.
// Angriff: ein weltlesbares Verzeichnis als Ablage des Passworts.
func TestCodesignPassfileFallsBackToMktemp(t *testing.T) {
	type variant struct {
		name  string
		setup func(ws *codesignWorkspace) (xdgEnv []string, forbidden string)
	}
	openDir := func(mode os.FileMode) func(ws *codesignWorkspace) ([]string, string) {
		return func(ws *codesignWorkspace) ([]string, string) {
			dir := ws.newRuntimeDir("xdg", mode)
			return []string{"XDG_RUNTIME_DIR=" + dir}, dir
		}
	}
	variants := []variant{
		{"leer", func(ws *codesignWorkspace) ([]string, string) { return []string{"XDG_RUNTIME_DIR="}, "" }},
		{"nicht gesetzt", func(ws *codesignWorkspace) ([]string, string) { return nil, "" }},
		{"Verzeichnis 0755", openDir(0o755)},
		{"Verzeichnis 0750", openDir(0o750)},
		{"Verzeichnis 0705", openDir(0o705)},
		{"nicht vorhanden", func(ws *codesignWorkspace) ([]string, string) {
			return []string{"XDG_RUNTIME_DIR=" + filepath.Join(ws.root, "gibt-es-nicht")}, ""
		}},
		{"normale Datei", func(ws *codesignWorkspace) ([]string, string) {
			return []string{"XDG_RUNTIME_DIR=" + ws.target}, ""
		}},
		{"Symlink auf 0700-Verzeichnis", func(ws *codesignWorkspace) ([]string, string) {
			real := ws.newRuntimeDir("xdg-real", 0o700)
			link := filepath.Join(ws.root, "xdg-link")
			if err := os.Symlink(real, link); err != nil {
				ws.t.Fatal(err)
			}
			return []string{"XDG_RUNTIME_DIR=" + link}, real
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			xdgEnv, forbidden := v.setup(ws)
			result := ws.run(codesignOptions{
				env: append([]string{
					"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword,
					"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
					"SSH_EASY_CODESIGN_TSA=none",
				}, xdgEnv...),
			})
			if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
				t.Fatalf("Signieren schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
			}
			passPath := ws.logFile("pass.path.1")
			if passPath == "" {
				t.Fatal("kein -readpass aufgezeichnet")
			}
			if filepath.Dir(passPath) != ws.tmpDir {
				t.Errorf("Passwortdatei %q liegt nicht in TMPDIR %q", passPath, ws.tmpDir)
			}
			if forbidden != "" && strings.HasPrefix(passPath, forbidden+"/") {
				t.Errorf("Passwortdatei %q liegt im unsicheren Verzeichnis %q", passPath, forbidden)
			}
			if mode := strings.TrimSpace(ws.logFile("pass.mode.1")); mode != "600" {
				t.Errorf("Passwortdatei hat Modus %s, erwartet 600", mode)
			}
			if got := ws.logFile("pass.content.1"); got != codesignTestPassword {
				t.Errorf("Inhalt der Passwortdatei = %q, erwartet %q", got, codesignTestPassword)
			}
			ws.assertPasswordHidden(1, codesignTestPassword)
			if _, err := os.Stat(passPath); !os.IsNotExist(err) {
				t.Errorf("Passwortdatei %s nach dem Lauf noch vorhanden (%v)", passPath, err)
			}
			if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
				t.Errorf("TMPDIR nicht leer: %v", left)
			}
			if forbidden != "" {
				if left := dirEntries(t, forbidden); len(left) != 0 {
					t.Errorf("unsicheres Verzeichnis nicht leer: %v", left)
				}
			}
		})
	}
}

// TestCodesignRejectsOpenPKCS12 prüft, dass eine .p12 mit Gruppen- oder
// Fremdzugriff (0640, 0604, 0644, 0660, 0707) den Build mit Exit 1 und der
// Meldung "accessible by group/others" abbricht - bevor osslsigncode (und
// damit der private Schlüssel) benutzt wird. 0600 und 0400 sind erlaubt.
func TestCodesignRejectsOpenPKCS12(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644, 0o660, 0o707, 0o777} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			ws.setMode(ws.p12, mode)
			result := ws.run(codesignOptions{env: []string{
				"SSH_EASY_CODESIGN_PASSWORD=geheim",
				"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
				"SSH_EASY_CODESIGN_TSA=none",
			}})
			if result.exitCode == 0 {
				t.Errorf(".p12 mit Modus %04o nicht abgelehnt:\n%s", mode, result.output)
			}
			if !strings.Contains(result.output, "accessible by group/others") {
				t.Errorf("Meldung fehlt:\n%s", result.output)
			}
			if strings.Contains(result.output, "DONE") {
				t.Error("Ablauf lief trotz Abbruch weiter")
			}
			if calls := ws.calls(); len(calls) != 0 {
				t.Errorf("osslsigncode wurde aufgerufen: %v", calls)
			}
		})
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		t.Run(fmt.Sprintf("erlaubt %04o", mode), func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			ws.setMode(ws.p12, mode)
			result := ws.run(codesignOptions{env: []string{
				"SSH_EASY_CODESIGN_PASSWORD=geheim",
				"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
				"SSH_EASY_CODESIGN_TSA=none",
			}})
			if result.exitCode != 0 || ws.signCall() == 0 {
				t.Errorf(".p12 mit Modus %04o abgelehnt (Exit %d):\n%s", mode, result.exitCode, result.output)
			}
		})
	}
}

// TestCodesignMissingPKCS12 prüft den Randfall einer .p12, die nicht
// existiert: Abbruch mit Meldung, kein Stub-Aufruf (kein stilles Überspringen,
// sonst entstünde unbemerkt ein unsigniertes Release).
func TestCodesignMissingPKCS12(t *testing.T) {
	ws := newCodesignWorkspace(t)
	result := ws.run(codesignOptions{env: []string{
		"SSH_EASY_CODESIGN_PASSWORD=geheim",
		"SSH_EASY_CODESIGN_PKCS12=" + filepath.Join(ws.root, "fehlt.p12"),
		"SSH_EASY_CODESIGN_TSA=none",
	}})
	if result.exitCode == 0 || !strings.Contains(result.output, "missing file") {
		t.Errorf("fehlende .p12 nicht gemeldet (Exit %d):\n%s", result.exitCode, result.output)
	}
	if len(ws.calls()) != 0 {
		t.Errorf("osslsigncode wurde aufgerufen: %v", ws.calls())
	}
}

// TestCodesignPassfileOption prüft SSH_EASY_CODESIGN_PASSFILE: Mit Modus 0644,
// 0640 oder 0604 bricht das Skript ab (Exit 1, "accessible by group/others",
// Stub nie aufgerufen). Mit 0600 gelingt es: -readpass ist genau diese Datei,
// sie bleibt unverändert erhalten (gehört dem Nutzer, wird nicht gelöscht),
// und es entsteht keine zusätzliche temporäre Datei. Ein gleichzeitig
// gesetztes SSH_EASY_CODESIGN_PASSWORD ändert daran nichts.
func TestCodesignPassfileOption(t *testing.T) {
	newPassfile := func(ws *codesignWorkspace, mode os.FileMode) string {
		path := filepath.Join(ws.root, "pass.txt")
		if err := os.WriteFile(path, []byte("aus-der-datei"), 0o600); err != nil {
			t.Fatal(err)
		}
		ws.setMode(path, mode)
		return path
	}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o666} {
		t.Run(fmt.Sprintf("offen %04o", mode), func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			result := ws.run(codesignOptions{env: []string{
				"SSH_EASY_CODESIGN_PASSFILE=" + newPassfile(ws, mode),
				"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
				"SSH_EASY_CODESIGN_TSA=none",
			}})
			if result.exitCode == 0 || !strings.Contains(result.output, "accessible by group/others") {
				t.Errorf("Passwortdatei mit Modus %04o nicht abgelehnt (Exit %d):\n%s", mode, result.exitCode, result.output)
			}
			if len(ws.calls()) != 0 {
				t.Errorf("osslsigncode wurde aufgerufen: %v", ws.calls())
			}
		})
	}
	t.Run("0600 mit zusätzlichem Passwort", func(t *testing.T) {
		ws := newCodesignWorkspace(t)
		passfile := newPassfile(ws, 0o600)
		xdg := ws.newRuntimeDir("xdg", 0o700)
		result := ws.run(codesignOptions{env: []string{
			"SSH_EASY_CODESIGN_PASSFILE=" + passfile,
			"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword,
			"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
			"SSH_EASY_CODESIGN_TSA=none",
			"XDG_RUNTIME_DIR=" + xdg,
		}})
		if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
			t.Fatalf("Signieren schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
		}
		if got := ws.logFile("pass.path.1"); got != passfile {
			t.Errorf("-readpass = %q, erwartet genau %q", got, passfile)
		}
		if got := ws.logFile("pass.content.1"); got != "aus-der-datei" {
			t.Errorf("Inhalt der Passwortdatei = %q", got)
		}
		ws.assertPasswordHidden(1, codesignTestPassword)
		data, err := os.ReadFile(passfile)
		if err != nil || string(data) != "aus-der-datei" {
			t.Errorf("Passwortdatei des Nutzers verändert oder gelöscht: %q, %v", data, err)
		}
		if left := dirEntries(t, xdg); len(left) != 0 {
			t.Errorf("es wurde trotz PASSFILE eine temporäre Datei angelegt: %v", left)
		}
		if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
			t.Errorf("TMPDIR nicht leer: %v", left)
		}
	})
	t.Run("fehlende Datei", func(t *testing.T) {
		ws := newCodesignWorkspace(t)
		result := ws.run(codesignOptions{env: []string{
			"SSH_EASY_CODESIGN_PASSFILE=" + filepath.Join(ws.root, "fehlt.txt"),
			"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
			"SSH_EASY_CODESIGN_TSA=none",
		}})
		if result.exitCode == 0 || !strings.Contains(result.output, "missing file") || len(ws.calls()) != 0 {
			t.Errorf("fehlende Passwortdatei nicht abgewiesen (Exit %d, Aufrufe %v):\n%s", result.exitCode, ws.calls(), result.output)
		}
	})
}

// TestCodesignSourceOnlyDoesNotBuild prüft, dass das Laden mit source und
// SSH_EASY_BUILD_SOURCE_ONLY=1 nichts baut: keine Meldung ">>> Running tests",
// kein build/-Ordner neben dem Skript, die Funktionen sind definiert und
// bash läuft danach normal weiter. Ohne diese Sperre würde jeder Test den
// echten Build starten.
func TestCodesignSourceOnlyDoesNotBuild(t *testing.T) {
	ws := newCodesignWorkspace(t)
	result := ws.run(codesignOptions{
		command: `declare -F codesign_init codesign_files cleanup_codesign; echo LOADED`,
	})
	if result.exitCode != 0 || !strings.Contains(result.output, "LOADED") {
		t.Fatalf("Laden schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
	}
	for _, fn := range []string{"codesign_init", "codesign_files", "cleanup_codesign"} {
		if !strings.Contains(result.output, fn) {
			t.Errorf("Funktion %s nach dem Laden nicht definiert:\n%s", fn, result.output)
		}
	}
	for _, forbidden := range []string{">>> Running tests", ">>> Linux", ">>> Windows", "Build :"} {
		if strings.Contains(result.output, forbidden) {
			t.Errorf("beim Laden wurde gebaut (%q in der Ausgabe):\n%s", forbidden, result.output)
		}
	}
	if _, err := os.Stat(filepath.Join(ws.root, "build")); !os.IsNotExist(err) {
		t.Errorf("beim Laden wurde build/ angelegt (%v)", err)
	}
	if len(ws.calls()) != 0 {
		t.Errorf("osslsigncode wurde beim Laden aufgerufen: %v", ws.calls())
	}
}

// TestCodesignSkippedWithoutCertificate prüft den Normalfall ohne Zertifikat:
// ohne SSH_EASY_CODESIGN_PKCS12 (oder leer) meldet codesign_init "skipped",
// endet mit Erfolg und osslsigncode wird nie aufgerufen - auch dann nicht,
// wenn ein Passwort gesetzt ist.
func TestCodesignSkippedWithoutCertificate(t *testing.T) {
	cases := map[string][]string{
		"nicht gesetzt":     nil,
		"leer":              {"SSH_EASY_CODESIGN_PKCS12="},
		"mit Passwort":      {"SSH_EASY_CODESIGN_PASSWORD=geheim"},
		"nur Passwortdatei": {"SSH_EASY_CODESIGN_PASSFILE=/gibt/es/nicht"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			result := ws.run(codesignOptions{env: env})
			if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
				t.Fatalf("Lauf schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
			}
			if !strings.Contains(result.output, "skipped") {
				t.Errorf("Hinweis \"skipped\" fehlt:\n%s", result.output)
			}
			if calls := ws.calls(); len(calls) != 0 {
				t.Errorf("osslsigncode wurde aufgerufen: %v", calls)
			}
			if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
				t.Errorf("es entstand eine temporäre Datei: %v", left)
			}
		})
	}
}

// TestCodesignForeignPasswordIsIgnored prüft, dass ein von außen exportiertes
// CODESIGN_PASSWORD nie als Passwort übernommen wird: Ohne PASSFILE und ohne
// SSH_EASY_CODESIGN_PASSWORD bricht die Initialisierung ab ("password
// missing"), der Stub wird nie aufgerufen. Sonst könnte eine fremde Variable
// aus der Umgebung unbemerkt das Signieren steuern.
func TestCodesignForeignPasswordIsIgnored(t *testing.T) {
	ws := newCodesignWorkspace(t)
	result := ws.run(codesignOptions{env: []string{
		"CODESIGN_PASSWORD=fremd",
		"CODESIGN_PASSWORD_SET=1",
		"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
		"SSH_EASY_CODESIGN_TSA=none",
	}})
	if result.exitCode == 0 || !strings.Contains(result.output, "password missing") {
		t.Errorf("fehlendes Passwort nicht gemeldet (Exit %d):\n%s", result.exitCode, result.output)
	}
	if len(ws.calls()) != 0 {
		t.Errorf("osslsigncode wurde aufgerufen: %v", ws.calls())
	}
}

// TestCodesignEmptyPassword prüft den Randfall eines leeren, aber gesetzten
// SSH_EASY_CODESIGN_PASSWORD (eine .p12 ohne Passwort ist möglich): Es gilt
// als gesetzt, die Passwortdatei ist leer, 0600 und danach gelöscht.
func TestCodesignEmptyPassword(t *testing.T) {
	ws := newCodesignWorkspace(t)
	result := ws.run(codesignOptions{env: []string{
		"SSH_EASY_CODESIGN_PASSWORD=",
		"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
		"SSH_EASY_CODESIGN_TSA=none",
	}})
	if result.exitCode != 0 || ws.signCall() == 0 {
		t.Fatalf("Signieren mit leerem Passwort schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
	}
	if got := ws.logFile("pass.content.1"); got != "" {
		t.Errorf("Passwortdatei nicht leer: %q", got)
	}
	if mode := strings.TrimSpace(ws.logFile("pass.mode.1")); mode != "600" {
		t.Errorf("Passwortdatei hat Modus %s, erwartet 600", mode)
	}
	if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
		t.Errorf("Passwortdatei nicht gelöscht: %v", left)
	}
}

// TestCodesignCleansUpAfterSignFailure prüft den Abbruch mitten im Vorgang:
// Scheitert osslsigncode sign (Exit 3), bricht das Skript mit Exit 1 ab, löscht
// die halbfertige .signing-Datei, lässt die Zieldatei unverändert und
// entfernt die temporäre Passwortdatei über den EXIT-Trap.
func TestCodesignCleansUpAfterSignFailure(t *testing.T) {
	ws := newCodesignWorkspace(t)
	xdg := ws.newRuntimeDir("xdg", 0o700)
	result := ws.run(codesignOptions{
		signFail: true,
		env: []string{
			"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword,
			"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
			"SSH_EASY_CODESIGN_TSA=none",
			"XDG_RUNTIME_DIR=" + xdg,
		},
	})
	if result.exitCode == 0 || !strings.Contains(result.output, "failed") {
		t.Errorf("Fehler beim Signieren nicht gemeldet (Exit %d):\n%s", result.exitCode, result.output)
	}
	if strings.Contains(result.output, "DONE") {
		t.Error("Ablauf lief trotz Abbruch weiter")
	}
	passPath := ws.logFile("pass.path.1")
	if passPath == "" {
		t.Fatal("Stub hat keine Passwortdatei gesehen")
	}
	if _, err := os.Stat(passPath); !os.IsNotExist(err) {
		t.Errorf("Passwortdatei %s nach dem Abbruch noch vorhanden (%v)", passPath, err)
	}
	if left := dirEntries(t, xdg); len(left) != 0 {
		t.Errorf("XDG_RUNTIME_DIR nicht leer: %v", left)
	}
	data, err := os.ReadFile(ws.target)
	if err != nil || string(data) != "attrappe" {
		t.Errorf("Zieldatei verändert oder gelöscht: %q, %v", data, err)
	}
	if _, err := os.Stat(ws.target + ".signing"); !os.IsNotExist(err) {
		t.Errorf(".signing-Datei übrig (%v)", err)
	}
}

// ---------------------------------------------------------------------------
// Build 90, Teil 2: frühe Prüfung (codesign_check_config), Deinstaller-Signatur
// (--codesign-uninstaller, run_makensis) und das NSIS-Skript.
//
// Sicherheitsregeln wie oben: Jede Skriptkopie liegt in einem leeren
// Testordner, GO wird in der Kopie auf einen Stub umgebogen (ein echter
// "go test" läuft nie), HOME zeigt in den Testordner (der echte
// Signaturschlüssel unter ~/.config/ssh-easy wird nie berührt).
// ---------------------------------------------------------------------------

// defaultNSISVersion ist die Ausgabe des Stub-makensis bei "-VERSION".
const defaultNSISVersion = "v3.10-2"

// emptyNSISVersion steht für einen makensis, der bei "-VERSION" nichts ausgibt.
const emptyNSISVersion = "<leer>"

// writeMakensisStub schreibt einen Stub-makensis vor den echten (PATH-Vorderteil).
// "-VERSION" liefert version; jeder andere Aufruf schreibt Argumente und
// Umgebung nach makensis.args / makensis.env und endet mit exitCode.
func (ws *codesignWorkspace) writeMakensisStub(version string, exitCode int) {
	ws.t.Helper()
	versionLine := ""
	if version != emptyNSISVersion {
		versionLine = fmt.Sprintf("echo '%s'", version)
	}
	script := fmt.Sprintf(`#!/bin/bash
LOG=%q
if [ "$1" = "-VERSION" ]; then
	%s
	exit 0
fi
printf '%%s\n' "$@" > "$LOG/makensis.args"
env > "$LOG/makensis.env"
echo "Output: stub-setup.exe"
if [ %d -ne 0 ]; then
	echo "Error: boom" >&2
fi
exit %d
`, ws.logDir, versionLine, exitCode, exitCode)
	if err := os.WriteFile(filepath.Join(ws.binDir, "makensis"), []byte(script), 0o700); err != nil {
		ws.t.Fatal(err)
	}
}

// newStubbedCodesignWorkspace legt ein Testverzeichnis an, in dem zusätzlich
// ein Stub-makensis (NSIS 3.10) den echten überlagert: Die Prüfung der
// NSIS-Version ist damit unabhängig vom Rechner.
func newStubbedCodesignWorkspace(t *testing.T) *codesignWorkspace {
	t.Helper()
	ws := newCodesignWorkspace(t)
	ws.writeMakensisStub(defaultNSISVersion, 0)
	return ws
}

// writePassfile legt eine Passwortdatei mit dem Testpasswort an.
func (ws *codesignWorkspace) writePassfile(mode os.FileMode) string {
	ws.t.Helper()
	path := filepath.Join(ws.root, "pass.txt")
	if err := os.WriteFile(path, []byte(codesignTestPassword), 0o600); err != nil {
		ws.t.Fatal(err)
	}
	ws.setMode(path, mode)
	return path
}

// signingEnv liefert eine gültige Signatur-Konfiguration (.p12 + 0600-Passwortdatei).
func (ws *codesignWorkspace) signingEnv() []string {
	ws.t.Helper()
	return []string{
		"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
		"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o600),
		"SSH_EASY_CODESIGN_TSA=none",
	}
}

// restrictedPath legt einen Ordner mit Symlinks auf nur die für die Prüfung
// nötigen Programme an (kein osslsigncode, kein makensis). Mit withOssl
// kommt der Stub-osslsigncode dazu. Als PATH benutzt, lässt sich damit
// "Programm fehlt" nachstellen, ohne den echten PATH zu verbiegen.
func (ws *codesignWorkspace) restrictedPath(withOssl bool) string {
	ws.t.Helper()
	dir := filepath.Join(ws.root, "restricted")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		ws.t.Fatal(err)
	}
	for _, name := range []string{"dirname", "basename", "stat", "head", "cat", "mktemp", "rm", "mkdir"} {
		real, err := exec.LookPath(name)
		if err != nil {
			ws.t.Skipf("%s nicht gefunden", name)
		}
		if err := os.Symlink(real, filepath.Join(dir, name)); err != nil {
			ws.t.Fatal(err)
		}
	}
	if withOssl {
		if err := os.Symlink(filepath.Join(ws.binDir, "osslsigncode"), filepath.Join(dir, "osslsigncode")); err != nil {
			ws.t.Fatal(err)
		}
	}
	return dir
}

// assertNothingCreated prüft, dass in TMPDIR und im XDG-Ordner nichts entstand
// (die Prüfung hat keine Nebenwirkungen) und osslsigncode nie lief.
func (ws *codesignWorkspace) assertNothingCreated(xdg string) {
	ws.t.Helper()
	if left := dirEntries(ws.t, ws.tmpDir); len(left) != 0 {
		ws.t.Errorf("TMPDIR nicht leer: %v", left)
	}
	if xdg != "" {
		if left := dirEntries(ws.t, xdg); len(left) != 0 {
			ws.t.Errorf("XDG_RUNTIME_DIR nicht leer: %v", left)
		}
	}
	if calls := ws.calls(); len(calls) != 0 {
		ws.t.Errorf("osslsigncode wurde aufgerufen: %v", calls)
	}
}

// writeScriptCopy schreibt eine Kopie der Skriptkopie, in der GO auf einen
// Stub zeigt (jeder Aufruf wird in log/go.calls vermerkt, Exit 0). Mit
// removeEarlyCheck fehlt zusätzlich die Zeile "codesign_check_config" im
// Rumpf von main (Gegenprobe). Weicht das Skript vom erwarteten Aufbau ab,
// scheitert der Test sofort, statt versehentlich ein echtes go zu starten.
func (ws *codesignWorkspace) writeScriptCopy(name string, removeEarlyCheck bool) string {
	ws.t.Helper()
	stub := filepath.Join(ws.binDir, "go-stub")
	stubScript := fmt.Sprintf("#!/bin/bash\nprintf '%%s\\n' \"$*\" >> %q\nexit 0\n", filepath.Join(ws.logDir, "go.calls"))
	if err := os.WriteFile(stub, []byte(stubScript), 0o700); err != nil {
		ws.t.Fatal(err)
	}
	data, err := os.ReadFile(ws.script)
	if err != nil {
		ws.t.Fatal(err)
	}
	text := string(data)
	const goLine = "\nGO=/usr/local/go/bin/go\n"
	if strings.Count(text, goLine) != 1 {
		ws.t.Fatalf("build.sh enthält die Zeile %q nicht genau einmal - GO lässt sich nicht auf den Stub umbiegen", strings.TrimSpace(goLine))
	}
	text = strings.Replace(text, goLine, "\nGO="+stub+"\n", 1)
	if removeEarlyCheck {
		lines := strings.Split(text, "\n")
		mainAt, removed := -1, 0
		var kept []string
		for i, line := range lines {
			if strings.HasPrefix(line, "main() {") {
				mainAt = i
			}
			if line == "codesign_check_config" && mainAt >= 0 && i > mainAt {
				removed++
				continue
			}
			kept = append(kept, line)
		}
		if removed != 1 {
			ws.t.Fatalf("erwartet genau einen Aufruf \"codesign_check_config\" im Rumpf von main, gefunden: %d", removed)
		}
		text = strings.Join(kept, "\n")
	}
	path := filepath.Join(ws.root, name)
	if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
		ws.t.Fatal(err)
	}
	return path
}

// prepareMainDirs legt src/build.txt ("90") im Testordner an, damit main über
// die Prüfung hinaus bis zum go-Stub laufen kann.
func (ws *codesignWorkspace) prepareMainDirs() {
	ws.t.Helper()
	src := filepath.Join(ws.root, "src")
	if err := os.Mkdir(src, 0o700); err != nil {
		ws.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "build.txt"), []byte("90\n"), 0o600); err != nil {
		ws.t.Fatal(err)
	}
}

// runScript führt "bash <script> <args...>" aus, OHNE SSH_EASY_BUILD_SOURCE_ONLY
// (das Skript läuft also wirklich: main oder --codesign-uninstaller), höchstens
// 30 s. HOME und SSH_EASY_SIGNING_KEY zeigen in den Testordner.
func (ws *codesignWorkspace) runScript(script string, args []string, opts codesignOptions) codesignResult {
	ws.t.Helper()
	ws.writeStub(opts.signFail)
	var env []string
	for _, entry := range ws.buildEnv(nil) {
		if strings.HasPrefix(entry, "SSH_EASY_BUILD_SOURCE_ONLY=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+ws.root, "SSH_EASY_SIGNING_KEY="+filepath.Join(ws.root, "kein-schluessel"))
	env = append(env, opts.env...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Env = env
	cmd.Dir = ws.root
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	result := codesignResult{output: string(out)}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exitCode = exitErr.ExitCode()
	default:
		ws.t.Fatalf("bash nicht ausführbar: %v\n%s", err, out)
	}
	if ctx.Err() != nil {
		ws.t.Fatalf("bash-Lauf überschritt 30 s:\n%s", out)
	}
	return result
}

// ---------------------------------------------------------------------------
// A. codesign_check_config allein
// ---------------------------------------------------------------------------

// TestCodesignCheckConfigRejectsBadConfig prüft, dass jede Fehlkonfiguration
// der Signatur mit Exit 1 und passender Meldung abbricht (fehlende oder offene
// .p12 / Passwortdatei, fehlendes osslsigncode, keine Passwortquelle, fehlende
// CA-Datei, NSIS zu alt, unlesbar oder ohne Ausgabe). Die Prüfung ist rein:
// osslsigncode läuft nie, in TMPDIR und XDG_RUNTIME_DIR entsteht nichts.
// Randfälle: .p12 als Verzeichnis, makensis mit Müll- oder leerer Version.
func TestCodesignCheckConfigRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		nsis   string
		noOssl bool
		setup  func(ws *codesignWorkspace) []string
	}{
		{name: ".p12 fehlt", want: "SSH_EASY_CODESIGN_PKCS12 points to a missing file",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + filepath.Join(ws.root, "fehlt.p12"),
					"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o600)}
			}},
		{name: ".p12 ist ein Verzeichnis", want: "SSH_EASY_CODESIGN_PKCS12 points to a missing file",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.tmpDir,
					"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o600)}
			}},
		{name: ".p12 mit Modus 0644", want: "accessible by group/others",
			setup: func(ws *codesignWorkspace) []string {
				ws.setMode(ws.p12, 0o644)
				return ws.signingEnv()
			}},
		{name: ".p12 mit Modus 0604 (nur andere)", want: "accessible by group/others",
			setup: func(ws *codesignWorkspace) []string {
				ws.setMode(ws.p12, 0o604)
				return ws.signingEnv()
			}},
		{name: "Passwortdatei mit Modus 0644", want: "SSH_EASY_CODESIGN_PASSFILE is accessible by group/others",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
					"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o644)}
			}},
		{name: "Passwortdatei mit Modus 0640", want: "SSH_EASY_CODESIGN_PASSFILE is accessible by group/others",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
					"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o640)}
			}},
		{name: "Passwortdatei fehlt", want: "SSH_EASY_CODESIGN_PASSFILE points to a missing file",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
					"SSH_EASY_CODESIGN_PASSFILE=" + filepath.Join(ws.root, "fehlt.txt")}
			}},
		{name: "osslsigncode fehlt", want: "osslsigncode not found", noOssl: true,
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "keine Passwortquelle", want: "password missing",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12}
			}},
		{name: "fremdes CODESIGN_PASSWORD zählt nicht", want: "password missing",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "CODESIGN_PASSWORD=fremd", "CODESIGN_PASSWORD_SET=1"}
			}},
		{name: "CA-Datei fehlt", want: "SSH_EASY_CODESIGN_CAFILE points to a missing file",
			setup: func(ws *codesignWorkspace) []string {
				return append(ws.signingEnv(), "SSH_EASY_CODESIGN_CAFILE="+filepath.Join(ws.root, "fehlt-ca.pem"))
			}},
		{name: "NSIS 3.07", want: "NSIS >= 3.08", nsis: "v3.07",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS 3.06", want: "NSIS >= 3.08", nsis: "v3.06",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS 2.51", want: "NSIS >= 3.08", nsis: "v2.51",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS-Version unlesbar (kaputt)", want: "NSIS >= 3.08", nsis: "kaputt",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS-Version leer", want: "NSIS >= 3.08", nsis: emptyNSISVersion,
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			nsis := c.nsis
			if nsis == "" {
				nsis = defaultNSISVersion
			}
			ws.writeMakensisStub(nsis, 0)
			xdg := ws.newRuntimeDir("xdg", 0o700)
			env := append(c.setup(ws), "XDG_RUNTIME_DIR="+xdg)
			if c.noOssl {
				// PATH ohne osslsigncode (und ohne makensis)
				env = append(env, "PATH="+ws.restrictedPath(false))
			}
			result := ws.run(codesignOptions{env: env, command: `codesign_check_config; echo DONE`})
			if result.exitCode != 1 {
				t.Errorf("Exit %d, erwartet 1:\n%s", result.exitCode, result.output)
			}
			if !strings.Contains(result.output, c.want) {
				t.Errorf("Meldung %q fehlt:\n%s", c.want, result.output)
			}
			if strings.Contains(result.output, "DONE") {
				t.Error("Ablauf lief trotz Abbruch weiter")
			}
			ws.assertNothingCreated(xdg)
		})
	}
}

// TestCodesignCheckConfigAcceptsValidConfig prüft die Gegenseite: Korrekte
// Konfigurationen (Passwortdatei oder -variable, leeres Passwort, vorhandene
// CA-Datei, .p12 mit 0400, NSIS 3.08 / 3.10 / 4.0, kein makensis) und der
// Normalfall ohne Zertifikat enden mit Exit 0 und völlig stiller Ausgabe.
// Es entsteht nichts (auch nicht die Passwort-Tmp-Datei: die legt erst
// codesign_init an) und osslsigncode läuft nie. Nicht konfiguriert wird
// nichts geprüft, auch kein Müll in den anderen Variablen.
func TestCodesignCheckConfigAcceptsValidConfig(t *testing.T) {
	cases := []struct {
		name    string
		nsis    string
		noMake  bool // PATH ohne makensis (nur Stub-osslsigncode)
		noOssl  bool // PATH ganz ohne osslsigncode und makensis
		silent  bool // Ausgabe muss genau "DONE\n" sein
		setup   func(ws *codesignWorkspace) []string
		wantOut string
	}{
		{name: "nicht konfiguriert", silent: true,
			setup: func(ws *codesignWorkspace) []string { return nil }},
		{name: "PKCS12 leer", silent: true,
			setup: func(ws *codesignWorkspace) []string { return []string{"SSH_EASY_CODESIGN_PKCS12="} }},
		{name: "nicht konfiguriert: Müll in den übrigen Variablen wird nicht geprüft", silent: true, noOssl: true, nsis: "kaputt",
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PASSFILE=/gibt/es/nicht", "SSH_EASY_CODESIGN_CAFILE=/gibt/es/nicht"}
			}},
		{name: "Passwortdatei 0600", silent: true,
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: ".p12 mit 0400", silent: true,
			setup: func(ws *codesignWorkspace) []string {
				ws.setMode(ws.p12, 0o400)
				return ws.signingEnv()
			}},
		{name: "Passwortvariable (keine Tmp-Datei in der Prüfung)", silent: true,
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword}
			}},
		{name: "leeres Passwort (gesetzt)", silent: true,
			setup: func(ws *codesignWorkspace) []string {
				return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "SSH_EASY_CODESIGN_PASSWORD="}
			}},
		{name: "CA-Datei vorhanden", silent: true,
			setup: func(ws *codesignWorkspace) []string {
				return append(ws.signingEnv(), "SSH_EASY_CODESIGN_CAFILE="+ws.target)
			}},
		{name: "ohne makensis", silent: true, noMake: true,
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS 3.08 (Grenze)", silent: true, nsis: "v3.08",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS 3.10-2 (zweistellig)", silent: true, nsis: "v3.10-2",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS 3.11", silent: true, nsis: "v3.11",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS v4.0", silent: true, nsis: "v4.0",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "NSIS ohne v-Präfix", silent: true, nsis: "3.08",
			setup: func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{name: "beide Passwortquellen: nur Hinweis", wantOut: "both set",
			setup: func(ws *codesignWorkspace) []string {
				return append(ws.signingEnv(), "SSH_EASY_CODESIGN_PASSWORD=zusatz")
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			nsis := c.nsis
			if nsis == "" {
				nsis = defaultNSISVersion
			}
			ws.writeMakensisStub(nsis, 0)
			xdg := ws.newRuntimeDir("xdg", 0o700)
			env := append(c.setup(ws), "XDG_RUNTIME_DIR="+xdg)
			if c.noMake || c.noOssl {
				// Stub zuerst schreiben: der Symlink im PATH-Ordner zeigt darauf
				ws.writeStub(false)
				env = append(env, "PATH="+ws.restrictedPath(c.noMake))
			}
			result := ws.run(codesignOptions{env: env, command: `codesign_check_config; echo DONE`})
			if result.exitCode != 0 || !strings.HasSuffix(result.output, "DONE\n") {
				t.Fatalf("gültige Konfiguration abgelehnt (Exit %d):\n%s", result.exitCode, result.output)
			}
			// Das Laden von build.sh gibt immer das Banner aus; die Prüfung selbst nichts
			if c.silent && strings.TrimPrefix(result.output, "=== ssh-easy Build ===\n\n") != "DONE\n" {
				t.Errorf("Ausgabe nicht still: %q", result.output)
			}
			if c.wantOut != "" {
				if !strings.Contains(result.output, "NOTE:") || !strings.Contains(result.output, c.wantOut) {
					t.Errorf("Hinweis %q fehlt:\n%s", c.wantOut, result.output)
				}
				if strings.Contains(result.output, "ERROR") {
					t.Errorf("Hinweis darf kein Fehler sein:\n%s", result.output)
				}
			}
			ws.assertNothingCreated(xdg)
		})
	}
}

// TestCodesignCheckConfigRunsOnce prüft, dass die Prüfung nur einmal läuft
// (CODESIGN_CHECKED): Bei zwei Passwortquellen erscheint der Hinweis "NOTE"
// genau einmal, auch wenn main die Prüfung und danach codesign_init
// aufruft. PASSFILE gilt: -readpass ist genau die Datei des Nutzers.
func TestCodesignCheckConfigRunsOnce(t *testing.T) {
	ws := newStubbedCodesignWorkspace(t)
	env := append(ws.signingEnv(), "SSH_EASY_CODESIGN_PASSWORD=zusatz")
	result := ws.run(codesignOptions{
		env:     env,
		command: `codesign_check_config; codesign_check_config; codesign_init; codesign_files "$2"; echo DONE`,
	})
	if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
		t.Fatalf("Lauf schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
	}
	if n := strings.Count(result.output, "NOTE:"); n != 1 {
		t.Errorf("Hinweis NOTE erschien %d-mal, erwartet 1:\n%s", n, result.output)
	}
	if got := ws.logFile("pass.path.1"); got != filepath.Join(ws.root, "pass.txt") {
		t.Errorf("-readpass = %q, erwartet die Passwortdatei des Nutzers", got)
	}
	if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
		t.Errorf("TMPDIR nicht leer: %v", left)
	}
}

// TestCodesignInitRunsCheckFirst prüft, dass codesign_init die Prüfung selbst
// aufruft, BEVOR es die Passwort-Tmp-Datei anlegt: Mit gesetzter
// Passwortvariable und neuer Fehlkonfiguration (CA-Datei fehlt, NSIS 3.07)
// bricht es mit Exit 1 ab, und TMPDIR/XDG bleiben leer (sonst bliebe nach
// dem Abbruch eine Passwortdatei, falls der Trap fehlte).
func TestCodesignInitRunsCheckFirst(t *testing.T) {
	cases := map[string]struct {
		nsis  string
		extra []string
		want  string
	}{
		"CA-Datei fehlt": {extra: []string{"SSH_EASY_CODESIGN_CAFILE=/gibt/es/nicht.pem"},
			want: "SSH_EASY_CODESIGN_CAFILE points to a missing file"},
		"NSIS 3.07": {nsis: "v3.07", want: "NSIS >= 3.08"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ws := newCodesignWorkspace(t)
			nsis := c.nsis
			if nsis == "" {
				nsis = defaultNSISVersion
			}
			ws.writeMakensisStub(nsis, 0)
			xdg := ws.newRuntimeDir("xdg", 0o700)
			env := append([]string{
				"SSH_EASY_CODESIGN_PKCS12=" + ws.p12,
				"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword,
				"SSH_EASY_CODESIGN_TSA=none",
				"XDG_RUNTIME_DIR=" + xdg,
			}, c.extra...)
			result := ws.run(codesignOptions{env: env})
			if result.exitCode != 1 || !strings.Contains(result.output, c.want) {
				t.Errorf("Exit %d, Meldung %q fehlt:\n%s", result.exitCode, c.want, result.output)
			}
			ws.assertNothingCreated(xdg)
		})
	}
}

// ---------------------------------------------------------------------------
// B. Reihenfolge in main (Hauptprobe, Gegenprobe, gültige Konfiguration)
// ---------------------------------------------------------------------------

// mainFault beschreibt eine Fehlkonfiguration für die Reihenfolge-Tests.
type mainFault struct {
	name  string
	want  string
	setup func(ws *codesignWorkspace) []string
}

// mainFaults sind Fehlkonfigurationen, die codesign_check_config UND (später)
// codesign_init melden.
func mainFaults() []mainFault {
	return []mainFault{
		{".p12 mit Modus 0644", "accessible by group/others", func(ws *codesignWorkspace) []string {
			ws.setMode(ws.p12, 0o644)
			return ws.signingEnv()
		}},
		{"Passwortquelle fehlt", "password missing", func(ws *codesignWorkspace) []string {
			return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "SSH_EASY_CODESIGN_TSA=none"}
		}},
		{".p12 fehlt", "SSH_EASY_CODESIGN_PKCS12 points to a missing file", func(ws *codesignWorkspace) []string {
			return []string{"SSH_EASY_CODESIGN_PKCS12=" + filepath.Join(ws.root, "fehlt.p12"),
				"SSH_EASY_CODESIGN_PASSFILE=" + ws.writePassfile(0o600), "SSH_EASY_CODESIGN_TSA=none"}
		}},
		{"CA-Datei fehlt", "SSH_EASY_CODESIGN_CAFILE points to a missing file", func(ws *codesignWorkspace) []string {
			return append(ws.signingEnv(), "SSH_EASY_CODESIGN_CAFILE="+filepath.Join(ws.root, "fehlt-ca.pem"))
		}},
		{"NSIS zu alt", "NSIS >= 3.08", func(ws *codesignWorkspace) []string {
			ws.writeMakensisStub("v3.07", 0)
			return ws.signingEnv()
		}},
	}
}

// TestCodesignMainChecksBeforeTestsAndBuilds ist die Hauptprobe zu Punkt 3:
// main läuft wirklich (kein SOURCE_ONLY), und bei einer Fehlkonfiguration
// bricht es mit Exit 1 und Meldung ab, BEVOR "go test" oder ein Build
// startet: Das go-Stub wird nie aufgerufen, ">>> Running tests",
// ">>> Linux amd64" und "Build :" fehlen, es entsteht kein build/-Ordner.
// Ohne frühe Prüfung käme der Fehler erst nach Minuten (siehe Gegenprobe).
func TestCodesignMainChecksBeforeTestsAndBuilds(t *testing.T) {
	for _, fault := range mainFaults() {
		t.Run(fault.name, func(t *testing.T) {
			ws := newStubbedCodesignWorkspace(t)
			ws.prepareMainDirs()
			env := fault.setup(ws)
			script := ws.writeScriptCopy("build-main.sh", false)
			result := ws.runScript(script, nil, codesignOptions{env: env})
			if result.exitCode != 1 || !strings.Contains(result.output, fault.want) {
				t.Errorf("Exit %d, Meldung %q fehlt:\n%s", result.exitCode, fault.want, result.output)
			}
			if got := ws.logFile("go.calls"); got != "" {
				t.Errorf("go wurde vor der Prüfung aufgerufen:\n%s", got)
			}
			for _, forbidden := range []string{">>> Running tests", ">>> Linux amd64", "Build :"} {
				if strings.Contains(result.output, forbidden) {
					t.Errorf("%q in der Ausgabe, die Prüfung kam zu spät:\n%s", forbidden, result.output)
				}
			}
			if _, err := os.Stat(filepath.Join(ws.root, "build")); !os.IsNotExist(err) {
				t.Errorf("build/ wurde angelegt (%v)", err)
			}
			if len(ws.calls()) != 0 {
				t.Errorf("osslsigncode wurde aufgerufen: %v", ws.calls())
			}
		})
	}
}

// TestCodesignMainCounterProofEarlyCheck ist die Gegenprobe: In einer Kopie,
// aus der der Aufruf codesign_check_config in main entfernt ist, laufen bei
// derselben Fehlkonfiguration erst die Tests und Builds (go-Stub aufgerufen,
// ">>> Running tests" steht in der Ausgabe) und der Fehler kommt spät bei
// codesign_init. Das beweist, dass die Hauptprobe die frühe Prüfung wirklich
// misst und nicht nur zufällig besteht.
func TestCodesignMainCounterProofEarlyCheck(t *testing.T) {
	for _, fault := range mainFaults() {
		t.Run(fault.name, func(t *testing.T) {
			ws := newStubbedCodesignWorkspace(t)
			ws.prepareMainDirs()
			env := fault.setup(ws)
			script := ws.writeScriptCopy("build-main-ohne-pruefung.sh", true)
			result := ws.runScript(script, nil, codesignOptions{env: env})
			if result.exitCode != 1 || !strings.Contains(result.output, fault.want) {
				t.Errorf("Exit %d, Meldung %q fehlt (der späte Fehler muss trotzdem kommen):\n%s", result.exitCode, fault.want, result.output)
			}
			if !strings.Contains(ws.logFile("go.calls"), "test ./...") {
				t.Errorf("go test wurde ohne frühe Prüfung nicht aufgerufen: %q", ws.logFile("go.calls"))
			}
			if !strings.Contains(result.output, ">>> Running tests") || !strings.Contains(result.output, ">>> Linux amd64") {
				t.Errorf("Tests und Builds liefen nicht vor dem Fehler:\n%s", result.output)
			}
		})
	}
}

// TestCodesignMainValidConfigPassesCheck prüft, dass main mit gültiger
// Konfiguration die Prüfung besteht: Das go-Stub wird aufgerufen (zuerst
// "test ./..."), keine Konfigurationsmeldung steht in der Ausgabe, und
// codesign_init meldet "Authenticode: enabled". Ob der Lauf danach endet
// (hier fehlt assets/), ist egal.
func TestCodesignMainValidConfigPassesCheck(t *testing.T) {
	ws := newStubbedCodesignWorkspace(t)
	ws.prepareMainDirs()
	script := ws.writeScriptCopy("build-main.sh", false)
	result := ws.runScript(script, nil, codesignOptions{env: ws.signingEnv()})
	goCalls := strings.Split(strings.TrimSpace(ws.logFile("go.calls")), "\n")
	if len(goCalls) == 0 || goCalls[0] != "test ./..." {
		t.Fatalf("erster go-Aufruf = %q, erwartet \"test ./...\" (Prüfung nicht bestanden?):\n%s", goCalls, result.output)
	}
	for _, unwanted := range []string{"missing file", "accessible by group/others", "password missing", "not found", "NSIS >= 3.08"} {
		if strings.Contains(result.output, unwanted) {
			t.Errorf("Konfigurationsfehler %q trotz gültiger Konfiguration:\n%s", unwanted, result.output)
		}
	}
	for _, want := range []string{"Build : 90", "Version: 0.90.0", ">>> Running tests", ">>> Authenticode: enabled"} {
		if !strings.Contains(result.output, want) {
			t.Errorf("%q fehlt in der Ausgabe:\n%s", want, result.output)
		}
	}
}

// ---------------------------------------------------------------------------
// C. build.sh --codesign-uninstaller <Datei>
// ---------------------------------------------------------------------------

// TestCodesignUninstallerModeSigns prüft den internen Aufruf von makensis:
// "bash build.sh --codesign-uninstaller <Datei>" (ohne SOURCE_ONLY) signiert
// die Datei an Ort und Stelle, prüft sie mit verify, meldet "signed +
// verified: Uninstall.exe" (nicht den Zufallsnamen) und gibt keine Banner aus.
// Das Passwort steht weder in Umgebung noch Argumenten des Kindprozesses.
// Randfall: Pfad mit Leerzeichen, Apostroph und $ (NSIS liefert Temp-Pfade).
func TestCodesignUninstallerModeSigns(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		env      func(ws *codesignWorkspace) []string
	}{
		{"mit Passwortdatei", "app.exe", func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
		{"mit Passwortvariable", "app.exe", func(ws *codesignWorkspace) []string {
			return []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "SSH_EASY_CODESIGN_TSA=none",
				"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword}
		}},
		{"Pfad mit Sonderzeichen", "Un inst'all $x.exe", func(ws *codesignWorkspace) []string { return ws.signingEnv() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := newStubbedCodesignWorkspace(t)
			target := filepath.Join(ws.root, c.fileName)
			if err := os.WriteFile(target, []byte("attrappe"), 0o600); err != nil {
				t.Fatal(err)
			}
			script := ws.writeScriptCopy("build-copy.sh", false)
			result := ws.runScript(script, []string{"--codesign-uninstaller", target}, codesignOptions{env: c.env(ws)})
			if result.exitCode != 0 {
				t.Fatalf("Exit %d:\n%s", result.exitCode, result.output)
			}
			if got := strings.Join(ws.calls(), ","); got != "sign,verify" {
				t.Fatalf("Stub-Aufrufe = %q, erwartet sign,verify", got)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "signiert\n" {
				t.Errorf("Datei nicht vom Stub ersetzt: %q, %v", data, err)
			}
			if !strings.Contains(result.output, "signed + verified: Uninstall.exe") {
				t.Errorf("Meldung fehlt:\n%s", result.output)
			}
			if strings.Contains(result.output, "Authenticode: enabled") || strings.Contains(result.output, "Running tests") {
				t.Errorf("Banner oder Build in der Ausgabe:\n%s", result.output)
			}
			ws.assertPasswordHidden(1, codesignTestPassword)
			ws.assertPasswordHidden(2, codesignTestPassword)
			if _, err := os.Stat(target + ".signing"); !os.IsNotExist(err) {
				t.Errorf(".signing-Datei übrig (%v)", err)
			}
			if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
				t.Errorf("TMPDIR nicht leer: %v", left)
			}
			if _, err := os.Stat(filepath.Join(ws.root, "build")); !os.IsNotExist(err) {
				t.Errorf("es wurde gebaut (build/ vorhanden, %v)", err)
			}
		})
	}
}

// TestCodesignUninstallerModeRejects prüft die Fehlerfälle: ohne eingerichtete
// Signatur Exit 1 "not configured" (nie stilles Überspringen: sonst entstünde
// ein unsignierter Deinstaller), ohne Argument, mit fehlender Datei, einem
// Verzeichnis oder zwei Argumenten Exit 1 mit "needs the path", eine offene
// .p12 ebenso, und scheitert sign (Exit 3), bleibt die Datei unverändert,
// ohne .signing-Rest. In allen Fällen bleibt die Datei erhalten.
func TestCodesignUninstallerModeRejects(t *testing.T) {
	cases := []struct {
		name     string
		want     string
		args     func(target string) []string
		env      func(ws *codesignWorkspace) []string
		signFail bool
		calls    int // erwartete Stub-Aufrufe
	}{
		{"ohne Konfiguration", "not configured",
			func(f string) []string { return []string{"--codesign-uninstaller", f} },
			func(ws *codesignWorkspace) []string { return nil }, false, 0},
		{"kein Argument", "needs the path",
			func(f string) []string { return []string{"--codesign-uninstaller"} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, false, 0},
		{"leeres Argument", "needs the path",
			func(f string) []string { return []string{"--codesign-uninstaller", ""} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, false, 0},
		{"Datei fehlt", "needs the path",
			func(f string) []string { return []string{"--codesign-uninstaller", f + ".fehlt"} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, false, 0},
		{"Verzeichnis statt Datei", "needs the path",
			func(f string) []string { return []string{"--codesign-uninstaller", filepath.Dir(f)} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, false, 0},
		{"zwei Argumente", "needs the path",
			func(f string) []string { return []string{"--codesign-uninstaller", f, f} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, false, 0},
		{".p12 mit Modus 0644", "accessible by group/others",
			func(f string) []string { return []string{"--codesign-uninstaller", f} },
			func(ws *codesignWorkspace) []string {
				ws.setMode(ws.p12, 0o644)
				return ws.signingEnv()
			}, false, 0},
		{"sign scheitert", "failed",
			func(f string) []string { return []string{"--codesign-uninstaller", f} },
			func(ws *codesignWorkspace) []string { return ws.signingEnv() }, true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := newStubbedCodesignWorkspace(t)
			script := ws.writeScriptCopy("build-copy.sh", false)
			result := ws.runScript(script, c.args(ws.target), codesignOptions{env: c.env(ws), signFail: c.signFail})
			if result.exitCode != 1 || !strings.Contains(result.output, c.want) {
				t.Errorf("Exit %d, Meldung %q fehlt:\n%s", result.exitCode, c.want, result.output)
			}
			if got := len(ws.calls()); got != c.calls {
				t.Errorf("Stub-Aufrufe = %v, erwartet %d", ws.calls(), c.calls)
			}
			data, err := os.ReadFile(ws.target)
			if err != nil || string(data) != "attrappe" {
				t.Errorf("Datei verändert oder gelöscht: %q, %v", data, err)
			}
			if _, err := os.Stat(ws.target + ".signing"); !os.IsNotExist(err) {
				t.Errorf(".signing-Datei übrig (%v)", err)
			}
			if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
				t.Errorf("TMPDIR nicht leer: %v", left)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// D. run_makensis
// ---------------------------------------------------------------------------

// envNames liest die Variablennamen einer Umgebungs-Aufzeichnung (env > Datei).
func envNames(envLog string) map[string]string {
	vars := map[string]string{}
	for _, line := range strings.Split(envLog, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			vars[name] = value
		}
	}
	return vars
}

// runMakensisCommand ruft run_makensis mit gesetzten BUILD/VERSION und
// vorheriger Initialisierung auf. Im Testordner liegt eine leere
// ssh-easy-setup.nsi (cwd des Laufs).
func (ws *codesignWorkspace) runMakensisCommand(arch string, env []string) codesignResult {
	ws.t.Helper()
	if err := os.WriteFile(filepath.Join(ws.root, "ssh-easy-setup.nsi"), nil, 0o600); err != nil {
		ws.t.Fatal(err)
	}
	return ws.run(codesignOptions{
		env:     env,
		command: `BUILD=90; VERSION=0.90.0; codesign_init; run_makensis ` + arch + `; echo DONE`,
	})
}

// TestCodesignRunMakensisWithSignature prüft run_makensis bei aktiver
// Signatur: makensis bekommt -DSIGN_UNINSTALLER=1 (und ARCH/VERSION/BUILD),
// in seiner Umgebung stehen nur Pfade (SSH_EASY_SIGN_SCRIPT = die Skriptdatei,
// _PKCS12, _PASSFILE, _TSA, _CAFILE), das Passwort kommt nirgends vor, weder
// als Wert noch als Variablenname (SSH_EASY_CODESIGN_PASSWORD, CODESIGN_PASSWORD).
// Beide Wege: Passwortdatei des Nutzers und Passwortvariable (Tmp-Datei).
func TestCodesignRunMakensisWithSignature(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, viaVariable := range []bool{false, true} {
			name := arch + "/Passwortdatei"
			if viaVariable {
				name = arch + "/Passwortvariable"
			}
			t.Run(name, func(t *testing.T) {
				ws := newStubbedCodesignWorkspace(t)
				env := ws.signingEnv()
				if viaVariable {
					env = []string{"SSH_EASY_CODESIGN_PKCS12=" + ws.p12, "SSH_EASY_CODESIGN_TSA=none",
						"SSH_EASY_CODESIGN_PASSWORD=" + codesignTestPassword}
				}
				result := ws.runMakensisCommand(arch, env)
				if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
					t.Fatalf("Lauf schlug fehl (Exit %d):\n%s", result.exitCode, result.output)
				}
				args := strings.Split(strings.TrimSpace(ws.logFile("makensis.args")), "\n")
				for _, want := range []string{"-DSIGN_UNINSTALLER=1", "-DARCH=" + arch, "-DVERSION=0.90.0", "-DBUILD=90"} {
					found := false
					for _, a := range args {
						found = found || a == want
					}
					if !found {
						t.Errorf("Argument %q fehlt: %v", want, args)
					}
				}
				if args[len(args)-1] != "ssh-easy-setup.nsi" {
					t.Errorf("letztes Argument = %q, erwartet ssh-easy-setup.nsi", args[len(args)-1])
				}
				envLog := ws.logFile("makensis.env")
				vars := envNames(envLog)
				if vars["SSH_EASY_SIGN_SCRIPT"] != ws.script {
					t.Errorf("SSH_EASY_SIGN_SCRIPT = %q, erwartet %q", vars["SSH_EASY_SIGN_SCRIPT"], ws.script)
				}
				if vars["SSH_EASY_CODESIGN_PKCS12"] != ws.p12 {
					t.Errorf("SSH_EASY_CODESIGN_PKCS12 = %q, erwartet %q", vars["SSH_EASY_CODESIGN_PKCS12"], ws.p12)
				}
				if vars["SSH_EASY_CODESIGN_TSA"] != "none" {
					t.Errorf("SSH_EASY_CODESIGN_TSA = %q, erwartet none", vars["SSH_EASY_CODESIGN_TSA"])
				}
				if _, ok := vars["SSH_EASY_CODESIGN_CAFILE"]; !ok {
					t.Error("SSH_EASY_CODESIGN_CAFILE fehlt (leer erwartet)")
				}
				passfile := vars["SSH_EASY_CODESIGN_PASSFILE"]
				if passfile == "" {
					t.Error("SSH_EASY_CODESIGN_PASSFILE fehlt")
				}
				if viaVariable && filepath.Dir(passfile) != ws.tmpDir {
					t.Errorf("Passwortdatei %q liegt nicht in TMPDIR", passfile)
				}
				for name := range vars {
					switch name {
					case "SSH_EASY_CODESIGN_PASSWORD", "CODESIGN_PASSWORD", "CODESIGN_PASSWORD_SET":
						t.Errorf("Passwortvariable %s steht in der Umgebung von makensis", name)
					}
				}
				if strings.Contains(envLog, codesignTestPassword) || strings.Contains(strings.Join(args, "\n"), codesignTestPassword) {
					t.Error("das Testpasswort steht in Umgebung oder Argumenten von makensis")
				}
				if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
					t.Errorf("TMPDIR nicht leer (Log oder Passwortdatei übrig): %v", left)
				}
			})
		}
	}
}

// TestCodesignRunMakensisFailureAborts prüft den Abbruch: Mit Signatur und
// makensis-Exit 2 bricht der Build mit Exit 1 und "makensis (amd64) failed
// (exit 2)" ab (samt Fehlerzeile des Logs); sonst entstünde ein Setup mit
// unsigniertem Deinstaller. Ohne Signatur gilt das alte Verhalten (Rückgabewert
// ignoriert, Exit 0, kein -DSIGN_UNINSTALLER, keine SSH_EASY_SIGN_*-Umgebung).
func TestCodesignRunMakensisFailureAborts(t *testing.T) {
	t.Run("mit Signatur, Exit 2", func(t *testing.T) {
		ws := newStubbedCodesignWorkspace(t)
		ws.writeMakensisStub(defaultNSISVersion, 2)
		result := ws.runMakensisCommand("amd64", ws.signingEnv())
		if result.exitCode != 1 || !strings.Contains(result.output, "makensis (amd64) failed (exit 2)") {
			t.Errorf("Abbruch fehlt (Exit %d):\n%s", result.exitCode, result.output)
		}
		if !strings.Contains(result.output, "boom") {
			t.Errorf("Fehlerzeile des makensis-Logs fehlt:\n%s", result.output)
		}
		if strings.Contains(result.output, "DONE") {
			t.Error("Ablauf lief trotz Abbruch weiter")
		}
		if left := dirEntries(t, ws.tmpDir); len(left) != 0 {
			t.Errorf("TMPDIR nicht leer: %v", left)
		}
	})
	for _, exitCode := range []int{0, 2} {
		t.Run(fmt.Sprintf("ohne Signatur, Exit %d", exitCode), func(t *testing.T) {
			ws := newStubbedCodesignWorkspace(t)
			ws.writeMakensisStub(defaultNSISVersion, exitCode)
			result := ws.runMakensisCommand("amd64", nil)
			if result.exitCode != 0 || !strings.Contains(result.output, "DONE") {
				t.Fatalf("altes Verhalten verletzt (Exit %d):\n%s", result.exitCode, result.output)
			}
			if strings.Contains(result.output, "failed") {
				t.Errorf("Fehlermeldung trotz fehlender Signatur:\n%s", result.output)
			}
			args := ws.logFile("makensis.args")
			if args == "" {
				t.Fatal("makensis wurde nicht aufgerufen")
			}
			if strings.Contains(args, "SIGN_UNINSTALLER") {
				t.Errorf("-DSIGN_UNINSTALLER ohne Signatur:\n%s", args)
			}
			if !strings.Contains(args, "-DARCH=amd64") {
				t.Errorf("-DARCH fehlt:\n%s", args)
			}
			for name := range envNames(ws.logFile("makensis.env")) {
				if name == "SSH_EASY_SIGN_SCRIPT" || strings.HasPrefix(name, "SSH_EASY_CODESIGN_") {
					t.Errorf("Variable %s steht ohne Signatur in der Umgebung von makensis", name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// E. ssh-easy-setup.nsi
// ---------------------------------------------------------------------------

// uninstfinalizeCommand liefert den Befehlstext der einzigen
// !uninstfinalize-Zeile (ohne "!uninstfinalize ") und meldet Fehler, wenn sie
// fehlt, mehrfach vorkommt oder nicht in "!ifdef SIGN_UNINSTALLER" steht.
func uninstfinalizeCommand(t *testing.T) string {
	t.Helper()
	var stack []string
	var found []string
	for _, line := range nsisCodeLines(readInstallerScript(t)) {
		fields := strings.Fields(line)
		switch fields[0] {
		case "!ifdef", "!ifndef", "!if", "!ifmacrodef", "!ifmacrondef":
			stack = append(stack, strings.Join(fields, " "))
		case "!else":
			// Zweig umgedreht: "!ifdef SIGN_UNINSTALLER" gilt hier nicht mehr
			if len(stack) > 0 {
				stack[len(stack)-1] += " <else>"
			}
		case "!endif":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case "!uninstfinalize":
			inside := false
			for _, entry := range stack {
				inside = inside || entry == "!ifdef SIGN_UNINSTALLER"
			}
			if !inside {
				t.Errorf("!uninstfinalize steht außerhalb von !ifdef SIGN_UNINSTALLER (Stapel %v): %s", stack, line)
			}
			found = append(found, strings.TrimSpace(strings.TrimPrefix(line, "!uninstfinalize")))
		case "!define":
			if len(fields) > 1 && fields[1] == "SIGN_UNINSTALLER" {
				t.Errorf("SIGN_UNINSTALLER darf nie im Skript gesetzt werden (nur durch build.sh): %s", line)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("!uninstfinalize kommt %d-mal vor, erwartet genau 1: %v", len(found), found)
	}
	return found[0]
}

// TestInstallerUninstfinalizeText prüft das NSIS-Skript als Text: Die einzige
// !uninstfinalize-Zeile steht innerhalb von !ifdef SIGN_UNINSTALLER, endet mit
// "= 0" (Pflicht: ohne baut NSIS bei Fehlschlag trotzdem ein Setup mit
// unsigniertem Deinstaller), ruft --codesign-uninstaller mit "%1" auf und
// holt den Skriptpfad aus $SSH_EASY_SIGN_SCRIPT (in Anführungszeichen). Kein
// fest eingebetteter Pfad: Sonderzeichen im Projektpfad (z. B. $ oder ')
// würden sonst von der Shell ausgewertet, und der Pfad wäre rechnerabhängig.
func TestInstallerUninstfinalizeText(t *testing.T) {
	command := uninstfinalizeCommand(t)
	if !regexp.MustCompile(`\s=\s*0$`).MatchString(command) {
		t.Errorf("der Befehl muss mit \"= 0\" enden: %s", command)
	}
	for _, want := range []string{"--codesign-uninstaller", `"%1"`, `bash "$SSH_EASY_SIGN_SCRIPT"`} {
		if !strings.Contains(command, want) {
			t.Errorf("%q fehlt im Befehl: %s", want, command)
		}
	}
	for _, forbidden := range []string{"build.sh", "/home", ":\\", "PASSWORD", "PASSFILE"} {
		if strings.Contains(command, forbidden) {
			t.Errorf("%q hat im Befehl nichts zu suchen (fester Pfad oder Passwort): %s", forbidden, command)
		}
	}
}

// nsisAtLeast308 liest "makensis -VERSION" ("v3.10-2") und prüft >= 3.08.
func nsisAtLeast308(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("makensis", "-VERSION").Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`^v?(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return false
	}
	var major, minor int
	fmt.Sscanf(m[1], "%d", &major)
	fmt.Sscanf(m[2], "%d", &minor)
	return major > 3 || (major == 3 && minor >= 8)
}

// TestInstallerUninstfinalizeRealMakensis ist ein echter Mini-Lauf mit dem
// installierten makensis (NSIS >= 3.08): Ein Wegwerf-Skript mit der echten
// !uninstfinalize-Zeile (per Text aus ssh-easy-setup.nsi gelesen) und einem
// Stub als SSH_EASY_SIGN_SCRIPT. Mit -DSIGN_UNINSTALLER=1 ruft NSIS den Stub
// mit dem Pfad des Deinstallers auf (die Datei existiert dann), das Setup
// entsteht und enthält den verlängerten Deinstaller (größer als ohne
// Signatur). Ohne das Define läuft der Stub nie. Liefert der Stub Exit 3,
// bricht makensis ab und es gibt KEIN Setup: das "= 0" wirkt wirklich.
func TestInstallerUninstfinalizeRealMakensis(t *testing.T) {
	requireCodesignEnvironment(t)
	if _, err := exec.LookPath("makensis"); err != nil {
		t.Skip("makensis nicht gefunden")
	}
	if !nsisAtLeast308(t) {
		t.Skip("makensis ist älter als 3.08")
	}
	command := uninstfinalizeCommand(t)
	type miniResult struct {
		exitCode int
		setup    int64 // Größe des Setups, -1 = nicht vorhanden
		calls    string
	}
	runMini := func(t *testing.T, define bool, stubExit int) miniResult {
		t.Helper()
		dir := t.TempDir()
		stub := filepath.Join(dir, "stub-sign.sh")
		calls := filepath.Join(dir, "calls")
		stubScript := fmt.Sprintf(`#!/bin/bash
if [ -f "$2" ]; then echo "$1 $2 exists" >> %q; else echo "$1 $2 missing" >> %q; fi
head -c 65536 /dev/urandom >> "$2"
exit %d
`, calls, calls, stubExit)
		if err := os.WriteFile(stub, []byte(stubScript), 0o700); err != nil {
			t.Fatal(err)
		}
		setup := filepath.Join(dir, "mini-setup.exe")
		nsi := fmt.Sprintf("OutFile %q\n!ifdef SIGN_UNINSTALLER\n  !uninstfinalize %s\n!endif\nSection\n  WriteUninstaller \"$TEMP\\u.exe\"\nSectionEnd\nSection \"Uninstall\"\nSectionEnd\n", setup, command)
		nsiPath := filepath.Join(dir, "mini.nsi")
		if err := os.WriteFile(nsiPath, []byte(nsi), 0o600); err != nil {
			t.Fatal(err)
		}
		args := []string{}
		if define {
			args = append(args, "-DSIGN_UNINSTALLER=1")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "makensis", append(args, nsiPath)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SSH_EASY_SIGN_SCRIPT="+stub, "TMPDIR="+dir)
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.CombinedOutput()
		res := miniResult{setup: -1}
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			res.exitCode = exitErr.ExitCode()
		default:
			t.Fatalf("makensis nicht ausführbar: %v\n%s", err, out)
		}
		if info, err := os.Stat(setup); err == nil {
			res.setup = info.Size()
		}
		if data, err := os.ReadFile(calls); err == nil {
			res.calls = string(data)
		}
		t.Logf("makensis Exit %d, Setup %d Bytes, Aufrufe %q", res.exitCode, res.setup, res.calls)
		return res
	}
	var unsignedSize int64
	t.Run("ohne Define: Stub läuft nie", func(t *testing.T) {
		res := runMini(t, false, 0)
		if res.exitCode != 0 || res.setup <= 0 {
			t.Fatalf("Setup ohne Signatur nicht gebaut (Exit %d, %d Bytes)", res.exitCode, res.setup)
		}
		if res.calls != "" {
			t.Errorf("Stub wurde ohne -DSIGN_UNINSTALLER aufgerufen: %q", res.calls)
		}
		unsignedSize = res.setup
	})
	t.Run("mit Define: Stub signiert den Deinstaller", func(t *testing.T) {
		res := runMini(t, true, 0)
		if res.exitCode != 0 || res.setup <= 0 {
			t.Fatalf("Setup nicht gebaut (Exit %d, %d Bytes)", res.exitCode, res.setup)
		}
		lines := strings.Split(strings.TrimSpace(res.calls), "\n")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "--codesign-uninstaller /") || !strings.HasSuffix(lines[0], " exists") {
			t.Errorf("Stub-Aufruf unerwartet: %q (erwartet ein Aufruf mit absolutem Pfad einer vorhandenen Datei)", res.calls)
		}
		if unsignedSize > 0 && res.setup < unsignedSize+50000 {
			t.Errorf("Setup (%d Bytes) enthält den verlängerten Deinstaller nicht (ohne Signatur %d Bytes)", res.setup, unsignedSize)
		}
	})
	t.Run("Stub scheitert (Exit 3): kein Setup", func(t *testing.T) {
		res := runMini(t, true, 3)
		if res.exitCode == 0 {
			t.Error("makensis meldet Erfolg trotz fehlgeschlagener Signatur")
		}
		if res.setup != -1 {
			t.Errorf("Setup trotz fehlgeschlagener Signatur entstanden (%d Bytes)", res.setup)
		}
		if res.calls == "" {
			t.Error("Stub wurde nicht aufgerufen")
		}
	})
}
