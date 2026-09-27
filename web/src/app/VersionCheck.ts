import { GetVersion } from "../api/GetVersion";
import { RecheckVersion } from "../api/RecheckVersion";
import { demoVersion } from "../core/demoVersion";
import { notify, state } from "../core/state";

const POLL_MS = 10 * 60_000;
const RECHECK_WAIT_MS = 2500;
let timer: number | null = null;

export async function loadVersion() {
  state.version = state.demo ? demoVersion(state.mesh) : await GetVersion();
  notify();
}

/** Ask the Master to query GitHub now, then pick up the fresh result. */
export async function recheckVersion() {
  if (state.demo) {
    await new Promise((r) => setTimeout(r, 900));
    state.version = { ...demoVersion(state.mesh), checkedAt: new Date().toISOString() };
    notify();
    return;
  }
  await RecheckVersion();
  await new Promise((r) => setTimeout(r, RECHECK_WAIT_MS));
  await loadVersion();
}

export function startVersionPoll() {
  stopVersionPoll();
  loadVersion().catch(() => {});
  timer = window.setInterval(() => {
    if (state.token) loadVersion().catch(() => {});
  }, POLL_MS);
}

export function stopVersionPoll() {
  if (timer != null) {
    clearInterval(timer);
    timer = null;
  }
  state.version = null;
}
