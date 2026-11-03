// Package all registers every shipped source format. A host imports it for
// its side effect. To hold a format back from a release, remove its import.
package all

import (
	_ "github.com/launchdarkly/ldcli/internal/sync/adapter/ecmascript"
	_ "github.com/launchdarkly/ldcli/internal/sync/adapter/markdown"
	_ "github.com/launchdarkly/ldcli/internal/sync/adapter/python"
	_ "github.com/launchdarkly/ldcli/internal/sync/adapter/structured"
)
