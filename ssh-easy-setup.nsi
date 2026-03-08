; ssh-easy Windows Installer
; Erstellt mit NSIS (Nullsoft Scriptable Install System)
;
; Installiert ssh-easy mit Startmenüeintrag und optionalem Desktopicon.
; Erkennt automatisch eine bestehende Installation und deinstalliert
; diese lautlos bevor die neue Version installiert wird.
;
; @author Kurt Ingwer
; @date   2026-03-08

;----------------------------------------------------------------------
; Versionsdefinition – hier bei jedem Build anpassen
;----------------------------------------------------------------------
!define PRODUCT_VERSION "0.12.0"
!define PRODUCT_BUILD   "12"

;----------------------------------------------------------------------
; Allgemeine Einstellungen
;----------------------------------------------------------------------

Name "ssh-easy"
OutFile "build\ssh-easy-setup-amd64.exe"

; Standard-Installationsverzeichnis (aus Registry falls bereits installiert)
InstallDir "$PROGRAMFILES64\ssh-easy"
InstallDirRegKey HKLM "Software\ssh-easy" "InstallDir"

; Administratorrechte benötigt (für Program Files + Registry HKLM)
RequestExecutionLevel admin

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

;----------------------------------------------------------------------
; Installer-Optionen (MUI)
;----------------------------------------------------------------------

!define MUI_ICON "assets\icon.ico"
!define MUI_UNICON "assets\icon.ico"

!define MUI_WELCOMEPAGE_TITLE "ssh-easy Installation"
!define MUI_WELCOMEPAGE_TEXT "Willkommen beim Setup-Assistent für ssh-easy.$\n$\nssh-easy ist ein SSH-Verbindungsmanager mit Terminal-Oberfläche.$\nUnterstützt automatische Authentifizierung über SSH-Agent, Keys und Passwort.$\n$\nEine eventuell vorhandene ältere Version wird automatisch ersetzt.$\n$\nKlicken Sie auf Weiter um fortzufahren."

!define MUI_FINISHPAGE_RUN "$INSTDIR\ssh-easy.exe"
!define MUI_FINISHPAGE_RUN_TEXT "ssh-easy jetzt starten"

; Installer-Seiten
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
Page custom DesktopIconPage DesktopIconLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Deinstaller-Seiten
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Sprachen (Deutsch zuerst = Standard)
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"

;----------------------------------------------------------------------
; Variablen
;----------------------------------------------------------------------

Var DesktopIconCheckbox
Var DesktopIconState

;----------------------------------------------------------------------
; Desktop-Icon Optionsseite
;----------------------------------------------------------------------

Function DesktopIconPage
  nsDialogs::Create 1018
  Pop $0

  ${NSD_CreateLabel} 0 0 100% 24u "Zusätzliche Aufgaben:"
  Pop $0

  ${NSD_CreateCheckbox} 10u 30u 100% 14u "Desktop-Verknüpfung erstellen"
  Pop $DesktopIconCheckbox
  ${NSD_SetState} $DesktopIconCheckbox ${BST_UNCHECKED}

  nsDialogs::Show
FunctionEnd

Function DesktopIconLeave
  ${NSD_GetState} $DesktopIconCheckbox $DesktopIconState
FunctionEnd

;----------------------------------------------------------------------
; Vorversion erkennen und lautlos deinstallieren
;----------------------------------------------------------------------

Function .onInit
  ; Prüfen ob bereits eine Version installiert ist
  ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" "UninstallString"
  ${If} $0 != ""
    ; Vorhandenen Deinstaller lautlos ausführen (/S = silent)
    ; _? verhindert dass NSIS den Deinstaller in ein Temp-Verzeichnis kopiert,
    ; damit die Pfade korrekt bleiben
    ExecWait '"$0" /S _?=$INSTDIR'
    ; Kurz warten damit der Deinstaller abgeschlossen ist
    Sleep 500
  ${EndIf}
FunctionEnd

;----------------------------------------------------------------------
; Installer – Hauptabschnitt
;----------------------------------------------------------------------

Section "ssh-easy (erforderlich)" SecMain
  SectionIn RO

  SetOutPath "$INSTDIR"

  ; Programmdatei installieren
  File "build\ssh-easy-windows-amd64.exe"
  Rename "$INSTDIR\ssh-easy-windows-amd64.exe" "$INSTDIR\ssh-easy.exe"

  ; Installationspfad und Version in Registry speichern
  WriteRegStr HKLM "Software\ssh-easy" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\ssh-easy" "Version" "${PRODUCT_VERSION}"

  ; Deinstallationseintrag in der Systemsteuerung (Apps & Features)
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayName" "ssh-easy"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayVersion" "${PRODUCT_VERSION} (Build ${PRODUCT_BUILD})"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "Publisher" "Kurt Ingwer"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "QuietUninstallString" "$\"$INSTDIR\Uninstall.exe$\" /S"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "DisplayIcon" "$INSTDIR\ssh-easy.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "URLInfoAbout" "https://github.com/von-null/ssh-easy"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoRepair" 1

  ; Startmenü-Eintrag
  CreateDirectory "$SMPROGRAMS\ssh-easy"
  CreateShortcut "$SMPROGRAMS\ssh-easy\ssh-easy.lnk" \
    "$INSTDIR\ssh-easy.exe" "" \
    "$INSTDIR\ssh-easy.exe" 0 \
    SW_SHOWNORMAL "" "SSH-Verbindungsmanager"
  CreateShortcut "$SMPROGRAMS\ssh-easy\Deinstallieren.lnk" \
    "$INSTDIR\Uninstall.exe"

  ; Optionales Desktop-Icon
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

; Lautlose Deinstallation unterstützen (/S Flag)
Function un.onInit
  ${GetParameters} $0
  ${If} $0 == "/S"
    SetSilent silent
  ${EndIf}
FunctionEnd

Section "Uninstall"

  Delete "$INSTDIR\ssh-easy.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\ssh-easy\ssh-easy.lnk"
  Delete "$SMPROGRAMS\ssh-easy\Deinstallieren.lnk"
  RMDir "$SMPROGRAMS\ssh-easy"

  Delete "$DESKTOP\ssh-easy.lnk"

  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy"
  DeleteRegKey HKLM "Software\ssh-easy"

SectionEnd
