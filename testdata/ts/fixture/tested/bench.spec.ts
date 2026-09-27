import { bench } from "vitest";
import { join } from "./tested";

bench("join", () => {
  join([1, 2, 3]);
});
