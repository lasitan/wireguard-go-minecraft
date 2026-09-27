import type { Mesh, Meta } from "./models";

/** Demo nodes that should stay offline (never receive simulated heartbeats). */
const DEMO_OFFLINE_IDS = new Set(["aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004"]);

/**
 * Simulate agent heartbeats in dev preview so online status does not expire
 * after ONLINE_MS. Mutates lastSeen in place; keeps topology/positions intact.
 */
export function touchDemoHeartbeats(mesh: Mesh) {
  const now = new Date().toISOString();
  for (const n of mesh.nodes) {
    if (!DEMO_OFFLINE_IDS.has(n.id)) n.lastSeen = now;
  }
}

/** Dev-preview mesh: solid rules, indirect dashed peers, and one IP conflict. */
export function demoMesh(): Mesh {
  const now = new Date().toISOString();
  const ago = new Date(Date.now() - 120_000).toISOString();
  const later = new Date(Date.now() - 5_000).toISOString();
  const daysAgo = (d: number) => new Date(Date.now() - d * 86_400_000).toISOString();
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
        addressChangedAt: daysAgo(30),
        publicV4: "1.2.3.4",
        publicV6: "2001:db8:1::4",
        geoCountry: "Japan",
        geoCountryCode: "JP",
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        name: "db-replica",
        role: "server",
        address: "100.96.0.2/24",
        listenPort: 25591,
        token: "demo-token-db",
        lastSeen: now,
        addressChangedAt: daysAgo(20),
        routes: ["192.168.50.0/24"],
        publicV4: "203.0.113.20",
        geoCountry: "Singapore",
        geoCountryCode: "SG",
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        name: "MacBook Pro",
        role: "client",
        address: "100.96.0.10/24",
        token: "demo-token-mac",
        lastSeen: now,
        addressChangedAt: daysAgo(12),
        publicV4: "198.51.100.7",
        publicV6: "2001:db8:abcd::7",
        geoCountry: "China",
        geoCountryCode: "CN",
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004",
        name: "dev-vm",
        role: "client",
        address: "",
        token: "demo-token-vm",
        lastSeen: ago,
      },
      // Same VPN IP as db-replica but changed later → loses the conflict.
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0005",
        name: "stale-clone",
        role: "client",
        address: "100.96.0.2/24",
        token: "demo-token-clone",
        lastSeen: later,
        addressChangedAt: daysAgo(1),
        publicV4: "192.0.2.55",
        geoCountry: "United States",
        geoCountryCode: "US",
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0006",
        name: "backup-nas",
        role: "client",
        address: "100.96.0.20/24",
        token: "demo-token-nas",
        lastSeen: now,
        disabled: true,
        addressChangedAt: daysAgo(40),
        publicV4: "198.51.100.88",
        geoCountry: "Germany",
        geoCountryCode: "DE",
      },
    ],
    links: [
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0005", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0006", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002", keepalive: 5 },
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
