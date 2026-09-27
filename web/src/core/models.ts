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
