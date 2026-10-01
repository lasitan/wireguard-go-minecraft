export const ONLINE_MS = 45_000;
export const MASTER_ID = "__master__";
/** Gateway id in mesh.paths meaning "via Master's cross-subnet relay" (core.RelayNodeID). */
export const RELAY_ID = "master";
export const VIEW = { w: 1200, h: 720, cx: 600, cy: 360, radius: 240 };
export const FOCUS_SCALE = 1.38;
export const CAM_MS = 380;
export const TOKEN_KEY = "lasitan_admin_token";
export const isDevPreview = import.meta.env.DEV;

/** viewBox width bounds (home width = VIEW.w). */
export const ZOOM_MIN_W = VIEW.w * 0.35;
export const ZOOM_MAX_W = VIEW.w * 3.2;
/** Pointer movement below this (px) counts as a click, not a drag. */
export const DRAG_CLICK_PX = 5;
/** Fingers wobble more than a mouse before a tap is read as a drag. */
export const DRAG_CLICK_PX_TOUCH = 10;
/** Touch hold this long on a card before dragging = Ctrl/Shift "add mother" mode. */
export const LONG_PRESS_MS = 450;

export function dragClickPx(pointerType: string): number {
  return pointerType === "touch" ? DRAG_CLICK_PX_TOUCH : DRAG_CLICK_PX;
}
