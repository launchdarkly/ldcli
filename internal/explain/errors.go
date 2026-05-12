package explain

import "errors"

// ErrCommandNotFound is returned by Registry.Resolve when no explainer can
// produce a result for the given command path. Callers should map this to a
// user-facing "no explanation available for this command" message.
var ErrCommandNotFound = errors.New("command not found")
