import type { Mesh, Meta } from "./models";

/** Dev-preview mesh: solid rules, indirect dashed peers, and one IP conflict. */
export function demoMesh(): Mesh {
  const now = new Date().toISOString();
  const ago = new Date(Date.now() - 120_000).toISOString();
  const later = new Date(Date.now() - 5_000).toISOString();
  return {
    revision: 7,
    nodes: [
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        name: "web-server",
        role: "server",
        address: "100.96.0.1/24",
        endpoint: "1.2.3.4:25590",
        listenPort: 25590,
        token: "demo-token-web",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        name: "db-replica",
        role: "client",
        address: "100.96.0.2/24",
        token: "demo-token-db",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        name: "MacBook Pro",
        role: "client",
        address: "100.96.0.10/24",
        token: "demo-token-mac",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004",
        name: "dev-vm",
        role: "client",
        address: "",
        token: "demo-token-vm",
        lastSeen: ago,
      },
      // Same VPN IP as db-replica — later change; frontend marks yellow only.
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0005",
        name: "stale-clone",
        role: "client",
        address: "100.96.0.2/24",
        token: "demo-token-clone",
        lastSeen: later,
      },
    ],
    links: [
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0005", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
    ],
    forwards: [
      {
        nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        protocol: "tcp",
        listen: "3389",
        destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        destPort: 3389,
      },
      {
        nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        protocol: "tcp",
        listen: "5432",
        destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        destPort: 5432,
      },
    ],
  };
}

export function demoMeta(): Meta {
  return {
    enrollToken: "dev-preview-enroll-token",
    vpnSubnet: "100.96.0.0/24",
    listen: ":8443",
    defaultIface: "wg0",
    defaultPoll: "10s",
  };
}
