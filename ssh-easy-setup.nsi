; ssh-easy Windows Installer
; Built with NSIS (Nullsoft Scriptable Install System)
;
; Installs ssh-easy with a Start Menu entry and optional Desktop shortcut.
; Automatically detects and silently removes any previous installation.
; Supports both x64 and ARM64 binaries via /DARCH= define.
;
; Usage:
;   makensis /DARCH=amd64 /DVERSION=0.15 /DBUILD=15 ssh-easy-setup.nsi
;   makensis /DARCH=arm64 /DVERSION=0.15 /DBUILD=15 ssh-easy-setup.nsi
;
; @author Kurt Ingwer
; @date   2026-10-27 16:15

;----------------------------------------------------------------------
; Version aus den Kommandozeilen-Defines von build.sh (/DVERSION, /DBUILD).
; Bis Build 82 las das Skript PRODUCT_VERSION/PRODUCT_BUILD, build.sh
; übergab aber VERSION/BUILD - Windows zeigte dann immer "0.15.0 (Build 15)".
;----------------------------------------------------------------------
!ifdef VERSION
  !define PRODUCT_VERSION "${VERSION}"
!else
  !define PRODUCT_VERSION "0.0.0"
!endif
!ifdef BUILD
  !define PRODUCT_BUILD "${BUILD}"
!else
  !define PRODUCT_BUILD "dev"
!endif
!ifndef ARCH
  !define ARCH "amd64"
!endif

;----------------------------------------------------------------------
; Derived names
;----------------------------------------------------------------------
!if "${ARCH}" == "arm64"
  !define BINARY_FILE "build\ssh-easy-windows-arm64.exe"
  !define INSTALLER_NAME "build\ssh-easy-setup-arm64.exe"
  !define ARCH_LABEL "ARM64"
!else
  !define BINARY_FILE "build\ssh-easy-windows-amd64.exe"
  !define INSTALLER_NAME "build\ssh-easy-setup-amd64.exe"
  !define ARCH_LABEL "x64"
!endif

;----------------------------------------------------------------------
; General settings
;----------------------------------------------------------------------

Name "ssh-easy"
OutFile "${INSTALLER_NAME}"

; Default install directory (from registry if already installed)
InstallDir "$PROGRAMFILES64\ssh-easy"
InstallDirRegKey HKLM "Software\ssh-easy" "InstallDir"

; Administrator rights required (Program Files + HKLM registry)
RequestExecutionLevel admin

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

;----------------------------------------------------------------------
; MUI appearance
;----------------------------------------------------------------------

!define MUI_ICON "assets\icon.ico"
!define MUI_UNICON "assets\icon.ico"

!define MUI_WELCOMEPAGE_TITLE "Welcome to ssh-easy Setup"
!define MUI_WELCOMEPAGE_TEXT "This wizard will install ssh-easy ${PRODUCT_VERSION} (Build ${PRODUCT_BUILD}) for Windows ${ARCH_LABEL}.$\n$\nssh-easy is an SSH connection manager with a terminal UI.$\nIt supports automatic authentication via SSH agent, key files and password.$\n$\nAny existing installation will be replaced automatically.$\n$\nClick Next to continue."

!define MUI_FINISHPAGE_RUN "$INSTDIR\ssh-easy.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ssh-easy now"

; Installer pages
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
Page custom DesktopIconPage DesktopIconLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages (English first = default)
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"

;----------------------------------------------------------------------
; Variables
;----------------------------------------------------------------------

Var DesktopIconCheckbox
Var DesktopIconState

;----------------------------------------------------------------------
; Optional Desktop shortcut page
;----------------------------------------------------------------------

Function DesktopIconPage
  nsDialogs::Create 1018
  Pop $0

  ${NSD_CreateLabel} 0 0 100% 24u "Additional tasks:"
  Pop $0

  ${NSD_CreateCheckbox} 10u 30u 100% 14u "Create Desktop shortcut"
  Pop $DesktopIconCheckbox
  ; Vorbelegung: angehakt, wenn bereits eine Verknüpfung existierte (.onInit)
  ${If} $DesktopIconState == ${BST_CHECKED}
    ${NSD_SetState} $DesktopIconCheckbox ${BST_CHECKED}
  ${Else}
    ${NSD_SetState} $DesktopIconCheckbox ${BST_UNCHECKED}
  ${EndIf}

  nsDialogs::Show
FunctionEnd

Function DesktopIconLeave
  ${NSD_GetState} $DesktopIconCheckbox $DesktopIconState
FunctionEnd

;----------------------------------------------------------------------
; Desktop-Verknüpfung der Vorversion merken
;----------------------------------------------------------------------

Function .onInit
  ; Vorhandene Desktop-Verknüpfung merken, BEVOR der alte Deinstaller sie
  ; löscht. Wichtig für das stille Auto-Update (/S): Dort wird die Seite mit
  ; der Checkbox übersprungen - ohne dieses Merken ginge das Icon verloren.
  ; Den Deinstaller selbst startet erst die Section (RunOldUninstaller):
  ; Ein Abbruch vorher - Willkommensseite oder gesperrte EXE - lässt die
  ; alte Installation dann vollständig stehen.
  ${If} ${FileExists} "$DESKTOP\ssh-easy.lnk"
    StrCpy $DesktopIconState ${BST_CHECKED}
  ${EndIf}
FunctionEnd

;----------------------------------------------------------------------
; Vorversion still deinstallieren
;----------------------------------------------------------------------
; Läuft in der Section NACH RemoveOldBinary und VOR dem Entpacken: Danach
; kann das Setup nicht mehr an einer gesperrten EXE scheitern, und der
; Deinstaller kann die neue ssh-easy.exe nicht mehr löschen.
Function RunOldUninstaller
  ; $R0 = UninstallString aus der Registry (gequotet: "C:\...\Uninstall.exe")
  ReadRegStr $R0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" "UninstallString"
  ${If} $R0 == ""
    Return
  ${EndIf}
  ; Anführungszeichen entfernen: $R0 = Pfad des Deinstallers, $R1 = sein Ordner
  StrCpy $R1 $R0 1
  ${If} $R1 == '"'
    StrCpy $R0 $R0 "" 1
    StrCpy $R0 $R0 -1
  ${EndIf}
  ${GetParent} $R0 $R1
  ${IfNot} ${FileExists} $R0
    Return
  ${EndIf}
  ; WICHTIG: _?= ist Pflicht. Ohne _?= kopiert NSIS den Deinstaller in ein
  ; Temp-Verzeichnis und beendet den ersten Prozess sofort - ExecWait
  ; wartet dann nicht, und die Kopie konnte die gerade installierten
  ; Dateien wieder löschen. Mit _?= läuft er an Ort und Stelle und wir
  ; warten wirklich auf sein Ende. Er löscht sich dann nicht selbst.
  ExecWait '"$R0" /S _?=$R1'
  Delete $R0
  ; Ordner nur entfernen, wenn er leer ist (abweichender alter Pfad)
  RMDir $R1
FunctionEnd

;----------------------------------------------------------------------
; Alte ssh-easy.exe entfernen - auch wenn sie noch läuft
;----------------------------------------------------------------------
; Bis Build 82 entpackte das Setup die neue Binary unter ihrem Build-Namen
; und benannte sie danach um. Lief die alte ssh-easy.exe noch, scheiterten
; Löschen und Umbenennen still: Das Setup meldete Erfolg, die alte Version
; blieb. Jetzt wird die alte EXE gelöscht oder - wenn sie läuft - beiseite
; umbenannt (Windows erlaubt das bei laufenden EXEs). Gelingt beides nicht,
; wird gewartet und gefragt; am Ende bricht das Setup mit Fehlercode ab.
Function RemoveOldBinary
  StrCpy $R2 0
  retry:
    ${IfNot} ${FileExists} "$INSTDIR\ssh-easy.exe"
      Return
    ${EndIf}
    Delete "$INSTDIR\ssh-easy.exe"
    ${IfNot} ${FileExists} "$INSTDIR\ssh-easy.exe"
      Return
    ${EndIf}
    ; Laufende EXE: Löschen verboten, Umbenennen erlaubt. Freien Namen
    ; suchen: ssh-easy.exe.old, .old1, .old2 ... - eine früher beiseite
    ; gelegte EXE kann selbst noch laufen (mehrere Fenster offen)
    StrCpy $R3 ""
    findname:
      StrCpy $R4 "$INSTDIR\ssh-easy.exe.old$R3"
      Delete $R4
      ${If} ${FileExists} $R4
        IntOp $R3 $R3 + 1
        ${If} $R3 < 20
          Goto findname
        ${EndIf}
      ${EndIf}
    Rename "$INSTDIR\ssh-easy.exe" $R4
    ${IfNot} ${FileExists} "$INSTDIR\ssh-easy.exe"
      ; Löschen beim nächsten Neustart vormerken, ohne nach einem Neustart
      ; zu fragen (das nächste Setup räumt die Datei ebenfalls weg)
      Delete /REBOOTOK $R4
      SetRebootFlag false
      Return
    ${EndIf}
    ; Auto-Update (still): ssh-easy beendet sich gerade - bis zu 30 s warten.
    ; Mit Oberfläche: 3 s warten, dann fragen.
    IntOp $R2 $R2 + 1
    ${If} ${Silent}
      ${If} $R2 < 30
        Sleep 1000
        Goto retry
      ${EndIf}
    ${ElseIf} $R2 < 3
      Sleep 1000
      Goto retry
    ${EndIf}
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "ssh-easy.exe is still in use and cannot be replaced.$\n$\nPlease close all ssh-easy windows and click Retry." /SD IDCANCEL IDRETRY retry
    SetErrorLevel 2
    Abort "ssh-easy.exe is still in use - installation cancelled."
FunctionEnd

;----------------------------------------------------------------------
; Main install section
;----------------------------------------------------------------------

Section "ssh-easy (required)" SecMain
  SectionIn RO

  SetOutPath "$INSTDIR"

  ; Reste früherer Installationen: beiseite gelegte alte EXE und die unter
  ; dem Build-Namen liegen gebliebene Binary gescheiterter Setups (bis Build 82)
  Delete "$INSTDIR\ssh-easy.exe.old*"
  Delete "$INSTDIR\ssh-easy-windows-amd64.exe"
  Delete "$INSTDIR\ssh-easy-windows-arm64.exe"

  ; Alte Programmdatei entfernen (auch wenn sie noch läuft, siehe oben).
  ; Bricht das Setup hier ab, ist an der alten Installation nichts verändert.
  Call RemoveOldBinary

  ; Erst jetzt die Vorversion deinstallieren. Ihr Deinstaller kann den
  ; Programmordner leer vorfinden und entfernen - daher danach neu anlegen.
  Call RunOldUninstaller
  SetOutPath "$INSTDIR"

  ; Binärdatei direkt unter dem endgültigen Namen entpacken - kein Rename,
  ; das bei vorhandener ssh-easy.exe still scheitern würde
  ClearErrors
  File "/oname=$INSTDIR\ssh-easy.exe" "${BINARY_FILE}"
  ${If} ${Errors}
    SetErrorLevel 2
    Abort "ssh-easy.exe could not be written - installation cancelled."
  ${EndIf}

  ; Store install path and version in registry
  WriteRegStr HKLM "Software\ssh-easy" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\ssh-easy" "Version" "${PRODUCT_VERSION}"

  ; Add/Programs entry for Windows Apps & Features
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
    "URLInfoAbout" "https://github.com/WaschbaerImHaus/ssh-easy"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy" \
    "NoRepair" 1

  ; Start Menu entry
  ; Hinweis: Die AppUserModelID "KurtIngwer.ssh-easy" wird vom Programm selbst
  ; zur Laufzeit per SetCurrentProcessExplicitAppUserModelID gesetzt, damit
  ; Windows den Taskleisteneintrag korrekt der App (und nicht dem Console-Host)
  ; zuordnet.
  CreateDirectory "$SMPROGRAMS\ssh-easy"
  CreateShortcut "$SMPROGRAMS\ssh-easy\ssh-easy.lnk" \
    "$INSTDIR\ssh-easy.exe" "" \
    "$INSTDIR\ssh-easy.exe" 0 \
    SW_SHOWNORMAL "" "SSH Connection Manager"
  CreateShortcut "$SMPROGRAMS\ssh-easy\Uninstall ssh-easy.lnk" \
    "$INSTDIR\Uninstall.exe"

  ; Optional Desktop shortcut
  ${If} $DesktopIconState == ${BST_CHECKED}
    CreateShortcut "$DESKTOP\ssh-easy.lnk" \
      "$INSTDIR\ssh-easy.exe" "" \
      "$INSTDIR\ssh-easy.exe" 0 \
      SW_SHOWNORMAL "" "SSH Connection Manager"
  ${EndIf}

SectionEnd

;----------------------------------------------------------------------
; Uninstaller
;----------------------------------------------------------------------

; Support silent uninstall via /S flag
Function un.onInit
  ${GetParameters} $0
  ${If} $0 == "/S"
    SetSilent silent
  ${EndIf}
FunctionEnd

Section "Uninstall"

  Delete "$INSTDIR\ssh-easy.exe"
  Delete "$INSTDIR\ssh-easy.exe.old*"
  Delete "$INSTDIR\ssh-easy-windows-amd64.exe"
  Delete "$INSTDIR\ssh-easy-windows-arm64.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\ssh-easy\ssh-easy.lnk"
  Delete "$SMPROGRAMS\ssh-easy\Uninstall ssh-easy.lnk"
  RMDir "$SMPROGRAMS\ssh-easy"

  Delete "$DESKTOP\ssh-easy.lnk"

  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\ssh-easy"
  DeleteRegKey HKLM "Software\ssh-easy"

SectionEnd
