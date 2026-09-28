; "运行模式" page: writes lasitan-cluster-agent.json or lasitan-cluster-master.json
; into $INSTDIR (skipped when either already exists there).
!include "nsDialogs.nsh"

Var ModeDialog
Var ModeAgent
Var ModeMaster
Var ModeSkip
Var AgentUrlLabel
Var AgentUrl
Var AgentKeyLabel
Var AgentKey
Var AgentServer
Var AgentEpLabel
Var AgentEp
Var MasterListenLabel
Var MasterListen
Var MasterPassLabel
Var MasterPass
; Chosen values, kept after the page closes.
Var Mode
Var ValUrl
Var ValKey
Var ValServer
Var ValEp
Var ValListen
Var ValPass

Function ModePageInit
  StrCpy $Mode "agent"
  StrCpy $ValUrl "http://"
  StrCpy $ValServer ${BST_UNCHECKED}
  StrCpy $ValListen ":8443"
FunctionEnd

Function ModePageShow
  ${If} ${FileExists} "$INSTDIR\${AGENT_JSON}"
  ${OrIf} ${FileExists} "$INSTDIR\${MASTER_JSON}"
    StrCpy $Mode "keep"
    Abort
  ${EndIf}
  !insertmacro MUI_HEADER_TEXT "$(ModeTitle)" "$(ModeSubtitle)"
  nsDialogs::Create 1018
  Pop $ModeDialog

  ${NSD_CreateRadioButton} 0 0 100% 12u "$(ModeAgentText)"
  Pop $ModeAgent
  ${NSD_OnClick} $ModeAgent ModePageToggle
  ${NSD_CreateRadioButton} 0 14u 100% 12u "$(ModeMasterText)"
  Pop $ModeMaster
  ${NSD_OnClick} $ModeMaster ModePageToggle
  ${NSD_CreateRadioButton} 0 28u 100% 12u "$(ModeSkipText)"
  Pop $ModeSkip
  ${NSD_OnClick} $ModeSkip ModePageToggle

  ${NSD_CreateLabel} 0 50u 30% 12u "$(AgentUrlText)"
  Pop $AgentUrlLabel
  ${NSD_CreateText} 30% 48u 70% 12u "$ValUrl"
  Pop $AgentUrl
  ${NSD_CreateLabel} 0 66u 30% 12u "$(AgentKeyText)"
  Pop $AgentKeyLabel
  ${NSD_CreateText} 30% 64u 70% 12u "$ValKey"
  Pop $AgentKey
  ${NSD_CreateCheckbox} 30% 80u 70% 12u "$(AgentServerText)"
  Pop $AgentServer
  ${NSD_SetState} $AgentServer $ValServer
  ${NSD_OnClick} $AgentServer ModePageToggle
  ${NSD_CreateLabel} 0 98u 30% 12u "$(AgentEpText)"
  Pop $AgentEpLabel
  ${NSD_CreateText} 30% 96u 70% 12u "$ValEp"
  Pop $AgentEp

  ${NSD_CreateLabel} 0 50u 30% 12u "$(MasterListenText)"
  Pop $MasterListenLabel
  ${NSD_CreateText} 30% 48u 70% 12u "$ValListen"
  Pop $MasterListen
  ${NSD_CreateLabel} 0 66u 30% 12u "$(MasterPassText)"
  Pop $MasterPassLabel
  ${NSD_CreatePassword} 30% 64u 70% 12u "$ValPass"
  Pop $MasterPass

  ${NSD_CreateLabel} 0 116u 100% 24u "$(ModeHint)"
  Pop $0

  ${Select} $Mode
  ${Case} "master"
    ${NSD_Check} $ModeMaster
  ${Case} "skip"
    ${NSD_Check} $ModeSkip
  ${CaseElse}
    ${NSD_Check} $ModeAgent
  ${EndSelect}
  Push 0
  Call ModePageToggle
  nsDialogs::Show
FunctionEnd

!macro ModeShow CTRL VISIBLE
  ShowWindow ${CTRL} ${VISIBLE}
!macroend

Function ModePageToggle
  Pop $0 ; clicked control (unused; NSD_OnClick pushes it)
  ${NSD_GetState} $ModeAgent $1
  ${NSD_GetState} $ModeMaster $2
  ${NSD_GetState} $AgentServer $5
  StrCpy $6 ${SW_HIDE}
  ${If} $1 == ${BST_CHECKED}
  ${AndIf} $5 == ${BST_CHECKED}
    StrCpy $6 ${SW_SHOW}
  ${EndIf}
  ${If} $1 == ${BST_CHECKED}
    StrCpy $3 ${SW_SHOW}
  ${Else}
    StrCpy $3 ${SW_HIDE}
  ${EndIf}
  ${If} $2 == ${BST_CHECKED}
    StrCpy $4 ${SW_SHOW}
  ${Else}
    StrCpy $4 ${SW_HIDE}
  ${EndIf}
  !insertmacro ModeShow $AgentUrlLabel $3
  !insertmacro ModeShow $AgentUrl $3
  !insertmacro ModeShow $AgentKeyLabel $3
  !insertmacro ModeShow $AgentKey $3
  !insertmacro ModeShow $AgentServer $3
  !insertmacro ModeShow $AgentEpLabel $6
  !insertmacro ModeShow $AgentEp $6
  !insertmacro ModeShow $MasterListenLabel $4
  !insertmacro ModeShow $MasterListen $4
  !insertmacro ModeShow $MasterPassLabel $4
  !insertmacro ModeShow $MasterPass $4
FunctionEnd

Function ModePageLeave
  ${NSD_GetText} $AgentUrl $ValUrl
  ${NSD_GetText} $AgentKey $ValKey
  ${NSD_GetState} $AgentServer $ValServer
  ${NSD_GetText} $AgentEp $ValEp
  ${NSD_GetText} $MasterListen $ValListen
  ${NSD_GetText} $MasterPass $ValPass
  ${NSD_GetState} $ModeAgent $1
  ${NSD_GetState} $ModeMaster $2
  ${If} $1 == ${BST_CHECKED}
    StrCpy $Mode "agent"
    ${If} $ValUrl == ""
    ${OrIf} $ValUrl == "http://"
    ${OrIf} $ValKey == ""
      MessageBox MB_ICONEXCLAMATION "$(AgentMissing)"
      Abort
    ${EndIf}
  ${ElseIf} $2 == ${BST_CHECKED}
    StrCpy $Mode "master"
    ${If} $ValPass == ""
      MessageBox MB_ICONEXCLAMATION "$(MasterMissing)"
      Abort
    ${EndIf}
    ${If} $ValListen == ""
      StrCpy $ValListen ":8443"
    ${EndIf}
  ${Else}
    StrCpy $Mode "skip"
  ${EndIf}
FunctionEnd

; Called from the main section after files are in place.
Function WriteModeConfig
  ${If} $Mode != "agent"
  ${AndIf} $Mode != "master"
    Return
  ${EndIf}
  InitPluginsDir
  FileOpen $0 "$PLUGINSDIR\mode.txt" w
  FileWriteWord $0 0xFEFF
  FileWriteUTF16LE $0 "mode=$Mode$\r$\n"
  FileWriteUTF16LE $0 "masterUrl=$ValUrl$\r$\n"
  FileWriteUTF16LE $0 "key=$ValKey$\r$\n"
  ${If} $ValServer == ${BST_CHECKED}
    FileWriteUTF16LE $0 "role=server$\r$\n"
    FileWriteUTF16LE $0 "endpoint=$ValEp$\r$\n"
  ${EndIf}
  FileWriteUTF16LE $0 "listen=$ValListen$\r$\n"
  FileWriteUTF16LE $0 "adminPassword=$ValPass$\r$\n"
  FileClose $0
  nsExec::ExecToLog '${PS} "$INSTDIR\${HELPER}" -Action WriteConfig -Dir "$INSTDIR" -InputFile "$PLUGINSDIR\mode.txt"'
  Pop $0
FunctionEnd

LangString ModeTitle ${LANG_SIMPCHINESE} "运行模式"
LangString ModeTitle ${LANG_ENGLISH} "Mode"
LangString ModeSubtitle ${LANG_SIMPCHINESE} "配置文件会写入安装目录，双击 lasitan-cluster.exe 即按该配置启动。"
LangString ModeSubtitle ${LANG_ENGLISH} "The config is written to the install folder; double-click lasitan-cluster.exe to start with it."
LangString ModeAgentText ${LANG_SIMPCHINESE} "Agent（隧道节点，连接到 Master）"
LangString ModeAgentText ${LANG_ENGLISH} "Agent (tunnel node joining a Master)"
LangString ModeMasterText ${LANG_SIMPCHINESE} "Master（控制面板）"
LangString ModeMasterText ${LANG_ENGLISH} "Master (control panel)"
LangString ModeSkipText ${LANG_SIMPCHINESE} "暂不配置（稍后参考目录中的 .example 文件手动创建）"
LangString ModeSkipText ${LANG_ENGLISH} "Configure later (see the .example files in the install folder)"
LangString AgentUrlText ${LANG_SIMPCHINESE} "Master 地址"
LangString AgentUrlText ${LANG_ENGLISH} "Master URL"
LangString AgentKeyText ${LANG_SIMPCHINESE} "入网 key"
LangString AgentKeyText ${LANG_ENGLISH} "Enroll key"
LangString AgentServerText ${LANG_SIMPCHINESE} "作为服务端（server：所有客户端节点都会连到本机）"
LangString AgentServerText ${LANG_ENGLISH} "Act as server (every client node links to this machine)"
LangString AgentEpText ${LANG_SIMPCHINESE} "公网地址"
LangString AgentEpText ${LANG_ENGLISH} "Public endpoint"
LangString MasterListenText ${LANG_SIMPCHINESE} "监听地址"
LangString MasterListenText ${LANG_ENGLISH} "Listen address"
LangString MasterPassText ${LANG_SIMPCHINESE} "管理员密码"
LangString MasterPassText ${LANG_ENGLISH} "Admin password"
LangString ModeHint ${LANG_SIMPCHINESE} "入网 key 在 Master 面板中查看。服务端公网地址填 host:端口（默认端口 25590），留空则由 Master 按本机出口 IP 自动填写。"
LangString ModeHint ${LANG_ENGLISH} "The enroll key is shown in the Master panel. Server endpoint is host:port (default port 25590); leave empty to let Master use this machine's public IP."
LangString AgentMissing ${LANG_SIMPCHINESE} "请填写 Master 地址和入网 key。"
LangString AgentMissing ${LANG_ENGLISH} "Enter the Master URL and enroll key."
LangString MasterMissing ${LANG_SIMPCHINESE} "请设置管理员密码。"
LangString MasterMissing ${LANG_ENGLISH} "Set an admin password."
