; ssh-easy Windows Installer
; Erstellt mit NSIS (Nullsoft Scriptable Install System)
;
; Installiert ssh-easy mit Startmenüeintrag und optionalem Desktopicon.
; Erstellt auch einen Deinstallationseintrag in der Systemsteuerung.
;
; @author Kurt Ingwer
; @date   2026-03-08

;----------------------------------------------------------------------
; Allgemeine Einstellungen
;----------------------------------------------------------------------

; Name und Dateiname des Installers
Name "ssh-easy"
OutFile "build\ssh-easy-setup-amd64.exe"

; Standard-Installationsverzeichnis
InstallDir "$PROGRAMFILES64\ssh-easy"

; Registry-Schlüssel für das Installationsverzeichnis (für Deinstallation)
InstallDirRegKey HKLM "Software\ssh-easy" "InstallDir"

; Administratorrechte benötigt (für Program Files)
RequestExecutionLevel admin

; Modernes UI verwenden
!include "MUI2.nsh"

;----------------------------------------------------------------------
; Installer-Optionen (MUI)
;----------------------------------------------------------------------

; Icon für den Installer selbst
!define MUI_ICON "assets\icon.ico"
!define MUI_UNICON "assets\icon.ico"

; Willkommens-Seite
!define MUI_WELCOMEPAGE_TITLE "ssh-easy Installation"
!define MUI_WELCOMEPAGE_TEXT "Willkommen beim Setup-Assistent für ssh-easy.$\n$\nssh-easy ist ein SSH-Verbindungsmanager mit Terminal-Oberfläche.$\nUnterstützt automatische Authentifizierung über SSH-Agent, Keys und Passwort.$\n$\nKlicken Sie auf Weiter um fortzufahren."

; Abschluss-Seite
!define MUI_FINISHPAGE_RUN "$INSTDIR\ssh-easy.exe"
!define MUI_FINISHPAGE_RUN_TEXT "ssh-easy jetzt starten"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED

; Seiten des Installers
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
Page custom DesktopIconPage DesktopIconLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Seiten des Deinstallers
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Sprachen
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"

;----------------------------------------------------------------------
; Variablen
;----------------------------------------------------------------------

; Checkbox-Status für optionales Desktop-Icon
Var DesktopIconCheckbox
Var DesktopIconState

;----------------------------------------------------------------------
; Desktop-Icon Optionsseite (Custom Page)
;----------------------------------------------------------------------

Function DesktopIconPage
  ; Eigene Seite mit Checkbox für Desktop-Verknüpfung
  nsDialogs::Create 1018
  Pop $0

  ${NSD_CreateLabel} 0 0 100% 24u "Zusätzliche Aufgaben:"
  Pop $0

  ${NSD_CreateCheckbox} 10u 30u 100% 14u "Desktop-Verknüpfung erstellen"
  Pop $DesktopIconCheckbox
  ; Standard: nicht ausgewählt (optional)
  ${NSD_SetState} $DesktopIconCheckbox ${BST_UNCHECKED}

  nsDialogs::Show
FunctionEnd

Function DesktopIconLeave
  ; Zustand der Checkbox speichern
  ${NSD_GetState} $DesktopIconCheckbox $DesktopIconState
FunctionEnd

;----------------------------------------------------------------------
; Installer - Hauptabschnitt
;----------------------------------------------------------------------

Section "ssh-easy (erforderlich)" SecMain
  SectionIn RO  ; Pflichtkomponente, kann nicht abgewählt werden

  ; Installationsverzeichnis setzen
  SetOutPath "$INSTDIR"

  ; Programmdatei kopieren (amd64)
  File "build\ssh-easy-windows-amd64.exe"
  Rename "$INSTDIR\ssh-easy-windows-amd64.exe" "$INSTDIR\ssh-easy.exe"

  ; Installationspfad in Registry speichern
  WriteRegStr HKLM "Software\ssh-easy" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\ssh-easy" "Version" "0.10.0"

  ; Deinstallationseintrag in der Systemsteuerung erstellen
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayName" "ssh-easy"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayVersion" "0.10.0"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "Publisher" "Kurt Ingwer"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayIcon" "$INSTDIR\ssh-easy.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "URLInfoAbout" "https://github.com/von-null/ssh-easy"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoRepair" 1

  ; Startmenü-Eintrag erstellen
  CreateDirectory "$SMPROGRAMS\ssh-easy"
  CreateShortcut "$SMPROGRAMS\ssh-easy\ssh-easy.lnk" \
    "$INSTDIR\ssh-easy.exe" "" \
    "$INSTDIR\ssh-easy.exe" 0 \
    SW_SHOWNORMAL "" "SSH-Verbindungsmanager"
  CreateShortcut "$SMPROGRAMS\ssh-easy\Deinstallieren.lnk" \
    "$INSTDIR\Uninstall.exe"

  ; Desktop-Icon nur wenn Checkbox aktiviert wurde
  ${If} $DesktopIconState == ${BST_CHECKED}
    CreateShortcut "$DESKTOP\ssh-easy.lnk" \
      "$INSTDIR\ssh-easy.exe" "" \
      "$INSTDIR\ssh-easy.exe" 0 \
      SW_SHOWNORMAL "" "SSH-Verbindungsmanager"
  ${EndIf}

SectionEnd

;----------------------------------------------------------------------
; Deinstaller
;----------------------------------------------------------------------

Section "Uninstall"

  ; Programm und Verzeichnis entfernen
  Delete "$INSTDIR\ssh-easy.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  ; Startmenü-Einträge entfernen
  Delete "$SMPROGRAMS\ssh-easy\ssh-easy.lnk"
  Delete "$SMPROGRAMS\ssh-easy\Deinstallieren.lnk"
  RMDir "$SMPROGRAMS\ssh-easy"

  ; Desktop-Icon entfernen (falls vorhanden)
  Delete "$DESKTOP\ssh-easy.lnk"

  ; Registry-Einträge entfernen
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy"
  DeleteRegKey HKLM "Software\ssh-easy"

SectionEnd
