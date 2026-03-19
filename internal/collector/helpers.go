package collector

import (
	"math"
	"strings"
	"time"
)

func round2(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func timeFromUnixSeconds(value uint64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(int64(value), 0).UTC()
}

func timeFromUnixMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func containsAnyNormalized(haystacks []string, needles []string) bool {
	if len(needles) == 0 {
		return true
	}
	for _, haystack := range haystacks {
		normalized := strings.ToLower(haystack)
		for _, needle := range needles {
			if strings.Contains(normalized, needle) {
				return true
			}
		}
	}
	return false
}
