package explain

// flagsUpdateExplainer is the curated explainer for `ldcli flags update`.
//
// This is the highest-value command for agents: the auto-generated CLI accepts
// a free-form JSON body via --data, and the only way to discover the legal
// shape of that body is to read the (very long) Long description, which is
// itself embedded as a single line of escaped JSON in the generated
// resource_cmds.go. Bench data (T06 on claude-cli-md) showed agents spending
// 10–18 tool calls to converge on a valid payload.
//
// The shape below is hand-curated. Each `kind` of instruction matches the
// LaunchDarkly REST API semantic-patch contract documented at
// https://launchdarkly.com/docs/api/feature-flags/patch-feature-flag. When the
// OpenAPIExplainer fallback lands (see docs/agent-explain.md, "Path to full
// coverage"), this file should shrink to just the curated examples — the
// instruction catalog can be auto-derived from the OpenAPI Long description.
type flagsUpdateExplainer struct{}

func (flagsUpdateExplainer) Explain(_ []string) (CommandExplanation, error) {
	return CommandExplanation{
		Command:     "ldcli flags update",
		Summary:     "Update a feature flag using semantic patch, JSON patch, or JSON merge patch.",
		Description: "Apply a partial update to a feature flag. Semantic patch is the recommended format for agents because it expresses intent (e.g. \"add a targeting rule\") rather than raw JSON pointers. Append `--semantic-patch` to switch the request Content-Type.",
		Stability:   "stable",
		HTTPMethod:  "PATCH",
		Path:        "/api/v2/flags/{projectKey}/{featureFlagKey}",
		OperationID: "patchFeatureFlag",
		Inputs: []InputSpec{
			{Name: "project-key", Location: "flag", Type: "string", Required: true, Description: "The project key."},
			{Name: "feature-flag-key", Location: "flag", Type: "string", Required: true, Description: "The feature flag key."},
			{Name: "semantic-patch", Location: "flag", Type: "boolean", Default: false, Description: "Send the body as a semantic patch (recommended for agents)."},
			{Name: "ignore-conflicts", Location: "flag", Type: "boolean", Description: "Apply the patch even if it conflicts with a pending scheduled change or approval request."},
			{Name: "dry-run", Location: "flag", Type: "boolean", Description: "Validate the patch without persisting. Returns a preview of the post-patch flag."},
			{Name: "data", Location: "flag", Type: "object", Required: true, Description: "The patch body. Shape depends on --semantic-patch; see the `body` input below.", Fields: semanticPatchBodySpec()},
		},
		Output: OutputSpec{
			Format:      "json",
			Type:        "object",
			Description: "The updated feature flag, including environment-specific targeting state.",
			Fields: []InputSpec{
				{Name: "key", Type: "string", Description: "Flag key."},
				{Name: "name", Type: "string", Description: "Human-readable flag name."},
				{Name: "kind", Type: "string", Enum: []string{"boolean", "multivariate"}},
				{Name: "_version", Type: "integer", Description: "Monotonic version, useful for optimistic locking on subsequent patches."},
				{Name: "environments", Type: "object", Description: "Map of environment key to env-specific configuration (on/off, rules, targets, fallthrough)."},
				{Name: "variations", Type: "array", Description: "Variation definitions; each element has _id, value, name."},
			},
		},
		Errors: []ErrorSpec{
			{Code: "invalid_request", HTTPStatus: 400, Description: "The patch body did not match the expected schema, or conflicts with a pending change.", Remediation: "Re-read this explain output, in particular the instruction shape; if conflict, retry with --ignore-conflicts."},
			{Code: "unauthorized", HTTPStatus: 401, Description: "Access token missing or invalid.", Remediation: "Confirm LD_ACCESS_TOKEN is set."},
			{Code: "forbidden", HTTPStatus: 403, Description: "Token lacks required role for this flag/project."},
			{Code: "not_found", HTTPStatus: 404, Description: "Project key or flag key not found."},
			{Code: "approval_required", HTTPStatus: 405, Description: "The environment requires approval before changing this flag's targeting."},
			{Code: "conflict", HTTPStatus: 409, Description: "The change would cause a pending scheduled change or approval request to fail.", Remediation: "Retry with --ignore-conflicts=true, or resolve the pending change first."},
		},
		Examples: []ExampleSpec{
			{
				Title:       "Add a percentage-rollout targeting rule",
				Description: "Semantic patch with addRule + percentage rollout. Most common write the bench harness exercises.",
				Args: []string{
					"flags", "update",
					"--project-key", "default",
					"--feature-flag-key", "new-checkout",
					"--semantic-patch",
					"--data", "@-",
				},
				Body: `{
  "environmentKey": "production",
  "comment": "Roll out new checkout to 25% of users",
  "instructions": [
    {
      "kind": "addRule",
      "clauses": [
        {"contextKind": "user", "attribute": "country", "op": "in", "negate": false, "values": ["US"]}
      ],
      "rolloutContextKind": "user",
      "rolloutWeights": {
        "2f43f67c-3e4e-4945-a18a-26559378ca00": 25000,
        "e5830889-1ec5-4b0c-9cc9-c48790090c43": 75000
      }
    }
  ]
}`,
				Result: "Returns the updated flag JSON with the new rule appended under environments.production.rules.",
			},
			{
				Title:       "Add individual targets to a variation",
				Description: "addTargets instruction; values is a list of context keys.",
				Args: []string{
					"flags", "update",
					"--project-key", "default",
					"--feature-flag-key", "new-checkout",
					"--semantic-patch",
					"--data", "@-",
				},
				Body: `{
  "environmentKey": "production",
  "instructions": [
    {
      "kind": "addTargets",
      "contextKind": "user",
      "variationId": "2f43f67c-3e4e-4945-a18a-26559378ca00",
      "values": ["alice", "bob"]
    }
  ]
}`,
			},
			{
				Title:       "Remove a targeting rule by id",
				Description: "removeRule instruction; rule id comes from `flags get` under environments.<env>.rules[]._id.",
				Args: []string{
					"flags", "update",
					"--project-key", "default",
					"--feature-flag-key", "new-checkout",
					"--semantic-patch",
					"--data", "@-",
				},
				Body: `{
  "environmentKey": "production",
  "instructions": [
    {"kind": "removeRule", "ruleId": "a902ef4a-2faf-4eaf-88e1-ecc356708a29"}
  ]
}`,
			},
			{
				Title:       "Rename the flag (cross-environment)",
				Description: "updateName does not require environmentKey; it updates a flag-level attribute.",
				Args: []string{
					"flags", "update",
					"--project-key", "default",
					"--feature-flag-key", "new-checkout",
					"--semantic-patch",
					"--data", "@-",
				},
				Body: `{
  "instructions": [
    {"kind": "updateName", "value": "Checkout v2"}
  ]
}`,
			},
		},
		AgentNotes: []string{
			"Prefer --semantic-patch over raw JSON patch: it expresses intent and produces clearer diffs in audit log.",
			"Use --dry-run first when constructing a non-trivial patch — it validates the body without mutating state.",
			"Rule ids, clause ids, and variation ids are returned by `ldcli flags get`; copy them verbatim.",
			"Instructions that mutate per-environment state (addRule, addTargets, etc.) require `environmentKey`. Flag-level instructions (updateName, addTags, archiveFlag) do not.",
		},
		SeeAlso: []string{
			"ldcli flags get",
			"ldcli flags toggle-on",
			"ldcli flags toggle-off",
		},
	}, nil
}

// semanticPatchBodySpec returns the schema for the JSON body the agent must
// pass via --data when --semantic-patch is set. The catalog of instruction
// kinds is hand-curated for now; once the OpenAPIExplainer is in place this
// should be auto-derived from the operation's request schema.
func semanticPatchBodySpec() []InputSpec {
	return []InputSpec{
		{Name: "comment", Type: "string", Description: "Free-text comment recorded with the change."},
		{Name: "environmentKey", Type: "string", Description: "Required for instructions that mutate environment-specific state (addRule, addTargets, turnFlagOn, etc.). Omit for flag-level instructions."},
		{
			Name: "instructions", Type: "array", Required: true,
			Description: "Ordered list of mutation instructions; each element is an object with a `kind` field that determines the rest of the shape.",
			Fields:      []InputSpec{instructionsItemSpec()},
		},
	}
}

func instructionsItemSpec() InputSpec {
	return InputSpec{
		Name: "[]", Type: "object",
		Description: "One semantic-patch instruction. The `kind` field selects which other fields apply.",
		Fields: []InputSpec{
			{Name: "kind", Type: "string", Required: true, Enum: instructionKinds(), Description: "Selects the instruction shape. See OneOf for per-kind parameters."},
		},
		OneOf: instructionVariants(),
	}
}

func instructionKinds() []string {
	return []string{
		"turnFlagOn", "turnFlagOff",
		"addRule", "removeRule", "reorderRules",
		"addClauses", "removeClauses", "addValuesToClause", "removeValuesFromClause", "updateClause",
		"addTargets", "removeTargets", "clearTargets", "replaceTargets",
		"addUserTargets", "removeUserTargets", "clearUserTargets", "replaceUserTargets",
		"addPrerequisite", "removePrerequisite", "updatePrerequisite", "replacePrerequisites",
		"addVariation", "removeVariation", "updateVariation",
		"updateFallthroughVariationOrRollout", "updateOffVariation", "updateRuleVariationOrRollout",
		"updateDefaultVariation", "updateRuleDescription", "updateRuleTrackEvents",
		"updateTrackEvents", "updateTrackEventsFallthrough",
		"addTags", "removeTags",
		"addCustomProperties", "removeCustomProperties", "replaceCustomProperties",
		"updateName", "updateDescription", "updateMaintainerMember", "updateMaintainerTeam", "removeMaintainer",
		"makeFlagPermanent", "makeFlagTemporary",
		"turnOnClientSideAvailability", "turnOffClientSideAvailability",
		"archiveFlag", "restoreFlag", "deprecateFlag", "restoreDeprecatedFlag", "deleteFlag",
		"replaceRules",
	}
}

// instructionVariants enumerates the per-kind shapes. Only the high-frequency
// shapes are expanded today; the rest are documented in AgentNotes as "see the
// REST API docs". A full expansion is part of the path-to-full-coverage work.
func instructionVariants() []InputSpec {
	return []InputSpec{
		{
			Name: "addRule", Type: "object",
			Description: "Append a new targeting rule. Requires environmentKey on the parent body.",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"addRule"}},
				{Name: "clauses", Type: "array", Required: true, Description: "Array of clause objects (contextKind, attribute, op, negate, values)."},
				{Name: "beforeRuleId", Type: "string", Description: "Optional: insert before this rule id rather than appending."},
				{Name: "variationId", Type: "string", Description: "Serve this variation when the rule matches. Mutually exclusive with rolloutWeights."},
				{Name: "rolloutWeights", Type: "object", Description: "Map of variationId -> weight (0-100000 thousandths of a percent). Mutually exclusive with variationId."},
				{Name: "rolloutBucketBy", Type: "string", Description: "Context attribute to bucket by."},
				{Name: "rolloutContextKind", Type: "string", Default: "user"},
			},
		},
		{
			Name: "removeRule", Type: "object",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"removeRule"}},
				{Name: "ruleId", Type: "string", Required: true, Description: "Rule id, from `flags get` under environments.<env>.rules[]._id."},
			},
		},
		{
			Name: "addTargets", Type: "object",
			Description: "Add individual context keys to a variation's targets.",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"addTargets"}},
				{Name: "variationId", Type: "string", Required: true},
				{Name: "values", Type: "array", Required: true, Description: "List of context keys (strings)."},
				{Name: "contextKind", Type: "string", Default: "user"},
			},
		},
		{
			Name: "removeTargets", Type: "object",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"removeTargets"}},
				{Name: "variationId", Type: "string", Required: true},
				{Name: "values", Type: "array", Required: true},
				{Name: "contextKind", Type: "string", Default: "user"},
			},
		},
		{
			Name: "turnFlagOn", Type: "object",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"turnFlagOn"}},
			},
		},
		{
			Name: "turnFlagOff", Type: "object",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"turnFlagOff"}},
			},
		},
		{
			Name: "updateName", Type: "object",
			Description: "Flag-level rename; do NOT pass environmentKey.",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"updateName"}},
				{Name: "value", Type: "string", Required: true},
			},
		},
		{
			Name: "addTags", Type: "object",
			Description: "Flag-level; do NOT pass environmentKey.",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"addTags"}},
				{Name: "values", Type: "array", Required: true, Description: "List of tag strings."},
			},
		},
		{
			Name: "archiveFlag", Type: "object",
			Description: "Flag-level; equivalent to `ldcli flags archive`.",
			Fields: []InputSpec{
				{Name: "kind", Type: "string", Required: true, Enum: []string{"archiveFlag"}},
			},
		},
	}
}
