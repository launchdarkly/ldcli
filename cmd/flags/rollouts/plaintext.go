package rollouts

import (
	"fmt"
	"strings"
	"time"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// RenderRolloutListPlaintext is the Phase 1 placeholder plaintext renderer. Plan 03 replaces
// this with a properly aligned 5-column table (`ID | kind | environment | state/label |
// started`). For Phase 1 we emit either the "no rollouts" sentinel or a tab-separated
// per-item line, which is enough to prove the plumbing without committing to a final layout.
//
// `detailed` is accepted for forward-compatibility with Plan 03 but is currently ignored.
func RenderRolloutListPlaintext(list *rollouts.RolloutList, _ bool) string {
	if list == nil || len(list.Items) == 0 {
		return "No rollouts found.\n"
	}

	var b strings.Builder
	for _, item := range list.Items {
		started := ""
		if item.StartedAt != nil {
			started = item.StartedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			item.ID,
			item.Kind,
			item.EnvironmentKey,
			item.Status.Kind,
			started,
		)
	}
	return b.String()
}
