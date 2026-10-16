// @generated
// Written by a schema tool. Do not edit.
import { z } from "zod";

export let cache = 0;

export function parseLabel(raw: unknown): string {
  if (typeof raw === "string" && raw !== "") {
    for (const c of raw) {
      if (c === "!") {
        return z.string().parse(raw);
      }
    }
  }
  return "";
}
