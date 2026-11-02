package tree

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// FormatBytes writes a size in at most 4 cells: 900B, 612M, 1.2G, 12G. The unit sticks
// to the number, and G starts at 1000M. base is 1000 for disk, as docker system df
// counts, and 1024 for memory, as docker stats counts.
func FormatBytes(v int64, base float64) string {
	if v < 0 {
		v = 0
	}
	if float64(v) < 1000 {
		return strconv.FormatInt(v, 10) + "B"
	}
	units := []string{"K", "M", "G", "T", "P"}
	value := float64(v)
	for i, u := range units {
		value /= base
		// Promote at what the digits would show, so that 999.7M does not read 1000M.
		if math.Round(value) < 1000 || i == len(units)-1 {
			if u == "K" || u == "M" || value >= 9.95 {
				return fmt.Sprintf("%.0f%s", value, u)
			}
			return fmt.Sprintf("%.1f%s", value, u)
		}
	}
	return ""
}

// FormatDisk formats a size from the disk cache.
func FormatDisk(v int64) string { return FormatBytes(v, 1000) }

// FormatMem formats a size from docker stats.
func FormatMem(v int64) string { return FormatBytes(v, 1024) }

// FormatCPU keeps its decimal below 100%: 4.1%, 12.0%, then 173%, so that it takes at
// most 5 cells for any real machine.
func FormatCPU(pct float64) string {
	if pct >= 99.95 {
		return fmt.Sprintf("%.0f%%", pct)
	}
	return fmt.Sprintf("%.1f%%", pct)
}

// FormatAge writes an age in one unit and at most 3 cells: 45s, 10m, 2h, 12d, 14w, 2y.
func FormatAge(seconds int64) string {
	const (
		minute = 60
		hour   = 60 * minute
		day    = 24 * hour
		week   = 7 * day
		year   = 365 * day
	)
	switch {
	case seconds < 0:
		return "0s"
	case seconds < minute:
		return fmt.Sprintf("%ds", seconds)
	case seconds < hour:
		return fmt.Sprintf("%dm", seconds/minute)
	case seconds < day:
		return fmt.Sprintf("%dh", seconds/hour)
	case seconds < 100*day:
		return fmt.Sprintf("%dd", seconds/day)
	case seconds < 100*week && seconds < 2*year:
		return fmt.Sprintf("%dw", seconds/week)
	}
	return fmt.Sprintf("%dy", seconds/year)
}

// FormatRowAge is an age in a row, which is not live data: under a minute it says so, and it
// does not count seconds as if it did.
func FormatRowAge(seconds int64) string {
	if seconds < 60 {
		return "<1m"
	}
	return FormatAge(seconds)
}

var statusAge = regexp.MustCompile(`(?i)(?:^up |\) )(about an?|less than a|\d+) (second|minute|hour|day|week|month|year)s?`)

// statusSeconds reads the age out of the status text of docker: "Up 2 hours (healthy)",
// "Exited (0) 25 hours ago", "Up About a minute". The contract gives no time for one
// container. It reports false for a text with no age, such as "Created".
func statusSeconds(status string) (int64, bool) {
	match := statusAge.FindStringSubmatch(strings.TrimSpace(status))
	if match == nil {
		return 0, false
	}
	n := int64(1)
	if v, err := strconv.ParseInt(match[1], 10, 64); err == nil {
		n = v
	}
	if strings.EqualFold(match[1], "less than a") {
		n = 0
	}
	unit := map[string]int64{"second": 1, "minute": 60, "hour": 3600, "day": 86400, "week": 7 * 86400, "month": 30 * 86400, "year": 365 * 86400}
	return n * unit[strings.ToLower(match[2])], true
}
