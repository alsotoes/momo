package common

import (
	"bytes"
	"strconv"
)

// SafeParseInt extracts an int64 from a byte slice that may be null-padded.
// It performs strict character validation and overflow checks without heap allocations.
func SafeParseInt(b []byte) (int64, error) {
	// Find the end of the numeric part (either a null byte or the end of the slice)
	idx := bytes.IndexByte(b, 0)
	if idx == -1 {
		idx = len(b)
	}

	if idx == 0 {
		return 0, strconv.ErrSyntax
	}

	// Manual iteration to avoid string allocation and provide defensive character checking
	var res uint64
	var sign int64 = 1
	start := 0

	if b[0] == '-' {
		sign = -1
		start = 1
	} else if b[0] == '+' {
		start = 1
	}

	if start == idx {
		return 0, strconv.ErrSyntax
	}

	// Constants for overflow checks
	const cutoff = uint64(1<<63 - 1)
	const maxVal = cutoff / 10

	for i := start; i < idx; i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, strconv.ErrSyntax
		}

		v := uint64(c - '0')

		// Overflow check
		if res > maxVal || (res == maxVal && v > cutoff%10) {
			if sign == -1 && res == maxVal && v == cutoff%10+1 {
				// Handle math.MinInt64 edge case
				res = uint64(1 << 63)
				continue
			}
			return 0, strconv.ErrRange
		}

		res = res*10 + v
	}

	if sign == -1 {
		return int64(^res + 1), nil
	}

	return int64(res), nil
}
