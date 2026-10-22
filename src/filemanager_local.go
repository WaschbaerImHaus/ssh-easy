// Paket main - Datei-Manager (MC-Modus): "Lokaler PC" als Verbindung
//
// localFS erfüllt dasselbe Interface wie die SFTP-Verbindung (remoteFS),
// arbeitet aber auf dem Dateisystem dieses Rechners. So funktionieren
// Kopieren, Verschieben, Anlegen, Löschen, Ansehen und Bearbeiten zwischen
// PC und Server ohne eigene Sonderwege - mit denselben Schutzmaßnahmen
// (exklusive Temp-Datei, Umbenennen, Namensprüfung).
//
// Pfade im Fenster sind immer POSIX-Pfade ("/" als Trenner). Unter Linux
// und macOS sind sie unverändert echte Pfade. Unter Windows gilt:
//   - "/" ist die Laufwerksliste (C:, D:, …); dort lässt sich nichts anlegen
//   - "/C:/Users/x" entspricht C:\Users\x
//
// Windows erlaubt manche Namen nicht oder legt sie anders an (CON, NUL,
// "a:b" = alternativer Datenstrom, Punkt/Leerzeichen am Ende werden
// entfernt). Solche Namen lehnt ValidName ab; das Kopieren prüft das für
// das Ziel (nameValidator). Vom Server auf den PC kopierte Dateien werden
// als heruntergeladen gekennzeichnet (Windows: Mark of the Web, macOS:
// Quarantäne), damit Windows beim Ausführen warnt.
//
// @author Kurt Ingwer
// @date   2026-10-22 06:40
package main

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// fmLocalConnID - ID der Pseudo-Verbindung "Lokaler PC" (kann keine echte
// Verbindungs-ID sein, die sind UUIDs)
const fmLocalConnID = "\x00local-pc"

// errDriveList - in der Windows-Laufwerksliste lässt sich nichts anlegen
var errDriveList = errors.New("not possible in the drive list")

// errLocalPath - Fensterpfad lässt sich nicht in einen lokalen Pfad umrechnen
var errLocalPath = errors.New("invalid local path")

// nameValidator - Dateisysteme mit strengeren Namensregeln (lokales Windows)
type nameValidator interface {
	// ValidName meldet, ob ein Name dort unverändert angelegt werden kann
	ValidName(name string) bool
}

// downloadMarker - Dateisysteme, die heruntergeladene Dateien kennzeichnen
type downloadMarker interface {
	// MarkDownloaded kennzeichnet eine Datei als aus dem Netz stammend
	MarkDownloaded(p string) error
}

// localFS ist das Dateisystem dieses Rechners.
type localFS struct {
	// Startverzeichnis (Home) als Fensterpfad
	home string
	// Windows-Pfadregeln (Laufwerksliste, Laufwerksbuchstaben, Namen)
	windows bool
}

// newLocalFS öffnet den lokalen PC mit dem Home des Benutzers als Start.
//
// @return remoteFS - lokales Dateisystem
// @return error - Home-Verzeichnis nicht ermittelbar
// @date   2026-10-22 06:40
func newLocalFS() (remoteFS, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &localFS{home: localPanelPath(home, localIsWindows), windows: localIsWindows}, nil
}

// localNativePath rechnet einen Fensterpfad in einen lokalen Pfad um.
//
// @param p - Fensterpfad (absolut, "/" als Trenner)
// @param windows - Windows-Regeln anwenden
// @return string - lokaler Pfad ("" = Windows-Laufwerksliste)
// @return error - errLocalPath bei ungültigem Pfad
// @date   2026-10-22 06:40
func localNativePath(p string, windows bool) (string, error) {
	if !path.IsAbs(p) {
		return "", errLocalPath
	}
	p = path.Clean(p)
	if !windows {
		return p, nil
	}
	if p == "/" {
		return "", nil
	}
	rest := strings.TrimPrefix(p, "/")
	drive, tail, _ := strings.Cut(rest, "/")
	if len(drive) != 2 || drive[1] != ':' || !isASCIILetter(drive[0]) {
		return "", errLocalPath
	}
	native := strings.ToUpper(drive[:1]) + `:\`
	if tail != "" {
		native += strings.ReplaceAll(tail, "/", `\`)
	}
	return native, nil
}

// localPanelPath rechnet einen lokalen Pfad in einen Fensterpfad um.
// Unter Windows werden nur Pfade mit Laufwerksbuchstaben unterstützt;
// alles andere (z. B. Netzwerkpfade \\server\freigabe) wird zur
// Laufwerksliste.
//
// @param native - lokaler Pfad
// @param windows - Windows-Regeln anwenden
// @return string - Fensterpfad
// @date   2026-10-22 06:40
func localPanelPath(native string, windows bool) string {
	if !windows {
		return path.Clean(filepath.ToSlash(native))
	}
	s := strings.ReplaceAll(native, `\`, "/")
	if len(s) < 2 || s[1] != ':' || !isASCIILetter(s[0]) || (len(s) > 2 && s[2] != '/') {
		return "/"
	}
	return path.Clean("/" + strings.ToUpper(s[:1]) + s[1:])
}

// isASCIILetter meldet, ob b ein Buchstabe A-Z/a-z ist.
//
// @param b - Zeichen
// @return bool - true bei Buchstaben
// @date   2026-10-22 06:40
func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// validWindowsName prüft, ob Windows einen Namen unverändert anlegen kann:
// keine Zeichen <>:"/\|?*, keine Steuerzeichen, kein Punkt/Leerzeichen am
// Ende, kein Gerätename (CON, NUL, COM1 … auch mit Endung).
//
// @param name - Dateiname
// @return bool - true wenn gültig
// @date   2026-10-22 06:40
func validWindowsName(name string) bool {
	if name == "" || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 || unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToLower(name), ".")
	base = strings.TrimRight(base, " ")
	if windowsReservedNames[base] {
		return false
	}
	// Hochgestellte Ziffern zählen seit Windows 10 ebenfalls als Geräte
	for _, dev := range []string{"com", "lpt"} {
		for _, sup := range []string{"¹", "²", "³"} {
			if base == dev+sup {
				return false
			}
		}
	}
	return true
}

// native liefert den lokalen Pfad; die Laufwerksliste ist kein Pfad.
//
// @param p - Fensterpfad
// @return string - lokaler Pfad
// @return error - errLocalPath bzw. errDriveList
// @date   2026-10-22 06:40
func (l *localFS) native(p string) (string, error) {
	n, err := localNativePath(p, l.windows)
	if err == nil && n == "" {
		return "", errDriveList
	}
	return n, err
}

// isDriveList meldet, ob p die Windows-Laufwerksliste ist.
//
// @param p - Fensterpfad
// @return bool - true bei "/" unter Windows
// @date   2026-10-22 06:40
func (l *localFS) isDriveList(p string) bool {
	return l.windows && path.Clean(p) == "/"
}

// Getwd liefert das Home des Benutzers.
func (l *localFS) Getwd() (string, error) { return l.home, nil }

// ReadDir liest ein Verzeichnis, ohne Symlinks zu folgen (unter Windows
// liefert "/" die Laufwerke).
//
// @param p - Fensterpfad
// @return []os.FileInfo - Einträge (Infos wie Lstat)
// @return error - Fehler beim Lesen
// @date   2026-10-22 06:40
func (l *localFS) ReadDir(p string) ([]os.FileInfo, error) {
	if l.isDriveList(p) {
		return localDrives(), nil
	}
	n, err := l.native(p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(n)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		// Info() entspricht Lstat; verschwundene Einträge überspringen
		if info, err := e.Info(); err == nil {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// Stat liefert Infos zum Ziel eines Pfads (folgt Symlinks).
func (l *localFS) Stat(p string) (os.FileInfo, error) {
	if l.isDriveList(p) {
		return localDirInfo{name: "/"}, nil
	}
	n, err := l.native(p)
	if err != nil {
		return nil, err
	}
	return os.Stat(n)
}

// Lstat liefert Infos zum Pfad selbst (folgt Symlinks nicht).
func (l *localFS) Lstat(p string) (os.FileInfo, error) {
	if l.isDriveList(p) {
		return localDirInfo{name: "/"}, nil
	}
	n, err := l.native(p)
	if err != nil {
		return nil, err
	}
	return os.Lstat(n)
}

// ReadLink liefert das Ziel eines Symlinks ("/" als Trenner).
func (l *localFS) ReadLink(p string) (string, error) {
	n, err := l.native(p)
	if err != nil {
		return "", err
	}
	target, err := os.Readlink(n)
	return filepath.ToSlash(target), err
}

// RealPath löst alle Symlinks im Pfad auf.
func (l *localFS) RealPath(p string) (string, error) {
	if l.isDriveList(p) {
		return "/", nil
	}
	n, err := l.native(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(n)
	if err != nil {
		return "", err
	}
	return localPanelPath(real, l.windows), nil
}

// OpenRead öffnet eine Datei zum Lesen.
func (l *localFS) OpenRead(p string) (io.ReadCloser, error) {
	n, err := l.native(p)
	if err != nil {
		return nil, err
	}
	return os.Open(n)
}

// CreateExclusive legt eine NEUE Datei an (O_EXCL, nur für den Besitzer;
// ein vorhandener Pfad - auch ein Symlink - lässt es scheitern).
func (l *localFS) CreateExclusive(p string) (io.WriteCloser, error) {
	n, err := l.native(p)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(n, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// RenameOverwrite benennt um und ersetzt ein vorhandenes Ziel (Datei oder
// Symlink) in einem Schritt.
func (l *localFS) RenameOverwrite(oldPath, newPath string) error {
	from, err := l.native(oldPath)
	if err != nil {
		return err
	}
	to, err := l.native(newPath)
	if err != nil {
		return err
	}
	return os.Rename(from, to)
}

// Mkdir legt ein Verzeichnis an.
func (l *localFS) Mkdir(p string) error {
	n, err := l.native(p)
	if err != nil {
		return err
	}
	return os.Mkdir(n, 0o755)
}

// Remove löscht eine Datei, einen Symlink (nicht sein Ziel) oder ein
// leeres Verzeichnis.
func (l *localFS) Remove(p string) error {
	n, err := l.native(p)
	if err != nil {
		return err
	}
	return os.Remove(n)
}

// Symlink legt unter linkPath einen Symlink auf target an.
func (l *localFS) Symlink(target, linkPath string) error {
	n, err := l.native(linkPath)
	if err != nil {
		return err
	}
	if l.windows {
		target = filepath.FromSlash(target)
	}
	return os.Symlink(target, n)
}

// Chmod setzt die Zugriffsrechte (unter Windows nur "schreibgeschützt").
func (l *localFS) Chmod(p string, mode os.FileMode) error {
	n, err := l.native(p)
	if err != nil {
		return err
	}
	return os.Chmod(n, mode)
}

// Chtimes setzt Zugriffs- und Änderungszeit.
func (l *localFS) Chtimes(p string, atime, mtime time.Time) error {
	n, err := l.native(p)
	if err != nil {
		return err
	}
	return os.Chtimes(n, atime, mtime)
}

// Close - lokal gibt es keine Verbindung zu schließen.
func (l *localFS) Close() error { return nil }

// ValidName prüft unter Windows die Windows-Namensregeln.
//
// @param name - Dateiname
// @return bool - true wenn hier anlegbar
// @date   2026-10-22 06:40
func (l *localFS) ValidName(name string) bool {
	return !l.windows || validWindowsName(name)
}

// MarkDownloaded kennzeichnet eine vom Server kopierte Datei als
// heruntergeladen (Windows: Zone.Identifier, macOS: Quarantäne).
//
// @param p - Fensterpfad
// @return error - Kennzeichnung fehlgeschlagen
// @date   2026-10-22 07:10
func (l *localFS) MarkDownloaded(p string) error {
	n, err := l.native(p)
	if err != nil {
		return err
	}
	return markDownloaded(n)
}

// localDirInfo beschreibt einen Eintrag der Laufwerksliste.
type localDirInfo struct {
	name string
}

func (d localDirInfo) Name() string       { return d.name }
func (d localDirInfo) Size() int64        { return 0 }
func (d localDirInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (d localDirInfo) ModTime() time.Time { return time.Time{} }
func (d localDirInfo) IsDir() bool        { return true }
func (d localDirInfo) Sys() any           { return nil }

// fmShouldMarkDownloads meldet, ob kopierte Dateien als heruntergeladen
// gekennzeichnet werden: nur vom Server auf den lokalen PC.
//
// @param src - Quelle
// @param dst - Ziel
// @return bool - true wenn kennzeichnen
// @date   2026-10-22 06:40
func fmShouldMarkDownloads(src, dst remoteFS) bool {
	_, dstMarks := dst.(downloadMarker)
	return dstMarks && fmUntrustedToLocal(src, dst)
}

// fmUntrustedToLocal meldet, ob vom Server (nicht vertrauenswürdig) auf den
// lokalen PC kopiert wird - dann gelten die strengeren Regeln für den PC
// (Kennzeichnung, nur sichere Symlinks).
//
// @param src - Quelle
// @param dst - Ziel
// @return bool - true bei Server -> PC
// @date   2026-10-22 07:10
func fmUntrustedToLocal(src, dst remoteFS) bool {
	return isLocalFS(dst) && !isLocalFS(src)
}

// safeLinkTarget prüft das Ziel eines Symlinks, der vom Server auf den PC
// kopiert wird: nur relative Ziele ohne Laufwerk, Backslash, Steuerzeichen
// und ohne "..". Sonst könnte ein Server z. B. einen Link auf ~/.ssh oder
// C:\Windows unterschieben, dem spätere Aktionen auf dem PC folgen. ".."
// wird ganz abgelehnt, weil eine reine Textprüfung andere Links im
// kopierten Baum nicht kennt (sub/l1 -> ".." macht "l1/../x" zu einem Weg
// hinaus).
//
// @param rel - Pfad des Links relativ zum Zielverzeichnis (nicht mehr
// ausgewertet, bleibt für Meldungen und künftige Prüfungen)
// @param target - Linkziel vom Server
// @return bool - true wenn unbedenklich
// @date   2026-10-22 07:45
func safeLinkTarget(rel, target string) bool {
	if target == "" || path.IsAbs(target) || strings.ContainsAny(target, `\:`) {
		return false
	}
	for _, r := range target {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(target, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// isLocalFS meldet, ob ein Dateisystem der lokale PC ist (auch in
// Tests eingebettete Varianten).
//
// @param fsys - Dateisystem
// @return bool - true beim lokalen PC
// @date   2026-10-22 06:40
func isLocalFS(fsys remoteFS) bool {
	_, ok := fsys.(interface{ isDriveList(string) bool })
	return ok
}

// panelIsDriveList meldet, ob ein Dateisystem gerade die Windows-
// Laufwerksliste zeigt.
//
// @param fsys - Dateisystem des Fensters
// @param p - Pfad des Fensters
// @return bool - true in der Laufwerksliste
// @date   2026-10-22 06:40
func panelIsDriveList(fsys remoteFS, p string) bool {
	d, ok := fsys.(interface{ isDriveList(string) bool })
	return ok && d.isDriveList(p)
}
