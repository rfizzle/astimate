// a imports hub through a relative path.
import { normalize } from "../hub";

export function label(s: string): string {
  return "a:" + normalize(s);
}
