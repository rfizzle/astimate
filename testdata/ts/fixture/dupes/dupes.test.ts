import { expect, it, test } from "vitest";
import { answer } from "@app/trivial";
import { describeValue, sumOrders, tallyScores } from "./dupes";

it("describes negatives", () => {
  expect(describeValue(-1)).toBe("negative");
});

test("sums and tallies", () => {
  expect(sumOrders([1], 0)[0] + tallyScores([1], 0)[0]).toBe(answer() - 37);
});
