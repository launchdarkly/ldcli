package output

import (
	"fmt"
	"sort"
	"strings"
)

// ConfigPlaintextOutputFn converts the resource to plain text specifically for data from the
// config file.
var ConfigPlaintextOutputFn = func(r resource) string {
	keys := make([]string, 0)
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lst := make([]string, 0)
	for _, k := range keys {
		lst = append(lst, fmt.Sprintf("%s: %s", k, r[k]))
	}

	return strings.Join(lst, "\n")
}

// ErrorPlaintextOutputFn converts the resource to plain text specifically for data from the
// error file.
var ErrorPlaintextOutputFn = func(r resource) string {
	switch {
	case r["code"] == nil && r["message"] == "":
		return "unknown error occurred"
	case r["code"] == nil:
		return r["message"].(string)
	case r["message"] == "":
		return fmt.Sprintf("an error occurred (code: %s)", r["code"])
	default:
		return fmt.Sprintf("%s (code: %s)", r["message"], r["code"])
	}
}

// MultiplePlaintextOutputFn converts the resource to plain text based on its name and key in a list.
var MultiplePlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s (%s)", r["name"], r["key"])
}

// SingularPlaintextOutputFn converts the resource to plain text based on its name and key.
var SingularPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("%s (%s)", r["name"], r["key"])
}

// ConfigPlaintextOutputFn converts the resource to plain text specifically for member data.
var MultipleEmailPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s (%s)", r["email"], r["_id"])
}
