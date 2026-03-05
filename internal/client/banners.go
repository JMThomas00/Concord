package client

import (
	"math/rand"
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

	// Select a random index different from the last one
	newIndex := rng.Intn(len(banners))
	for newIndex == lastBannerIndex {
		newIndex = rng.Intn(len(banners))
	}

	return banners[newIndex], newIndex
}
