; OpenPlayer installer: per-user (no admin rights), no bundled extras.
; Built by scripts/build-installer.sh, which stages every input and passes:
;   -DVERSION  -DSTAGE=<dir with openplayer.exe, enginectl.exe, engine\, config\, licenses\>
;   -DASSETS=<dir with openplayer.ico, side.bmp, header.bmp>
Unicode true
!include "MUI2.nsh"
!include "x64.nsh"
!include "LogicLib.nsh"
!include "Sections.nsh"

!ifndef VERSION
  !define VERSION "0.3.0"
!endif
!ifndef STAGE
  !error "pass -DSTAGE=<staging dir>"
!endif
!ifndef ASSETS
  !error "pass -DASSETS=<assets dir>"
!endif

Name "OpenPlayer"
OutFile "OpenPlayer-Setup-${VERSION}.exe"
InstallDir "$LOCALAPPDATA\Programs\OpenPlayer"
InstallDirRegKey HKCU "Software\OpenPlayer" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "OpenPlayer ${VERSION}"
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "OpenPlayer"
VIAddVersionKey "FileDescription" "OpenPlayer installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Open source"

!define MUI_ICON "${ASSETS}\openplayer.ico"
!define MUI_UNICON "${ASSETS}\openplayer.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "${ASSETS}\side.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "${ASSETS}\side.bmp"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_BITMAP "${ASSETS}\header.bmp"
!define MUI_HEADERIMAGE_UNBITMAP "${ASSETS}\header.bmp"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_SMALLDESC
!define MUI_WELCOMEPAGE_TITLE "Welcome to OpenPlayer"
!define MUI_WELCOMEPAGE_TEXT "OpenPlayer plays almost any audio and video file, with subtitles, on-screen controls and a full menu.$\r$\n$\r$\nNo ads, no tracking, no bundled software.$\r$\n$\r$\nClick Next to choose what to install."
!define MUI_FINISHPAGE_TITLE "OpenPlayer is ready"
!define MUI_FINISHPAGE_RUN "$INSTDIR\openplayer.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start OpenPlayer"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Make OpenPlayer the default player (opens Windows Settings)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenDefaultApps

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "OpenPlayer requires 64-bit Windows 10 or 11."
    Abort
  ${EndIf}
FunctionEnd

; ---- registry helpers ---------------------------------------------------------
!macro RegExt EXT
  WriteRegStr HKCU "Software\Classes\.${EXT}\OpenWithProgids" "OpenPlayer.Media" ""
  WriteRegStr HKCU "Software\OpenPlayer\Capabilities\FileAssociations" ".${EXT}" "OpenPlayer.Media"
!macroend
!macro UnRegExt EXT
  DeleteRegValue HKCU "Software\Classes\.${EXT}\OpenWithProgids" "OpenPlayer.Media"
!macroend
!macro CtxExt EXT
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Play" "" "Play with OpenPlayer"
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Play" "Icon" "$INSTDIR\openplayer.exe,0"
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Play\command" "" '"$INSTDIR\openplayer.exe" "%1"'
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Enqueue" "" "Add to OpenPlayer playlist"
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Enqueue" "Icon" "$INSTDIR\openplayer.exe,0"
  WriteRegStr HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Enqueue\command" "" '"$INSTDIR\openplayer.exe" --enqueue "%1"'
!macroend
!macro UnCtxExt EXT
  DeleteRegKey HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Play"
  DeleteRegKey HKCU "Software\Classes\SystemFileAssociations\.${EXT}\shell\OpenPlayer.Enqueue"
!macroend

; ---- components -------------------------------------------------------------------
Section "OpenPlayer (required)" SecCore
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${STAGE}\openplayer.exe"
  File "${STAGE}\enginectl.exe"
  SetOutPath "$INSTDIR\engine"
  File /r "${STAGE}\engine\*.*"
  SetOutPath "$INSTDIR\config"
  File /r "${STAGE}\config\*.*"
  SetOutPath "$INSTDIR\licenses"
  File /r "${STAGE}\licenses\*.*"
  SetOutPath "$INSTDIR"

  ; Settings file; the auto-update component below switches updates on.
  FileOpen $0 "$INSTDIR\config\openplayer.conf" w
  FileWrite $0 "# OpenPlayer settings$\r$\nauto_update=no$\r$\n"
  FileClose $0

  ; Basic program registration (needed for "Open with")
  WriteRegStr HKCU "Software\Classes\OpenPlayer.Media" "" "Media file"
  WriteRegStr HKCU "Software\Classes\OpenPlayer.Media\DefaultIcon" "" "$INSTDIR\openplayer.exe,0"
  WriteRegStr HKCU "Software\Classes\OpenPlayer.Media\shell\open\command" "" '"$INSTDIR\openplayer.exe" "%1"'
  WriteRegStr HKCU "Software\Classes\Applications\openplayer.exe\shell\open\command" "" '"$INSTDIR\openplayer.exe" "%1"'

  WriteRegStr HKCU "Software\OpenPlayer" "InstallDir" "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "DisplayName" "OpenPlayer"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "DisplayIcon" "$INSTDIR\openplayer.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer" "NoRepair" 1
SectionEnd

Section "Start menu shortcut" SecStart
  CreateShortCut "$SMPROGRAMS\OpenPlayer.lnk" "$INSTDIR\openplayer.exe"
SectionEnd

Section "Desktop shortcut" SecDesktop
  CreateShortCut "$DESKTOP\OpenPlayer.lnk" "$INSTDIR\openplayer.exe"
SectionEnd

Section "Explorer right-click menu" SecContext
!include "ext-context.nsh"
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Play" "" "Play folder with OpenPlayer"
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Play" "Icon" "$INSTDIR\openplayer.exe,0"
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Play\command" "" '"$INSTDIR\openplayer.exe" "%1"'
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Enqueue" "" "Add folder to OpenPlayer playlist"
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Enqueue" "Icon" "$INSTDIR\openplayer.exe,0"
  WriteRegStr HKCU "Software\Classes\Directory\shell\OpenPlayer.Enqueue\command" "" '"$INSTDIR\openplayer.exe" --enqueue "%1"'
SectionEnd

Section "Associate with media formats (65 file types)" SecAssoc
  WriteRegStr HKCU "Software\OpenPlayer\Capabilities" "ApplicationName" "OpenPlayer"
  WriteRegStr HKCU "Software\OpenPlayer\Capabilities" "ApplicationDescription" "Fast, clean, open-source media player"
  WriteRegStr HKCU "Software\RegisteredApplications" "OpenPlayer" "Software\OpenPlayer\Capabilities"
!include "ext-install.nsh"
SectionEnd

Section "Automatic component updates" SecUpdate
  FileOpen $0 "$INSTDIR\config\openplayer.conf" w
  FileWrite $0 "# OpenPlayer settings$\r$\nauto_update=yes$\r$\n"
  FileClose $0
SectionEnd

Section "-Refresh Explorer"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} "The player, its playback engine (libmpv with FFmpeg) and the on-screen controls."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecStart} "Add OpenPlayer to the Start menu."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "Put an OpenPlayer icon on the desktop."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecContext} "Add 'Play with OpenPlayer' and 'Add to OpenPlayer playlist' when you right-click media files and folders. On Windows 11 they are under 'Show more options'."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecAssoc} "Register OpenPlayer for 65 audio and video file types so it can be chosen as the default player."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUpdate} "Check once a day for newer playback engine and controls, verify their signature, and install them the next time OpenPlayer starts. You can change this later in Tools."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; Windows does not let installers silently take over default apps. This opens
; Windows' own page for OpenPlayer, where one click per type makes it default.
Function OpenDefaultApps
  ${If} ${SectionIsSelected} ${SecAssoc}
    ExecShell "open" "ms-settings:defaultapps?registeredAppUser=OpenPlayer"
  ${EndIf}
FunctionEnd

Section "Uninstall"
  ; Remove only what we own; never a recursive delete of the install folder itself.
  Delete "$INSTDIR\openplayer.exe"
  Delete "$INSTDIR\enginectl.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir /r "$INSTDIR\engine"
  RMDir /r "$INSTDIR\engine.prev"
  RMDir /r "$INSTDIR\engine.staging"
  RMDir /r "$INSTDIR\engine.new"
  RMDir /r "$INSTDIR\config"
  RMDir /r "$INSTDIR\licenses"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\OpenPlayer.lnk"
  Delete "$DESKTOP\OpenPlayer.lnk"
  DeleteRegKey HKCU "Software\Classes\OpenPlayer.Media"
  DeleteRegKey HKCU "Software\Classes\Applications\openplayer.exe"
  DeleteRegKey HKCU "Software\Classes\Directory\shell\OpenPlayer.Play"
  DeleteRegKey HKCU "Software\Classes\Directory\shell\OpenPlayer.Enqueue"
  DeleteRegValue HKCU "Software\RegisteredApplications" "OpenPlayer"
  DeleteRegKey HKCU "Software\OpenPlayer"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPlayer"
!include "ext-uninstall.nsh"
!include "ext-context-uninstall.nsh"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd
