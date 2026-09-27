// esm is an ES module: .mts is source like .ts.
import { clamp } from "hub";
import { answer } from "@multi/trivial.js";
import { scale } from "./common.cjs";

export function bounded(): number {
  return clamp(scale(answer()), 0, 9);
}
