; lasitan-cluster Windows installer (NSIS 3.05+, builds on Linux with makensis).
; Build: bash deploy/scripts/build-windows-installer.sh
; Required defines: VERSION, VI_VERSION (x.x.x.x), EXE_AMD64, EXE_ARM64, OUTFILE

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!macro RequireDefine NAME
  !ifndef ${NAME}
    !error "missing define: ${NAME}"
  !endif
!macroend
!insertmacro RequireDefine VERSION
!insertmacro RequireDefine VI_VERSION
!insertmacro RequireDefine EXE_AMD64
!insertmacro RequireDefine EXE_ARM64
!insertmacro RequireDefine OUTFILE

!define APP_NAME "Lasitan-Cluster"
!define APP_DIR "lasitan-cluster"
!define APP_EXE "lasitan-cluster.exe"
!define AGENT_JSON "lasitan-cluster-agent.json"
!define MASTER_JSON "lasitan-cluster-master.json"
!define HELPER "setup-helper.ps1"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\lasitan-cluster"
!define PS 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File'

!include "MUI2.nsh"
!include "x64.nsh"
!include "WinVer.nsh"
!include "LogicLib.nsh"
!include "Sections.nsh"

Name "${APP_NAME} ${VERSION}"
OutFile "${OUTFILE}"
RequestExecutionLevel admin
InstallDir "$PROGRAMFILES64\${APP_DIR}"
InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
BrandingText "${APP_NAME} ${VERSION}"

VIProductVersion "${VI_VERSION}"
VIAddVersionKey "ProductName" "${APP_NAME}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "${APP_NAME} installer"
VIAddVersionKey "LegalCopyright" "MIT"

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!define MUI_FINISHPAGE_RUN_TEXT "$(RunNow)"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_TEXT "$(OpenDir)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenInstDir

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
Page custom ModePageShow ModePageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

!include "mode-page.nsh"

Function .onInit
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_ICONSTOP "Windows 10 or later is required."
    Abort
  ${EndIf}
  ${IfNot} ${IsNativeAMD64}
  ${AndIfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "Only x64 and ARM64 Windows are supported."
    Abort
  ${EndIf}
  SetRegView 64
  Call ModePageInit
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

Function OpenInstDir
  ExecShell "open" "$INSTDIR"
FunctionEnd

Section "!${APP_NAME}" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "/oname=${HELPER}" "${HELPER}"
  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action PreInstall -Dir "$INSTDIR"'
  Pop $0

  ${If} ${IsNativeARM64}
    File "/oname=${APP_EXE}" "${EXE_ARM64}"
  ${Else}
    File "/oname=${APP_EXE}" "${EXE_AMD64}"
  ${EndIf}
  File "..\examples\${AGENT_JSON}.example"
  File "..\examples\${MASTER_JSON}.example"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  Call WriteModeConfig

  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action PostInstall -Dir "$INSTDIR"'
  Pop $0

  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${APP_NAME}"
  WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${APP_EXE}"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "$(AutostartSection)" SecAutostart
  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action Autostart -Dir "$INSTDIR"'
  Pop $0
SectionEnd

Section "$(PathSection)" SecPath
  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action AddPath -Dir "$INSTDIR"'
  Pop $0
SectionEnd

Section "$(ShortcutSection)" SecShortcut
  SetShellVarContext all
  CreateShortCut "$SMPROGRAMS\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
SectionEnd

; Configs (*.json) and master-data are left in place so a reinstall keeps them.
Section "Uninstall"
  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action Uninstall -Dir "$INSTDIR"'
  Pop $0
  SetShellVarContext all
  Delete "$SMPROGRAMS\${APP_NAME}.lnk"
  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\${APP_EXE}.old"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\${AGENT_JSON}.example"
  Delete "$INSTDIR\${MASTER_JSON}.example"
  Delete "$INSTDIR\${HELPER}"
  Delete "$INSTDIR\.role"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "${UNINST_KEY}"
SectionEnd

LangString AutostartSection ${LANG_SIMPCHINESE} "开机自启（注册为系统服务）"
LangString AutostartSection ${LANG_ENGLISH} "Start at boot (Windows service)"
LangString DescAutostart ${LANG_SIMPCHINESE} "按所选模式注册自动启动的系统服务（lasitan-cluster-lc0 / lasitan-cluster-master），开机无需登录即运行，并立即启动。未选择模式时跳过。"
LangString DescAutostart ${LANG_ENGLISH} "Register an automatic Windows service for the chosen mode (lasitan-cluster-lc0 / lasitan-cluster-master); it runs at boot without logon and starts now. Skipped when no mode is chosen."
LangString PathSection ${LANG_SIMPCHINESE} "添加到系统 PATH"
LangString PathSection ${LANG_ENGLISH} "Add to system PATH"
LangString ShortcutSection ${LANG_SIMPCHINESE} "开始菜单快捷方式"
LangString ShortcutSection ${LANG_ENGLISH} "Start menu shortcut"
LangString RunNow ${LANG_SIMPCHINESE} "立即启动 lasitan-cluster"
LangString RunNow ${LANG_ENGLISH} "Start lasitan-cluster now"
LangString OpenDir ${LANG_SIMPCHINESE} "打开安装目录"
LangString OpenDir ${LANG_ENGLISH} "Open the install folder"
LangString DescMain ${LANG_SIMPCHINESE} "lasitan-cluster 主程序（按本机 CPU 自动选择 x64 / ARM64）。已有服务会先停止，安装后重新启动。"
LangString DescMain ${LANG_ENGLISH} "lasitan-cluster binary (x64 / ARM64 picked for this CPU). Running services are stopped and restarted."
LangString DescPath ${LANG_SIMPCHINESE} "在任意终端中直接运行 lasitan-cluster。"
LangString DescPath ${LANG_ENGLISH} "Run lasitan-cluster from any terminal."
LangString DescShortcut ${LANG_SIMPCHINESE} "从开始菜单一键启动（等同双击 lasitan-cluster.exe）。"
LangString DescShortcut ${LANG_ENGLISH} "Start from the Start menu (same as double-clicking lasitan-cluster.exe)."

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecMain} $(DescMain)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecAutostart} $(DescAutostart)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecPath} $(DescPath)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecShortcut} $(DescShortcut)
!insertmacro MUI_FUNCTION_DESCRIPTION_END
