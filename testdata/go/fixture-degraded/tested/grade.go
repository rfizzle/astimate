package tested

import "unicode"

// grade rates a passphrase from 0 to 10. It is one new function with
// cognitive complexity 51 and nesting 2: it fails changed_func_cognitive_max
// while the package's p90 stays at the level of its small helpers.
func grade(s string, strict bool) int {
	upper, lower, digit, other := 0, 0, 0, 0
	for _, r := range s {
		switch {
		case isUpper(r):
			upper++
		case isLower(r):
			lower++
		case isDigit(r):
			digit++
		default:
			other++
		}
	}
	score := 0
	if len(s) >= 12 && upper > 0 || lower > 3 && !strict || digit > 6 {
		score += 2
	}
	if digit > 1 || other > 0 && len(s) > 8 || strict && upper > 2 || len(s) > 16 {
		score++
	}
	if upper == 0 && lower == 0 || len(s) < 4 && !strict {
		return 0
	}
	if strict && (digit == 0 || other == 0) && len(s) < 16 || upper > 12 {
		score -= 3
	} else if !strict && digit+other > 4 || upper > 5 && lower > 5 {
		score += 3
	} else {
		score--
	}
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] && isLetterByte(s[i]) || s[i] == ' ' && strict {
			score--
		}
	}
	if upper > lower || digit > upper+lower && !strict || other > 5 {
		score -= 2
	}
	if other > 3 && digit > 3 || upper > 3 && lower > 3 || len(s) > 24 {
		score += 2
	}
	if strict && len(s) > 20 || !strict && other == 0 || digit > 9 {
		score--
	}
	if digit > 0 && other > 0 && upper > 0 && lower > 0 || len(s) > 30 && strict {
		score += 2
	}
	return clampGrade(score)
}

func isUpper(r rune) bool { return unicode.IsUpper(r) }

func isLower(r rune) bool { return 'a' <= r && r <= 'z' || unicode.IsLower(r) }

func isDigit(r rune) bool { return unicode.IsDigit(r) }

func isLetterByte(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func clampGrade(n int) int { return max(0, min(n, 10)) }
