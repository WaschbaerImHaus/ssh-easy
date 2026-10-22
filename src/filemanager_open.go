// Paket main - Datei-Manager: Datei lokal mit dem Standardprogramm öffnen
//
// F9 im Betrachter lädt die Datei in einen privaten Temp-Ordner dieses
// Rechners und übergibt sie dem Programm, das das Betriebssystem für die
// Dateiendung vorsieht (Windows: ShellExecute "open", Linux: xdg-open,
// macOS: open). Ein .jpg landet so im Bildbetrachter, ein .html im Browser.
//
// Sicherheit (die Datei stammt vom Server und ist nicht vertrauenswürdig):
//   - Nur bekannte Dokument- und Medientypen werden geöffnet (Erlaubnisliste:
//     Bilder, PDF, Text, HTML, Office ohne Makro-Varianten, Audio/Video,
//     Archive). Alles andere - insbesondere Programme, Skripte und
//     Verknüpfungen (.exe, .bat, .ps1, .lnk, .py, .jnlp, .desktop …) - wird
//     abgelehnt: "Öffnen" hieße dort "Ausführen". Eine Sperrliste wäre nie
//     vollständig (Security-Review Build 60).
//   - Der lokale Name wird Windows-sicher gemacht (keine : \ < > | ? *,
//     keine reservierten Namen wie CON/NUL, keine Punkte/Leerzeichen am Ende,
//     die Windows sonst stillschweigend entfernt: "x.exe." wäre x.exe).
//   - Jede Datei liegt in einem eigenen, nur für den Nutzer zugänglichen
//     Ordner (0700, Datei 0600, exklusiv angelegt).
//   - Windows: Kennzeichnung "aus dem Internet" (Mark of the Web), damit
//     Office & Co. die geschützte Ansicht nutzen und SmartScreen warnt;
//     macOS: Quarantäne-Kennzeichnung (Gatekeeper).
//   - Beim Programmende wird der Temp-Ordner gelöscht; Reste (z. B. weil
//     ein Programm die Datei noch offen hatte) räumt der nächste Start auf.
//
// @author Kurt Ingwer
// @date   2026-10-22 00:43
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

// allowedOpenExtensions - Typen, die mit dem Standardprogramm geöffnet
// werden dürfen: reine Dokumente und Medien. Makro-fähige Office-Varianten
// (.docm, .xlsm …), MHT-Archive und alle ausführbaren Typen fehlen bewusst.
var allowedOpenExtensions = map[string]bool{
	// Bilder
	".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true, ".bmp": true,
	".webp": true, ".tif": true, ".tiff": true, ".svg": true, ".ico": true, ".heic": true,
	".heif": true, ".avif": true,
	// Text und Web
	".txt": true, ".log": true, ".md": true, ".csv": true, ".tsv": true, ".json": true,
	".xml": true, ".yaml": true, ".yml": true, ".html": true, ".htm": true, ".css": true,
	// Dokumente
	".pdf": true, ".rtf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true, ".odg": true,
	// Audio und Video
	".mp3": true, ".wav": true, ".ogg": true, ".oga": true, ".flac": true, ".m4a": true,
	".aac": true, ".opus": true, ".mp4": true, ".m4v": true, ".mkv": true, ".avi": true,
	".mov": true, ".webm": true, ".wmv": true, ".mpg": true, ".mpeg": true,
	// Archive (werden nur angezeigt, nicht entpackt-und-ausgeführt)
	".zip": true, ".7z": true, ".rar": true, ".tar": true, ".gz": true, ".tgz": true,
	".bz2": true, ".xz": true,
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
// @date   2026-10-22 00:43
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
	local := out.String()
	// Erst kürzen (Endung behalten), DANACH alle Bereinigungen - sonst
	// könnte der gekürzte Name wieder mit Punkt/Leerzeichen enden
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
	// Windows entfernt Punkte und Leerzeichen am Ende stillschweigend
	// ("x.exe." wäre x.exe) - deshalb hier selbst entfernen
	local = strings.TrimRight(local, ". ")
	if local == "" {
		local = "datei"
	}
	base := strings.ToLower(strings.TrimSuffix(local, filepath.Ext(local)))
	if windowsReservedNames[base] {
		local = "_" + local
	}
	return local
}

// blockedOpenExtension prüft, ob ein (lokaler) Name NICHT auf der
// Erlaubnisliste steht (auch: keine Endung).
//
// @param localName - Ergebnis von localOpenName
// @return string - Endung in Kleinbuchstaben (mit Punkt; "" ohne Endung)
// @return bool - true wenn gesperrt
// @date   2026-10-22 00:43
func blockedOpenExtension(localName string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(strings.TrimRight(localName, ". ")))
	return ext, !allowedOpenExtensions[ext]
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
