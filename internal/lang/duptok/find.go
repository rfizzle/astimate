package duptok

import (
	"cmp"
	"slices"
)

// find returns the suffix array of s and the maximal repeats of at least
// minTokens codes in s that survive merging, as intervals of that suffix
// array. s must end with a code that occurs nowhere else in it.
//
// Let L(q) be the longest repeat starting at stream position q (the larger
// LCP with q's two suffix-array neighbours). An occurrence [q, q+l) lies
// inside a longer repeat exactly when l < L(q) or some q' < q has
// q'+L(q') >= q+l. So, sweeping q upwards with reach = max(q'+L(q')) over
// q' < q, a position with L(q) >= minTokens and q+L(q) > reach starts an
// occurrence of a surviving block: the sequence of length L(q) at q. Its
// LCP interval, found from previous and next smaller LCP values, gives all
// its occurrences; blocks are the distinct intervals.
func find(s []int32, minTokens int) (sa []int32, reps []repeat) {
	n := len(s)
	if n < 2 {
		return nil, nil
	}
	sa, rank := suffixArray(s)
	lcp := lcpArray(s, sa, rank)
	prevLess, nextLess := smallerNeighbours(lcp)
	lcpAt := func(k int32) int32 {
		if int(k) < n {
			return lcp[k]
		}
		return 0
	}
	type key struct{ lb, n int32 }
	seen := make(map[key]bool)
	reach := int32(0)
	for q := range int32(n) {
		r := rank[q]
		longest := max(lcp[r], lcpAt(r+1))
		if int(longest) >= minTokens && q+longest > reach {
			lb, rb := r, r
			if lcp[r] == longest {
				lb = prevLess[r]
			}
			if lcpAt(r+1) == longest {
				rb = nextLess[r+1] - 1
			}
			if k := (key{lb, longest}); !seen[k] {
				seen[k] = true
				reps = append(reps, repeat{lb: lb, rb: rb, n: longest})
			}
		}
		reach = max(reach, q+longest)
	}
	return sa, reps
}

// smallerNeighbours returns, for each index k of lcp, the largest j < k and
// the smallest j > k with lcp[j] < lcp[k]; -1 and len(lcp) when there is
// none.
func smallerNeighbours(lcp []int32) (prevLess, nextLess []int32) {
	n := int32(len(lcp))
	return nearestSmaller(lcp, 0, n, 1, -1), nearestSmaller(lcp, n-1, -1, -1, n)
}

// nearestSmaller visits the indexes of lcp from first towards stop, in
// steps of step, and returns for each the nearest index visited before it
// whose value is smaller, or none when there is none, using a monotonic
// stack.
func nearestSmaller(lcp []int32, first, stop, step, none int32) []int32 {
	out := make([]int32, len(lcp))
	stack := make([]int32, 0, 64)
	for k := first; k != stop; k += step {
		for len(stack) > 0 && lcp[stack[len(stack)-1]] >= lcp[k] {
			stack = stack[:len(stack)-1]
		}
		out[k] = none
		if len(stack) > 0 {
			out[k] = stack[len(stack)-1]
		}
		stack = append(stack, k)
	}
	return out
}

// suffixArray returns the suffix array of s and its inverse, built by
// prefix doubling with counting sorts. s must end with a unique code.
func suffixArray(s []int32) (sa, rank []int32) {
	n := len(s)
	sa = make([]int32, n)
	rank = make([]int32, n)
	for i := range sa {
		sa[i] = int32(i)
	}
	slices.SortFunc(sa, func(a, b int32) int { return cmp.Compare(s[a], s[b]) })
	for i := 1; i < n; i++ {
		rank[sa[i]] = rank[sa[i-1]]
		if s[sa[i]] != s[sa[i-1]] {
			rank[sa[i]]++
		}
	}
	if n == 0 || int(rank[sa[n-1]]) == n-1 {
		return sa, rank
	}
	tmp := make([]int32, n)
	by2 := make([]int32, n)
	cnt := make([]int32, n+1)
	for k := 1; ; k <<= 1 {
		// Order by second key: suffixes with no second half first.
		j := 0
		for i := n - k; i < n; i++ {
			by2[j] = int32(i)
			j++
		}
		for _, v := range sa {
			if int(v) >= k {
				by2[j] = v - int32(k)
				j++
			}
		}
		// Stable counting sort by first key.
		clear(cnt)
		for _, r := range rank {
			cnt[r+1]++
		}
		for i := 1; i <= n; i++ {
			cnt[i] += cnt[i-1]
		}
		for _, v := range by2 {
			sa[cnt[rank[v]]] = v
			cnt[rank[v]]++
		}
		second := func(i int32) int32 {
			if int(i)+k < n {
				return rank[int(i)+k]
			}
			return -1
		}
		tmp[sa[0]] = 0
		for i := 1; i < n; i++ {
			a, b := sa[i-1], sa[i]
			tmp[b] = tmp[a]
			if rank[a] != rank[b] || second(a) != second(b) {
				tmp[b]++
			}
		}
		rank, tmp = tmp, rank
		if int(rank[sa[n-1]]) == n-1 {
			return sa, rank
		}
	}
}

// lcpArray returns lcp where lcp[i] is the length of the longest common
// prefix of the suffixes sa[i-1] and sa[i], and lcp[0] is 0 (Kasai et al.).
func lcpArray(s, sa, rank []int32) []int32 {
	n := len(s)
	lcp := make([]int32, n)
	h := 0
	for i := range n {
		r := rank[i]
		if r == 0 {
			h = 0
			continue
		}
		j := int(sa[r-1])
		for i+h < n && j+h < n && s[i+h] == s[j+h] {
			h++
		}
		lcp[r] = int32(h)
		if h > 0 {
			h--
		}
	}
	return lcp
}
