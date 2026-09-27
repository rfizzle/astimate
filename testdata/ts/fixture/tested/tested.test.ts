import { describe, expect, it, test } from "vitest";
import { bounded, join, shout } from "./tested";

describe("join", () => {
  it("joins numbers", () => {
    expect(join([1, 2])).toBe("1,2");
  });
});

test("bounded", () => {
  expect(bounded(3, 1, 5)).toBe(true);
  expect(shout("ab")).toBe(2);
});
