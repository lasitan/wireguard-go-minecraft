export type CamoProfile =
  | "none"
  | "minecraft"
  | "source"
  | "terraria"
  | "steam"
  | "bedrock"
  | "fivem";

export type TransportCamouflage = {
  profile: CamoProfile;
  deep: boolean;
  handshakeTimeout: string;
  loginUsername: string;
  loginPluginChannel: string;
  loginPluginSecret: string;
  rejectMessage: string;
  serverName: string;
};

export type TransportView = {
  camouflage: TransportCamouflage;
};

export const CAMO_PROFILES: { id: CamoProfile; label: string; hint: string }[] = [
  { id: "none", label: "关闭伪装", hint: "纯 TCP 长度前缀，无游戏握手" },
  { id: "minecraft", label: "Minecraft Java", hint: "状态查询 + 登录插件（深度伪装）" },
  { id: "bedrock", label: "Minecraft Bedrock", hint: "RakNet 风格离线 Ping/Pong" },
  { id: "source", label: "Source / CS2", hint: "Source Engine A2S 查询" },
  { id: "terraria", label: "Terraria", hint: "连接包 + 断开提示" },
  { id: "steam", label: "Steam（Rust / Valheim 等）", hint: "Steam 挑战/应答" },
  { id: "fivem", label: "FiveM / GTA", hint: "HTTP info.json 探针" },
];

const defaultCamo = (): TransportCamouflage => ({
  profile: "minecraft",
  deep: true,
  handshakeTimeout: "10s",
  loginUsername: "Steve",
  loginPluginChannel: "minecraft:register",
  loginPluginSecret: "change-me-shared-secret",
  rejectMessage: "You are not whitelisted on this server!",
  serverName: "Dedicated Server",
});

export function parseTransport(raw: unknown): TransportView {
  const camo = defaultCamo();
  if (!raw || typeof raw !== "object") return { camouflage: camo };
  const root = raw as Record<string, unknown>;
  const c = (root.camouflage as Record<string, unknown>) || {};
  const mc = (root.mc as Record<string, unknown>) || {};
  if (typeof c.profile === "string") camo.profile = c.profile as CamoProfile;
  else if (mc.enabled === false) camo.profile = "none";
  if (typeof c.deep === "boolean") camo.deep = c.deep;
  else if (typeof mc.deepCamouflage === "boolean") camo.deep = mc.deepCamouflage;
  if (typeof c.handshakeTimeout === "string") camo.handshakeTimeout = c.handshakeTimeout;
  else if (typeof mc.handshakeTimeout === "string") camo.handshakeTimeout = mc.handshakeTimeout;
  if (typeof c.loginUsername === "string") camo.loginUsername = c.loginUsername;
  else if (typeof mc.loginUsername === "string") camo.loginUsername = mc.loginUsername;
  if (typeof c.loginPluginChannel === "string") camo.loginPluginChannel = c.loginPluginChannel;
  else if (typeof mc.loginPluginChannel === "string") camo.loginPluginChannel = mc.loginPluginChannel;
  if (typeof c.loginPluginSecret === "string") camo.loginPluginSecret = c.loginPluginSecret;
  else if (typeof mc.loginPluginSecret === "string") camo.loginPluginSecret = mc.loginPluginSecret;
  if (typeof c.rejectMessage === "string") camo.rejectMessage = c.rejectMessage;
  if (typeof c.serverName === "string") camo.serverName = c.serverName;
  return { camouflage: camo };
}

export function buildTransportJson(camo: TransportCamouflage, prev?: unknown): string {
  const root: Record<string, unknown> =
    prev && typeof prev === "object"
      ? { ...(prev as Record<string, unknown>) }
      : {
          tcp: {
            dialTimeout: "3s",
            reconnectInitialBackoff: "1s",
            reconnectMaxBackoff: "60s",
            rxIdleTimeout: "5s",
          },
        };
  root.camouflage = {
    profile: camo.profile,
    deep: camo.deep,
    handshakeTimeout: camo.handshakeTimeout,
    loginUsername: camo.loginUsername,
    loginPluginChannel: camo.loginPluginChannel,
    loginPluginSecret: camo.loginPluginSecret,
    rejectMessage: camo.rejectMessage,
    serverName: camo.serverName,
  };
  const mcEnabled = camo.profile === "minecraft";
  root.mc = {
    enabled: mcEnabled,
    handshakeTimeout: camo.handshakeTimeout,
    deepCamouflage: camo.deep,
    loginUsername: camo.loginUsername,
    loginPluginChannel: camo.loginPluginChannel,
    loginPluginSecret: camo.loginPluginSecret,
    rejectMessage: camo.rejectMessage,
  };
  return JSON.stringify(root, null, 2);
}
