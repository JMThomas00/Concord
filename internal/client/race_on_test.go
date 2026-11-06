//go:build race

package client

// raceEnabled: built with -race, which slows some tests past real limits.
const raceEnabled = true
