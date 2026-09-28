export type Node = {
  id: string;
  name?: string;
  role: string;
  address: string;
  endpoint?: string;
  token?: string;
  listenPort?: number;
  lastSeen?: string;
  disabled?: boolean;
  routes?: string[];
  /** Later address change loses an IP conflict. */
  addressChangedAt?: string;
  publicV4?: string;
  publicV6?: string;
  geoCountry?: string;
  geoCountryCode?: string;
};

export type NodePatch = {
  name?: string;
  address?: string;
  enabled?: boolean;
  routes?: string[];
  /** > 0 makes the node a magnet mother card; 0 clears it. */
  listenPort?: number;
  /** Dial address override ("" = public IPv4 + listenPort). */
  endpoint?: string;
  /** Attach under a mother card; "" detaches. */
  parentId?: string;
};

/** Mother card id → attached child ids (in attach order), and the reverse. */
export type MagnetStacks = {
  parentOf: Map<string, string>;
  childrenOf: Map<string, string[]>;
};

export type LivePeer = {
  publicKey: string;
  rx: number;
  tx: number;
  lastHandshake?: string;
  nodeId?: string;
  name?: string;
};

export type LiveForward = {
  protocol: string;
  listen: string;
  rx: number;
  tx: number;
};

export type IpTraffic = {
  ip: string;
  nodeId?: string;
  name?: string;
  rx: number;
  tx: number;
  rxRate: number;
  txRate: number;
  updatedAt?: number;
};

/** Master control link: ws = long connection, http = legacy polling. */
export type LinkKind = "ws" | "http" | "offline";

export type NodeStats = {
  nodeId: string;
  link: LinkKind;
  lastSeen?: string;
  connectedAt?: string;
  rttMs?: number;
  /** Build version from the WS Hello; absent for HTTP / pre-2.0.3 agents. */
  agentVersion?: string;
  publicV4?: string;
  publicV6?: string;
  geoCountry?: string;
  geoCountryCode?: string;
  rxRate: number;
  txRate: number;
  sampleAt?: string;
  totals: { rx: number; tx: number };
  peers: LivePeer[];
  forwards: LiveForward[];
  ips: IpTraffic[];
};

export type TrafficRange = "1h" | "24h" | "7d" | "30d";

export type TrafficPoint = { ts: number; rx: number; tx: number };

export type TrafficSeries = {
  range: TrafficRange;
  /** Bucket width in seconds. */
  step: number;
  points: TrafficPoint[];
};

export type Link = {
  fromNodeId: string;
  toNodeId: string;
  allowedIPs?: string[];
  keepalive?: number;
};

export type Forward = {
  nodeId: string;
  protocol: string;
  listen: string;
  destNodeId: string;
  destPort: number;
};

export type Mesh = {
  revision: number;
  nodes: Node[];
  links: Link[];
  forwards: Forward[];
};

export type Meta = {
  enrollToken: string;
  vpnSubnet: string;
  listen: string;
  defaultIface?: string;
  defaultPoll?: string;
};

export type MetaPatch = {
  enrollToken?: string;
  vpnSubnet?: string;
};

export type UpdateCommands = {
  linux: string;
  linuxCn: string;
  installed: string;
  windows: string;
};

export type ReleaseInfo = {
  tag: string;
  version: string;
  url: string;
  notes: string;
  publishedAt: string;
};

export type OutdatedAgent = {
  nodeId: string;
  name: string;
  /** "" = agent too old to report its build version. */
  version: string;
};

export type VersionInfo = {
  current: string;
  latest?: string;
  hasUpdate: boolean;
  release?: ReleaseInfo;
  checkedAt?: string;
  error?: string;
  disabled?: boolean;
  releasesUrl: string;
  commands: UpdateCommands;
  outdatedAgents: OutdatedAgent[];
};

export type UpgradeRequest = { master?: boolean; nodeIds?: string[]; outdated?: boolean; force?: boolean };
export type UpgradeResult = { nodeId?: string; name?: string; ok: boolean; message: string };
export type UpgradeResponse = { master?: UpgradeResult; agents: UpgradeResult[] };

/** sending → running (updater started, waiting for the new version) → done | failed */
export type UpgradePhase = "sending" | "running" | "done" | "failed";
export type UpgradeStatus = { phase: UpgradePhase; message: string; from: string; at: number };

export type Cam = { x: number; y: number; w: number; h: number };
export type Focus = { x: number; y: number; scale: number };

export type PlacedNode = { node: Node; x: number; y: number };

/** Traffic-light status for topology edges. */
export type EdgeStatus = "green" | "yellow" | "red" | "grey";

/** Solid = active rule; dashed = indirect reachability. Mutually exclusive per pair. */
export type EdgeKind = "solid" | "dashed";

export type TopologyEdge = {
  id: string;
  fromId: string;
  toId: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  kind: EdgeKind;
  status: EdgeStatus;
  /** When green, dashes animate toward Master along this orientation. */
  flowTowardMaster: boolean;
};

export type IpConflictMap = Map<string, true>;
