import { formatBytes } from "./formatBytes";

/** Bytes per second → "1.2 MB/s". */
export function formatRate(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}
