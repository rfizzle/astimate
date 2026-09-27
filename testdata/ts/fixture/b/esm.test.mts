import { expect, it } from "vitest";
import { bounded } from "./esm.mjs";

it("bounds", () => {
  expect(bounded()).toBe(9);
});
