// hidden carries module-level state, module initialization calls and one
// function nested four levels deep.
import { clamp } from "@app/hub/index";

let limit = 3;
var events: number[] = [], done = false;
let counter = 0;
const ceiling = 10;

limit = clamp(limit, 1, ceiling);
events.push(limit);
events.push(counter);

export function drain(active: boolean, modes: string[]): number {
  if (active) {
    for (const mode of modes) {
      switch (mode) {
        case "wait":
          while (events.length > 0 && !done) {
            counter += events.pop() ?? 0;
          }
      }
    }
  }
  return counter;
}
