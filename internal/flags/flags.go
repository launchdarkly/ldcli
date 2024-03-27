package flags

import (
	"ldcli/internal/errors"
	"regexp"
	"strings"
)

const MaxNameLength = 50

// NewKeyFromName creates a valid key from the name.
func NewKeyFromName(name string) (string, error) {
	if len(name) < 1 {
		return "", errors.NewError("Name must not be empty.")
	}
	if len(name) > MaxNameLength {
		return "", errors.NewError("Name must be less than 50 characters.")
	}

	invalid := regexp.MustCompile(`(?i)[^a-z0-9-._\s]+`)
	if invalidStr := invalid.FindString(name); strings.TrimSpace(invalidStr) != "" {
		return "", errors.NewError("Name must start with a letter or number and only contain letters, numbers, '.', '_' or '-'.")
	}

	capitalLettersRegexp := regexp.MustCompile("[A-Z]")
	spacesRegexp := regexp.MustCompile(`\s+`)
	dashSpaceRegexp := regexp.MustCompile(`-\s+`)

	// change capital letters to lowercase with a prepended space
	key := capitalLettersRegexp.ReplaceAllStringFunc(name, func(match string) string {
		return " " + strings.ToLower(match)
	})
	// change any "- " to "-" because the previous step added a space that could be preceded by a
	// valid dash
	key = dashSpaceRegexp.ReplaceAllString(key, " ")
	// replace all spaces with a single dash
	key = spacesRegexp.ReplaceAllString(key, "-")
	// remove a starting dash that could have been added from converting a capital letter at the
	// beginning of the string
	key = strings.TrimPrefix(key, "-")

	return key, nil
}
