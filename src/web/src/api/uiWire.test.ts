import { describe, expect, it } from "vitest";
import { decodeFrame, decodePresence, encodeFrame, encodeString, Link, MsgType } from "./uiWire";

function presencePayload(): Uint8Array {
  // Same layout Go's wire.Presence.Marshal writes.
  const id = new TextEncoder().encode("node-α");
  const b = new Uint8Array(8 + 2 + 2 + id.length + 1 + 8 + 8 + 8 + 4);
  const dv = new DataView(b.buffer);
  let o = 0;
  dv.setBigInt64(o, 1_700_000_000_123n);
  o += 8;
  dv.setUint16(o, 1);
  o += 2;
  dv.setUint16(o, id.length);
  o += 2;
  b.set(id, o);
  o += id.length;
  dv.setUint8(o++, Link.WS);
  dv.setBigInt64(o, 1_700_000_000_000n);
  o += 8;
  dv.setFloat64(o, 1234.5);
  o += 8;
  dv.setFloat64(o, 0.25);
  o += 8;
  dv.setUint32(o, 3500);
  return b;
}

describe("uiWire", () => {
  it("round-trips the frame header", () => {
    const f = decodeFrame(encodeFrame(MsgType.UISubscribe, 7, encodeString("abc")).slice().buffer);
    expect(f.type).toBe(MsgType.UISubscribe);
    expect(f.seq).toBe(7);
    expect(Array.from(f.payload)).toEqual([0, 3, 97, 98, 99]);
  });

  it("decodes presence", () => {
    const p = decodePresence(presencePayload());
    expect(p.nowMs).toBe(1_700_000_000_123);
    expect(p.nodes).toEqual([
      { nodeId: "node-α", link: Link.WS, lastSeenMs: 1_700_000_000_000, rxRate: 1234.5, txRate: 0.25, rttMs: 3.5 },
    ]);
  });

  it("rejects truncated presence", () => {
    const b = presencePayload();
    expect(() => decodePresence(b.subarray(0, b.length - 1))).toThrow();
  });
});
