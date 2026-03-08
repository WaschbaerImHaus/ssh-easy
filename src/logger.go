// Paket main - Logging fuer ssh-easy
//
// Einfaches dateibasiertes Logging fuer Debugging und Audit.
// Logdatei wird unter ~/.ssh-easy/ssh-easy.log gespeichert.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 21:00
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger schreibt Lognachrichten in eine Datei.
// Thread-sicher durch Mutex.
type Logger struct {
	// Mutex fuer thread-sicheres Schreiben
	mu sync.Mutex
	// Pfad zur Logdatei
	filePath string
	// Ob Logging aktiviert ist
	enabled bool
}

// NewLogger erstellt einen neuen Logger.
// Die Logdatei wird im Konfigurationsverzeichnis angelegt.
//
// @return *Logger - Neuer Logger (nil-sicher, loggt ins Leere wenn Pfad nicht ermittelbar)
// @date   2026-03-07 21:00
func NewLogger() *Logger {
	dir, err := GetConfigDir()
	if err != nil {
		return &Logger{enabled: false}
	}

	logPath := filepath.Join(dir, "ssh-easy.log")
	return &Logger{
		filePath: logPath,
		enabled:  true,
	}
}

// Info schreibt eine Info-Nachricht ins Log.
//
// @param format - Format-String (wie fmt.Sprintf)
// @param args - Format-Argumente
// @date   2026-03-07 21:00
func (l *Logger) Info(format string, args ...interface{}) {
	l.write("INFO", format, args...)
}

// Error schreibt eine Fehlernachricht ins Log.
//
// @param format - Format-String (wie fmt.Sprintf)
// @param args - Format-Argumente
// @date   2026-03-07 21:00
func (l *Logger) Error(format string, args ...interface{}) {
	l.write("ERROR", format, args...)
}

// Warn schreibt eine Warnung ins Log.
//
// @param format - Format-String (wie fmt.Sprintf)
// @param args - Format-Argumente
// @date   2026-03-07 21:00
func (l *Logger) Warn(format string, args ...interface{}) {
	l.write("WARN", format, args...)
}

// write schreibt eine Zeile in die Logdatei.
// Thread-sicher durch Mutex. Fehler beim Schreiben werden ignoriert.
//
// @param level - Log-Level (INFO, ERROR, WARN)
// @param format - Format-String
// @param args - Format-Argumente
// @date   2026-03-07 21:00
func (l *Logger) write(level, format string, args ...interface{}) {
	if l == nil || !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Logdatei oeffnen (Append-Modus)
	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	// Zeitstempel und Nachricht schreiben
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s [%s] %s\n", timestamp, level, msg)
}
