package tested

import (
	"sort"
	"strings"
	"unicode"
)

// romanValues are the numeral values from largest to smallest, subtractive
// pairs included; romanSymbols holds the matching numerals in order.
func romanValues() []int {
	return []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
}

const romanSymbols = "M CM D CD C XC L XL X IX V IV I"

// Roman renders n in Roman numerals, or "" when n is outside 1 to 3999.
func Roman(n int) string {
	if n < 1 || n > 3999 {
		return ""
	}
	symbols := strings.Fields(romanSymbols)
	var b strings.Builder
	for i, v := range romanValues() {
		for n >= v {
			b.WriteString(symbols[i])
			n -= v
		}
	}
	return b.String()
}

// RunLength compresses runs of a repeated rune as the rune followed by the
// run length when the run is longer than one.
func RunLength(s string) string {
	runes := []rune(s)
	var out strings.Builder
	i := 0
	for i < len(runes) {
		j := i + 1
		for j < len(runes) && runes[j] == runes[i] {
			j++
		}
		out.WriteRune(runes[i])
		if run := j - i; run > 1 {
			out.WriteString(itoa(run))
		}
		i = j
	}
	return out.String()
}

// itoa formats a non-negative int without importing strconv twice over.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	for l, r := 0, len(digits)-1; l < r; l, r = l+1, r-1 {
		digits[l], digits[r] = digits[r], digits[l]
	}
	return string(digits)
}

// closers maps each closing bracket to its opener.
func closers() map[rune]rune {
	return map[rune]rune{')': '(', ']': '[', '}': '{'}
}

// Balanced reports whether every bracket in s is closed in order. Runes
// other than ()[]{} are ignored.
func Balanced(s string) bool {
	pairs := closers()
	stack := make([]rune, 0, len(s))
	ok := true
	for _, c := range s {
		if stack, ok = step(stack, c, pairs); !ok {
			return false
		}
	}
	return len(stack) == 0
}

// step pushes an opening bracket, pops the opener a closing bracket needs,
// and reports false when that opener is not on top.
func step(stack []rune, c rune, pairs map[rune]rune) ([]rune, bool) {
	switch c {
	case '(', '[', '{':
		return append(stack, c), true
	case ')', ']', '}':
		top := len(stack) - 1
		if top < 0 || stack[top] != pairs[c] {
			return stack, false
		}
		return stack[:top], true
	}
	return stack, true
}

// Median returns the middle value of xs, averaging the two middle values
// when the length is even, and false for an empty xs. xs is not modified.
func Median(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	sortedCopy := append([]float64(nil), xs...)
	sort.Float64s(sortedCopy)
	mid := len(sortedCopy) / 2
	if len(sortedCopy)%2 == 1 {
		return sortedCopy[mid], true
	}
	return (sortedCopy[mid-1] + sortedCopy[mid]) / 2, true
}

// wordCount is one entry of a frequency ranking.
type wordCount struct {
	word  string
	count int
}

// splitWords lower-cases s and splits it on anything that is not a letter
// or digit.
func splitWords(s string) []string {
	notWord := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	return strings.FieldsFunc(strings.ToLower(s), notWord)
}

// tally counts each word.
func tally(words []string) map[string]int {
	seen := make(map[string]int, len(words))
	for _, w := range words {
		seen[w]++
	}
	return seen
}

// rank orders counts by descending frequency, then alphabetically.
func rank(counts map[string]int) []wordCount {
	ranked := make([]wordCount, 0, len(counts))
	for w, c := range counts {
		ranked = append(ranked, wordCount{word: w, count: c})
	}
	sort.Slice(ranked, func(a, b int) bool {
		if ranked[a].count != ranked[b].count {
			return ranked[a].count > ranked[b].count
		}
		return ranked[a].word < ranked[b].word
	})
	return ranked
}

// TopWords returns the k most frequent words of text, most frequent first,
// ties broken alphabetically. Words are case-folded runs of letters and
// digits. A k below one yields nil.
func TopWords(text string, k int) []string {
	if k < 1 {
		return nil
	}
	ranked := rank(tally(splitWords(text)))
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	top := make([]string, len(ranked))
	for i := range ranked {
		top[i] = ranked[i].word
	}
	return top
}
