export type Node = {
  id: string;
  name?: string;
  role: string;
  address: string;
  endpoint?: string;
  token?: string;
  listenPort?: number;
  lastSeen?: string;
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
export type EdgeStatus = "green" | "yellow" | "red";

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
