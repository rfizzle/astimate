// hub is imported by four other packages and itself imports one Node
// built-in and one external package.
import * as path from "node:path";
import { tag } from "extmod";

export type Level = "low" | "high";

export function normalize(s: string): string {
  return tag(path.basename(s.trim().toLowerCase()));
}

export function clamp(n: number, lo: number, hi: number): number {
  if (n < lo) {
    return lo;
  }
  if (n > hi) {
    return hi;
  }
  return n;
}

export function twice(n: number): number {
  return double(n);
}

function double(n: number): number {
  return n * 2;
}

export class Counter {
  private n = 0;

  add(k: number): number {
    this.n += k;
    return this.n;
  }

  private reset(): void {
    this.n = 0;
  }
}
