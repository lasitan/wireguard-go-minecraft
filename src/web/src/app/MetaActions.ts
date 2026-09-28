import { PatchMeta } from "../api/PatchMeta";
import type { MetaPatch } from "../core/models";
import { notify, state } from "../core/state";
import { maskPool } from "../utils/validateSettings";

export async function patchMeta(patch: MetaPatch): Promise<void> {
  if (state.demo) {
    if (!state.meta) return;
    state.meta = {
      ...state.meta,
      ...(patch.enrollToken !== undefined ? { enrollToken: patch.enrollToken.trim() } : {}),
      ...(patch.vpnSubnet !== undefined ? { vpnSubnet: maskPool(patch.vpnSubnet) } : {}),
      ...(patch.transportJson !== undefined
        ? { transport: JSON.parse(patch.transportJson) as unknown }
        : {}),
    };
  } else {
    state.meta = await PatchMeta(patch);
  }
  notify();
}
