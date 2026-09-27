package tested

import (
	"strconv"
	"strings"
)

// joins counts JoinAgain calls: new package-level state.
var joins int

// JoinAgain is a copy of Join with no test: a duplicate block, an untested
// export and a global in one change.
func JoinAgain(nums []int) string {
	joins++
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}
