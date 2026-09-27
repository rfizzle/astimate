import { expect, it } from "vitest";
import { countUpper } from "../count";

it.each(["AbC", "xYz"])("counts %s", (s) => {
  expect(countUpper(s)).toBeGreaterThan(0);
});
