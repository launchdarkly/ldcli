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
		lst = append(lst, fmt.Sprintf("%s: %v", k, r[k]))
	}

	return strings.Join(lst, "\n")
}

// ErrorPlaintextOutputFn converts the resource to plain text specifically for data from the
// error file.
// An error response could have a code and message or just a message. It's also possible that
// there isn't either property.
var ErrorPlaintextOutputFn = func(r resource) string {
	switch {
	case r["code"] == nil && (r["message"] == "" || r["message"] == nil):
		return "unknown error occurred"
	case r["code"] == nil:
		return r["message"].(string)
	case r["message"] == "":
		return fmt.Sprintf("an error occurred (code: %s)", r["code"])
	default:
		return fmt.Sprintf("%s (code: %s)", r["message"], r["code"])
	}
}

// MultiplePlaintextOutputFn converts the resource to plain text.
var MultiplePlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s", SingularPlaintextOutputFn(r))
}

// SingularPlaintextOutputFn converts the resource to plain text based on its name and key.
var SingularPlaintextOutputFn = func(r resource) string {
	email := r["email"]
	id := r["_id"]
	key := r["key"]
	name := r["name"]

	switch {
	case name != nil && key != nil:
		return fmt.Sprintf("%s (%s)", name.(string), key.(string))
	case email != nil && id != nil:
		return fmt.Sprintf("%s (%s)", email.(string), id.(string))
	case name != nil && id != nil:
		return fmt.Sprintf("%s (%s)", name.(string), id.(string))
	case key != nil:
		return key.(string)
	case email != nil:
		return email.(string)
	case id != nil:
		return id.(string)
	case name != nil:
		return name.(string)
	default:
		return "cannot read resource"
	}
}
