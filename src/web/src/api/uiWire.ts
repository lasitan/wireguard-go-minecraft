/**
 * Admin UI <-> Master binary frames, mirroring src/core/wire (ui.go):
 * [u8 type][u32 seq][payload], big-endian; strings are [u16 len][utf-8].
 */
export const MsgType = {
  Ping: 4,
  Pong: 5,
  UIHello: 0x41,
  UIHelloAck: 0x42,
  UIError: 0x43,
  UIMesh: 0x44,
  UIMeta: 0x45,
  UIVersion: 0x46,
  UIPresence: 0x47,
  UISubscribe: 0x48,
  UINodeStats: 0x49,
} as const;

export const ErrCodeAuth = 401;

export const Link = { Offline: 0, HTTP: 1, WS: 2 } as const;

export type Frame = { type: number; seq: number; payload: Uint8Array };

export type PresenceNode = {
  nodeId: string;
  link: number;
  lastSeenMs: number;
  rxRate: number;
  txRate: number;
  rttMs: number;
};

export type Presence = { nowMs: number; nodes: PresenceNode[] };

const enc = new TextEncoder();
const dec = new TextDecoder();

export function encodeFrame(type: number, seq: number, payload: Uint8Array = new Uint8Array()): Uint8Array {
  const b = new Uint8Array(5 + payload.length);
  b[0] = type;
  new DataView(b.buffer).setUint32(1, seq >>> 0);
  b.set(payload, 5);
  return b;
}

export function decodeFrame(buf: ArrayBuffer): Frame {
  const b = new Uint8Array(buf);
  if (b.length < 5) throw new Error("wire: frame truncated");
  return { type: b[0], seq: new DataView(buf).getUint32(1), payload: b.subarray(5) };
}

export function encodeString(s: string): Uint8Array {
  const body = enc.encode(s);
  const b = new Uint8Array(2 + body.length);
  new DataView(b.buffer).setUint16(0, body.length);
  b.set(body, 2);
  return b;
}

class Reader {
  private off = 0;
  private dv: DataView;
  constructor(private b: Uint8Array) {
    this.dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  }
  private need(n: number) {
    if (this.off + n > this.b.length) throw new Error("wire: frame truncated");
  }
  u8(): number {
    this.need(1);
    return this.dv.getUint8(this.off++);
  }
  u16(): number {
    this.need(2);
    const v = this.dv.getUint16(this.off);
    this.off += 2;
    return v;
  }
  u32(): number {
    this.need(4);
    const v = this.dv.getUint32(this.off);
    this.off += 4;
    return v;
  }
  i64(): number {
    this.need(8);
    const v = Number(this.dv.getBigInt64(this.off));
    this.off += 8;
    return v;
  }
  f64(): number {
    this.need(8);
    const v = this.dv.getFloat64(this.off);
    this.off += 8;
    return v;
  }
  str(): string {
    const n = this.u16();
    this.need(n);
    const s = dec.decode(this.b.subarray(this.off, this.off + n));
    this.off += n;
    return s;
  }
}

export function decodePresence(p: Uint8Array): Presence {
  const r = new Reader(p);
  const nowMs = r.i64();
  const n = r.u16();
  const nodes: PresenceNode[] = [];
  for (let i = 0; i < n; i++) {
    nodes.push({
      nodeId: r.str(),
      link: r.u8(),
      lastSeenMs: r.i64(),
      rxRate: r.f64(),
      txRate: r.f64(),
      rttMs: r.u32() / 1000,
    });
  }
  return { nowMs, nodes };
}

export function decodeError(p: Uint8Array): { code: number; message: string } {
  const r = new Reader(p);
  return { code: r.u16(), message: r.str() };
}

export function decodeJSON<T>(p: Uint8Array): T {
  return JSON.parse(dec.decode(p)) as T;
}
