// Paket main - Datei-Manager: Datei lokal mit dem Standardprogramm öffnen
//
// F9 im Betrachter lädt die Datei in einen privaten Temp-Ordner dieses
// Rechners und übergibt sie dem Programm, das das Betriebssystem für die
// Dateiendung vorsieht (Windows: ShellExecute "open", Linux: xdg-open,
// macOS: open). Ein .jpg landet so im Bildbetrachter, ein .html im Browser.
//
// Sicherheit (die Datei stammt vom Server und ist nicht vertrauenswürdig):
//   - Ausführbare und Skript-Typen (.exe, .bat, .ps1, .lnk, .hta, .desktop …)
//     werden NICHT geöffnet - "Öffnen" hieße dort "Ausführen".
//   - Der lokale Name wird Windows-sicher gemacht (keine : \ < > | ? *,
//     keine reservierten Namen wie CON/NUL, keine Punkte/Leerzeichen am Ende,
//     die Windows sonst stillschweigend entfernt: "x.exe." wäre x.exe).
//   - Jede Datei liegt in einem eigenen, nur für den Nutzer zugänglichen
//     Ordner (0700, Datei 0600, exklusiv angelegt).
//   - Windows: Kennzeichnung "aus dem Internet" (Mark of the Web), damit
//     Office & Co. die geschützte Ansicht nutzen und SmartScreen warnt.
//   - Beim Programmende wird der Temp-Ordner gelöscht; Reste (z. B. weil
//     ein Programm die Datei noch offen hatte) räumt der nächste Start auf.
//
// @author Kurt Ingwer
// @date   2026-10-22 00:35
package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// openTempPrefix - Namensanfang der Temp-Ordner (für das Aufräumen alter Reste)
const openTempPrefix = "ssh-easy-open-"

// fmOpenMaxBytes - größte Datei, die zum Öffnen heruntergeladen wird
var fmOpenMaxBytes int64 = 1 << 30

// openTempRoot - Basis der Temp-Ordner ("" = Temp-Ordner des Systems; für Tests)
var openTempRoot = ""

// externalOpener öffnet eine lokale Datei mit dem Standardprogramm
// (plattformabhängig, für Tests austauschbar).
var externalOpener = platformOpenFile

// openSession - Temp-Ordner dieser Programmsitzung (lazy angelegt)
var openSession struct {
	mu  sync.Mutex
	dir string
}

// blockedOpenExtensions - Typen, bei denen "Öffnen" = "Ausführen" wäre
// (Windows-Programme, Skripte, Verknüpfungen, Installer, Datenträgerabbilder,
// Linux-Starter und Shell-Skripte).
var blockedOpenExtensions = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".scr": true, ".pif": true,
	".cpl": true, ".msi": true, ".msp": true, ".mst": true, ".msc": true, ".dll": true,
	".sys": true, ".drv": true, ".ocx": true, ".ps1": true, ".psm1": true, ".psd1": true,
	".ps1xml": true, ".psc1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true,
	".wsf": true, ".wsh": true, ".wsc": true, ".hta": true, ".lnk": true, ".url": true,
	".reg": true, ".inf": true, ".scf": true, ".jar": true, ".chm": true, ".gadget": true,
	".application": true, ".appref-ms": true, ".library-ms": true, ".settingcontent-ms": true,
	".search-ms": true, ".diagcab": true, ".cab": true, ".appx": true, ".appxbundle": true,
	".msix": true, ".msixbundle": true, ".iso": true, ".img": true, ".vhd": true, ".vhdx": true,
	".xll": true, ".desktop": true, ".sh": true, ".bash": true, ".zsh": true, ".csh": true,
	".run": true, ".bin": true, ".appimage": true, ".command": true, ".app": true,
}

// windowsReservedNames - Gerätenamen, die unter Windows keine Datei sein dürfen
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// localOpenName macht aus einem Dateinamen vom Server einen sicheren
// lokalen Namen (unter Windows und Linux gültig, Endung bleibt erhalten).
//
// @param name - Name vom Server
// @return string - lokaler Name (nie leer)
// @date   2026-10-22 00:35
func localOpenName(name string) string {
	var out strings.Builder
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		i += size
		switch {
		case r == utf8.RuneError && size <= 1, unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			out.WriteByte('_')
		case strings.ContainsRune("<>:\"/\\|?*", r):
			out.WriteByte('_')
		default:
			out.WriteRune(r)
		}
	}
	// Windows entfernt Punkte und Leerzeichen am Ende stillschweigend
	local := strings.TrimRight(out.String(), ". ")
	if local == "" {
		local = "datei"
	}
	base := strings.ToLower(strings.TrimSuffix(local, filepath.Ext(local)))
	if windowsReservedNames[base] {
		local = "_" + local
	}
	// Länge begrenzen, Endung behalten
	if len(local) > 150 {
		ext := filepath.Ext(local)
		if len(ext) > 20 {
			ext = ""
		}
		cut := 150 - len(ext)
		for cut > 0 && !utf8.RuneStart(local[cut]) {
			cut--
		}
		local = local[:cut] + ext
	}
	return local
}

// blockedOpenExtension prüft, ob ein (lokaler) Name einen gesperrten Typ hat.
//
// @param localName - Ergebnis von localOpenName
// @return string - Endung in Kleinbuchstaben (mit Punkt)
// @return bool - true wenn gesperrt
// @date   2026-10-22 00:35
func blockedOpenExtension(localName string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(localName))
	return ext, blockedOpenExtensions[ext]
}

// downloadForOpen lädt eine Datei in einen eigenen privaten Ordner.
//
// @param fsys - entferntes Dateisystem
// @param remotePath - Pfad auf dem Server
// @param localName - sicherer lokaler Name
// @return string - lokaler Pfad
// @return error - Fehler beim Laden/Schreiben oder Datei zu groß
// @date   2026-10-22 00:35
func downloadForOpen(fsys remoteFS, remotePath, localName string) (local string, err error) {
	session, err := openSessionDir()
	if err != nil {
		return "", err
	}
	// Eigener Unterordner je Datei: gleiche Namen überschreiben sich nie
	dir, err := os.MkdirTemp(session, "f")
	if err != nil {
		return "", err
	}
	local = filepath.Join(dir, localName)
	reader, err := fsys.OpenRead(remotePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	file, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(local)
		}
	}()
	written, err := io.Copy(file, io.LimitReader(reader, fmOpenMaxBytes+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written > fmOpenMaxBytes {
		err = errOpenTooLarge
	}
	if err != nil {
		return "", err
	}
	markDownloaded(local)
	return local, nil
}

// errOpenTooLarge - Datei überschreitet fmOpenMaxBytes
var errOpenTooLarge = &openError{"file too large to open locally"}

// openError ist ein einfacher Fehler mit festem Text.
type openError struct{ text string }

// Error liefert den Fehlertext.
//
// @date   2026-10-22 00:35
func (e *openError) Error() string { return e.text }

// openSessionDir liefert (und legt bei Bedarf an) den Temp-Ordner dieser
// Sitzung - nur für den Nutzer zugänglich.
//
// @return string - Ordner
// @return error - Fehler beim Anlegen
// @date   2026-10-22 00:35
func openSessionDir() (string, error) {
	openSession.mu.Lock()
	defer openSession.mu.Unlock()
	if openSession.dir != "" {
		if _, err := os.Stat(openSession.dir); err == nil {
			return openSession.dir, nil
		}
	}
	root := openTempRoot
	if root == "" {
		root = os.TempDir()
	}
	dir, err := os.MkdirTemp(root, openTempPrefix)
	if err != nil {
		return "", err
	}
	openSession.dir = dir
	return dir, nil
}

// cleanupExternalOpen löscht den Temp-Ordner dieser Sitzung (beim Beenden).
//
// @date   2026-10-22 00:35
func cleanupExternalOpen() {
	openSession.mu.Lock()
	defer openSession.mu.Unlock()
	if openSession.dir != "" {
		_ = os.RemoveAll(openSession.dir)
		openSession.dir = ""
	}
}

// cleanupStaleOpenDirs entfernt Temp-Ordner früherer Sitzungen, die älter
// als maxAge sind (z. B. weil ein Programm die Datei beim Beenden noch
// geöffnet hatte). Fremde Ordner bleiben unberührt.
//
// @param root - Temp-Basis ("" = Temp-Ordner des Systems)
// @param maxAge - Mindestalter
// @date   2026-10-22 00:35
func cleanupStaleOpenDirs(root string, maxAge time.Duration) {
	if root == "" {
		root = os.TempDir()
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), openTempPrefix) {
			continue
		}
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}
