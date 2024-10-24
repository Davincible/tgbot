package loginbot

import (
	"regexp"
)

var (
	reCode = regexp.MustCompile(`\b\d{5}\b`)
)

// extractCode takes a string and returns the first 5-digit numeric code found
// Returns empty string if no code is found
func extractCode(input string) string {
	// Find the first match
	match := reCode.FindString(input)

	return match
}

// hasCode takes a string and returns true if it contains a 5-digit numeric code
func hasCode(input string) bool {
	// Check if there's a match
	return reCode.MatchString(input)
}
