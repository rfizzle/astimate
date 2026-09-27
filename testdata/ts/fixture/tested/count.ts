export function countUpper(s: string): number {
  let n = 0;
  for (const ch of s) {
    if (ch >= "A" && ch <= "Z") {
      n++;
    }
  }
  return n;
}
