// covered is exercised only by test/covered.test.ts, a test file in a
// directory with no source file, so the directory is no package.
export function reach(n: number): number {
  return n + 1;
}

export function stray(n: number): number {
  return n - 1;
}
