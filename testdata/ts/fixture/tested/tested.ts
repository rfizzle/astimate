// tested has every exported function referenced from a test.
import { format } from "util";
import { clamp } from "../hub/index";
import { countUpper } from "./count";

function join(nums: number[]): string {
  const parts: string[] = [];
  for (const n of nums) {
    parts.push(String(n));
  }
  return format("%s", parts.join(","));
}

function bounded(n: number, lo: number, hi: number): boolean {
  return clamp(n, lo, hi) === n;
}

export { join, bounded };

export const shout = (s: string): number => countUpper(s.toUpperCase());
