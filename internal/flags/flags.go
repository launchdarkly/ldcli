package flags

import (
	"ldcli/internal/errors"
	"regexp"
	"strings"
)

const MaxNameLength = 50

func NameToKey(name string) (string, error) {
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

	key := spacesRegexp.ReplaceAllString(name, "-")
	key = capitalLettersRegexp.ReplaceAllStringFunc(key, func(match string) string {
		return "-" + strings.ToLower(match)
	})
	key = strings.ReplaceAll(key, "--", "-")
	key = strings.TrimPrefix(key, "-")

	return key, nil
}
