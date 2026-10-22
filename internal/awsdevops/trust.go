package awsdevops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// newSpaceID stands in for the ID of a space that the plan creates. A trust
// policy that names one space by its ID cannot match it.
const newSpaceID = "new"

// checkTrust stops the plan if the trust policy of an unmanaged role does not
// let the agent space assume it. Setup does not change that role, so a run
// would make an agent space and then fail to associate the account with it.
func checkTrust(ctx context.Context, client IAMAPI, name, region, accountID, spaceID string, actions []string) error {
	role, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
	if err != nil {
		return fmt.Errorf("unable to read the %s role: %w", name, err)
	}
	if role.Role == nil || role.Role.AssumeRolePolicyDocument == nil {
		return nil
	}
	if spaceID == "" {
		spaceID = newSpaceID
	}
	sourceARN := fmt.Sprintf("arn:aws:aidevops:%s:%s:agentspace/%s", region, accountID, spaceID)
	if trusts(aws.ToString(role.Role.AssumeRolePolicyDocument), sourceARN, actions) {
		return nil
	}

	return fmt.Errorf(
		"the trust policy of role %s does not let agent spaces in %s assume it, so setup did not change AWS. "+
			"Let aidevops.amazonaws.com call %s with aws:SourceArn arn:aws:aidevops:*:%s:agentspace/*, "+
			"or add the tag %s=%s to the role so that setup updates it",
		name, region, strings.Join(actions, " and "), accountID, ManagedTagKey, ManagedTagValue,
	)
}

type policyDocument struct {
	Statement oneOrMany[policyStatement]
}

type policyStatement struct {
	Effect       string
	Principal    json.RawMessage
	NotPrincipal json.RawMessage
	Action       oneOrMany[string]
	NotAction    json.RawMessage
	Condition    map[string]map[string]oneOrMany[string]
}

// oneOrMany reads a policy value that is one item or a list of items.
type oneOrMany[T any] []T

func (o *oneOrMany[T]) UnmarshalJSON(data []byte) error {
	var many []T
	if err := json.Unmarshal(data, &many); err == nil {
		*o = many
		return nil
	}
	var one T
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*o = []T{one}

	return nil
}

// trusts reports whether the trust policy lets aidevops.amazonaws.com call
// each action for sourceARN. It returns true for a policy it cannot read, so
// only a policy that clearly blocks the agent stops the plan.
func trusts(document, sourceARN string, actions []string) bool {
	if decoded, err := url.QueryUnescape(document); err == nil {
		// GetRole sends the document URL-encoded.
		document = decoded
	}
	var policy policyDocument
	if err := json.Unmarshal([]byte(document), &policy); err != nil {
		return true
	}

	allowed := map[string]bool{}
	for _, statement := range policy.Statement {
		if !strings.EqualFold(statement.Effect, "Allow") {
			continue
		}
		if statement.NotPrincipal != nil || statement.NotAction != nil {
			return true
		}
		if !trustsAgent(statement.Principal) || !sourceARNMatches(statement.Condition, sourceARN) {
			continue
		}
		for _, action := range actions {
			if actionMatches(statement.Action, action) {
				allowed[action] = true
			}
		}
	}
	for _, action := range actions {
		if !allowed[action] {
			return false
		}
	}

	return true
}

func trustsAgent(raw json.RawMessage) bool {
	var everyone string
	if json.Unmarshal(raw, &everyone) == nil {
		return everyone == "*"
	}
	var principal struct {
		Service oneOrMany[string]
		AWS     oneOrMany[string]
	}
	if json.Unmarshal(raw, &principal) != nil {
		return false
	}
	return slices.Contains(principal.Service, "aidevops.amazonaws.com") || slices.Contains(principal.AWS, "*")
}

func actionMatches(patterns []string, action string) bool {
	for _, pattern := range patterns {
		if globMatch(strings.ToLower(pattern), strings.ToLower(action)) {
			return true
		}
	}

	return false
}

// sourceARNMatches reports whether each aws:SourceArn condition matches arn.
// It ignores the other condition keys, and it counts an operator that it does
// not know as a match.
func sourceARNMatches(conditions map[string]map[string]oneOrMany[string], arn string) bool {
	for operator, keys := range conditions {
		for key, values := range keys {
			if !strings.EqualFold(key, "aws:SourceArn") {
				continue
			}
			match, known := conditionMatches(operator, values, arn)
			if known && !match {
				return false
			}
		}
	}

	return true
}

func conditionMatches(operator string, values []string, arn string) (match, known bool) {
	op := strings.TrimSuffix(strings.TrimPrefix(operator, "ForAnyValue:"), "IfExists")
	var like bool
	switch op {
	case "ArnLike", "StringLike":
		like = true
	case "ArnEquals", "StringEquals":
	default:
		return false, false
	}
	for _, value := range values {
		if value == arn || (like && globMatch(value, arn)) {
			return true, true
		}
	}

	return false, true
}

// globMatch matches IAM wildcards: * matches any characters, ? matches one.
func globMatch(pattern, value string) bool {
	expr := regexp.QuoteMeta(pattern)
	expr = strings.ReplaceAll(expr, `\*`, ".*")
	expr = strings.ReplaceAll(expr, `\?`, ".")

	return regexp.MustCompile("^" + expr + "$").MatchString(value)
}
