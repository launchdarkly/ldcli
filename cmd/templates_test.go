package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The root usage listing is hand-maintained, so a command added to or removed from
// the tree does not update it. The symbols entry was dropped from both at once.
func TestGetUsageTemplate_ListsTopLevelCommands(t *testing.T) {
	template := getUsageTemplate()

	for _, name := range []string{
		"setup",
		"quickstart",
		"config",
		"completion",
		"login",
		"signup",
		"dev-server",
		"flags",
		"environments",
		"projects",
		"members",
		"segments",
		"sourcemaps",
		"symbols",
	} {
		assert.Contains(t, template, `"`+name+`"`, "%s is missing from the root usage listing", name)
	}
}
