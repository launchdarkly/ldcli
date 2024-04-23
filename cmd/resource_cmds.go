// this file WILL be generated (sc-241153)

package cmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"ldcli/cmd/resources"
)

func addAllResourceCmds(rootCmd *cobra.Command, client *http.Client) {
	// Resource commands
	gen_TeamsResourceCmd := resources.NewResourceCmd(rootCmd, "teams", "Teams is an Enterprise feature", "Teams is available to customers on an Enterprise plan. To learn more, [read about our pricing](https://launchdarkly.com/pricing/). To upgrade your plan, [contact Sales](https://launchdarkly.com/contact-sales/).\\n\\nA team is a group of members in your LaunchDarkly account. A team can have maintainers who are able to add and remove team members. It also can have custom roles assigned to it that allows shared access to those roles for all team members. To learn more, read [Teams](https://docs.launchdarkly.com/home/teams).\\n\\nThe Teams API allows you to create, read, update, and delete a team.\\n\\nSeveral of the endpoints in the Teams API require one or more member IDs. The member ID is returned as part of the [List account members](/tag/Account-members#operation/getMembers) response. It is the `_id` field of each element in the `items` array.\\n\"")

	// Operation commands
	resources.NewOperationCmd(gen_TeamsResourceCmd, client, resources.OperationData{
		Short: "Create team",
		Long:  "Create a team. To learn more, read [Creating a team](https://docs.launchdarkly.com/home/teams/creating).\n\n### Expanding the teams response\nLaunchDarkly supports four fields for expanding the \"Create team\" response. By default, these fields are **not** included in the response.\n\nTo expand the response, append the `expand` query parameter and add a comma-separated list with any of the following fields:\n\n* `members` includes the total count of members that belong to the team.\n* `roles` includes a paginated list of the custom roles that you have assigned to the team.\n* `projects` includes a paginated list of the projects that the team has any write access to.\n* `maintainers` includes a paginated list of the maintainers that you have assigned to the team.\n\nFor example, `expand=members,roles` includes the `members` and `roles` fields in the response.\n",
		Use:   "create",
		Params: []resources.Param{
			{
				Name:        "expand",
				In:          "query",
				Description: "A comma-separated list of properties that can reveal additional information in the response. Supported fields are explained above.",
				Type:        "string",
			},
		},
		HTTPMethod:   "post",
		RequiresBody: true,
		Path:         "/api/v2/teams",
	})

	resources.NewOperationCmd(gen_TeamsResourceCmd, client, resources.OperationData{
		Short: "Get team",
		Long:  "Fetch a team by key.\n\n### Expanding the teams response\nLaunchDarkly supports four fields for expanding the \"Get team\" response. By default, these fields are **not** included in the response.\n\nTo expand the response, append the `expand` query parameter and add a comma-separated list with any of the following fields:\n\n* `members` includes the total count of members that belong to the team.\n* `roles` includes a paginated list of the custom roles that you have assigned to the team.\n* `projects` includes a paginated list of the projects that the team has any write access to.\n* `maintainers` includes a paginated list of the maintainers that you have assigned to the team.\n\nFor example, `expand=members,roles` includes the `members` and `roles` fields in the response.\n",
		Use:   "get",
		Params: []resources.Param{
			{
				Name:        "teamKey",
				In:          "path",
				Description: "The team key.",
				Type:        "string",
			},
			{
				Name:        "expand",
				In:          "query",
				Description: "A comma-separated list of properties that can reveal additional information in the response. Supported fields are explained above.",
				Type:        "string",
			},
		},
		HTTPMethod:   "get",
		RequiresBody: false,
		Path:         "/api/v2/teams/{teamKey}",
	})
}
