package awsdevopsagent

import "github.com/launchdarkly/ldcli/internal/awsdevops"

func realisticStatus() awsdevops.Status {
	return awsdevops.Status{
		AccountID: "123456789012",
		Region:    "us-east-1",
		Roles: []awsdevops.RoleStatus{
			{Name: awsdevops.AgentSpaceRoleName, ARN: "arn:aws:iam::123456789012:role/" + awsdevops.AgentSpaceRoleName, Exists: true},
			{Name: awsdevops.OperatorAppRoleName, ARN: "arn:aws:iam::123456789012:role/" + awsdevops.OperatorAppRoleName, Exists: true},
		},
		Services: []awsdevops.ServiceStatus{
			{ServiceID: "s-ld01", ServiceType: "mcpserver", Name: "LaunchDarkly"},
		},
		AgentSpaces: []awsdevops.AgentSpaceStatus{{
			AgentSpaceID:       "as-prod01",
			Name:               "Production",
			OperatorAppURL:     awsdevops.OperatorAppURL("as-prod01"),
			OperatorAppEnabled: true,
			Associations: []awsdevops.AssociationStatus{
				{AssociationID: "a-001", ServiceID: "aws", Status: "valid", AccountID: "123456789012", AccountType: "monitor"},
				{AssociationID: "a-002", ServiceID: "s-ld01", Status: "valid"},
			},
		}},
	}
}

// partialStatus is a setup that did not finish. It sets the fields that
// realisticStatus leaves empty, so the JSON test covers them.
func partialStatus() awsdevops.Status {
	return awsdevops.Status{
		AccountID: "123456789012",
		Region:    "us-east-1",
		Roles: []awsdevops.RoleStatus{
			{Name: awsdevops.AgentSpaceRoleName, ARN: "arn:aws:iam::123456789012:role/" + awsdevops.AgentSpaceRoleName, Exists: true},
			{Name: awsdevops.OperatorAppRoleName},
		},
		AgentSpaces: []awsdevops.AgentSpaceStatus{{
			AgentSpaceID:     "as-prod01",
			Name:             "Production",
			OperatorAppError: "AccessDeniedException",
		}},
		MissingSetup: []string{"IAM role " + awsdevops.OperatorAppRoleName + " does not exist"},
	}
}
