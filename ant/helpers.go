package ant

import "strings"

// capitalize returns a string with the first letter capitalized.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}
