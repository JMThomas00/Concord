package client

import (
	"math/rand"
	"strings"
	"time"
)

// Banner represents an ASCII art banner
type Banner struct {
	Name   string
	Art    string
	Height int
}

var (
	// Random generator initialized once
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// trimBannerArt strips trailing whitespace from each line of banner art so
// lipgloss measures the true visual width when centering on screen.
func trimBannerArt(art string) string {
	lines := strings.Split(art, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// GetRandomBanner returns a random banner, ensuring it's different from the last one
// It takes the lastBannerIndex (from config) and returns the selected banner and the new index
func GetRandomBanner(lastBannerIndex int) (Banner, int) {
	if len(banners) == 0 {
		// Fallback if banners slice is empty
		return Banner{
			Name:   "Default",
			Art:    "CONCORD",
			Height: 1,
		}, -1
	}

	if len(banners) == 1 {
		// Only one banner available
		return banners[0], 0
	}

	// Pick uniformly from all banners except the last one shown.
	// Choose from [0, n-2], then shift up by 1 if it lands on lastBannerIndex.
	// This guarantees termination in one step and a perfectly uniform distribution.
	n := len(banners)
	newIndex := rng.Intn(n - 1)
	if newIndex >= lastBannerIndex {
		newIndex++
	}

	return banners[newIndex], newIndex
}
