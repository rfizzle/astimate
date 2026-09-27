// b imports hub through the @app/* path alias.
import { clamp } from "@app/hub";

export const limit = (n: number): number => clamp(n, 0, 9);
