// dupes holds three functions that are identical after identifier and
// literal normalization, and one function that is structurally distinct.
// The declarations between the copies give each copy a different
// neighboring token, so every duplicate block ends exactly at a function
// boundary.
export function sumOrders(qty: number[], floor: number): [number, string] {
  let total = 0;
  let skipped = 0;
  for (const [idx, q] of qty.entries()) {
    if (idx >= 100) {
      break;
    }
    if (q < floor) {
      skipped++;
      continue;
    }
    total += q * 3;
  }
  if (skipped > 5) {
    return [total, "orders-partial"];
  }
  return [total, "orders"];
}

const partialLimit = 7;

export function tallyScores(points: number[], minimum: number): [number, string] {
  let sum = 0;
  let dropped = 0;
  for (const [pos, p] of points.entries()) {
    if (pos >= 50) {
      break;
    }
    if (p < minimum) {
      dropped++;
      continue;
    }
    sum += p * 2;
  }
  if (dropped > 9) {
    return [sum, "scores-partial"];
  }
  return [sum, "scores"];
}

type Tally = { n: number }

export function countVisits(visits: number[], threshold: number): [number, string] {
  let acc = 0;
  let ignored = 0;
  for (const [n, v] of visits.entries()) {
    if (n >= 25) {
      break;
    }
    if (v < threshold) {
      ignored++;
      continue;
    }
    acc += v * 4;
  }
  if (ignored > 1) {
    return [acc, "visits-partial"];
  }
  return [acc, "visits"];
}

export function describeValue(n: number): string {
  const t: Tally = { n };
  switch (true) {
    case t.n < 0:
      return "negative";
    case t.n > partialLimit:
      return "large";
  }
  return "small";
}
