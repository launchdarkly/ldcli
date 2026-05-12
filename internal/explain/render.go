package explain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderJSON serializes the explanation as pretty-printed JSON. This is the
// canonical machine-readable format and the one agents should consume.
func RenderJSON(e CommandExplanation) (string, error) {
	bs, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bs), nil
}

// RenderMarkdown produces a compact markdown view of the explanation. It's
// intended for human reviewers on the terminal; agents should prefer JSON.
//
// The output is deliberately terse: headers, bullet lists, no fancy tables.
// This keeps it readable when piped through `less` or rendered inline by an
// agent that strips markdown.
func RenderMarkdown(e CommandExplanation) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", e.Command)
	if e.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", e.Summary)
	}
	if e.HTTPMethod != "" || e.Path != "" {
		fmt.Fprintf(&b, "`%s %s`", e.HTTPMethod, e.Path)
		if e.OperationID != "" {
			fmt.Fprintf(&b, " (operationId: `%s`)", e.OperationID)
		}
		b.WriteString("\n\n")
	}
	if e.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", e.Description)
	}

	if len(e.Inputs) > 0 {
		b.WriteString("## Inputs\n\n")
		writeInputs(&b, e.Inputs, 0)
		b.WriteString("\n")
	}

	b.WriteString("## Output\n\n")
	fmt.Fprintf(&b, "- format: `%s`\n", e.Output.Format)
	if e.Output.Type != "" {
		fmt.Fprintf(&b, "- type: `%s`\n", e.Output.Type)
	}
	if e.Output.Description != "" {
		fmt.Fprintf(&b, "- %s\n", e.Output.Description)
	}
	if e.Output.Pagination != nil {
		fmt.Fprintf(&b, "- pagination: %s — %s\n", e.Output.Pagination.Style, e.Output.Pagination.Description)
	}
	if len(e.Output.Fields) > 0 {
		b.WriteString("\nFields:\n")
		writeInputs(&b, e.Output.Fields, 0)
	}
	b.WriteString("\n")

	if len(e.Errors) > 0 {
		b.WriteString("## Errors\n\n")
		for _, er := range e.Errors {
			fmt.Fprintf(&b, "- **%s** (HTTP %d): %s", er.Code, er.HTTPStatus, er.Description)
			if er.Remediation != "" {
				fmt.Fprintf(&b, " _Remediation: %s_", er.Remediation)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(e.Examples) > 0 {
		b.WriteString("## Examples\n\n")
		for i, ex := range e.Examples {
			fmt.Fprintf(&b, "### %d. %s\n\n", i+1, ex.Title)
			if ex.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", ex.Description)
			}
			fmt.Fprintf(&b, "```sh\nldcli %s\n```\n", strings.Join(ex.Args, " "))
			if ex.Body != "" {
				fmt.Fprintf(&b, "\nBody:\n\n```json\n%s\n```\n", strings.TrimSpace(ex.Body))
			}
			if ex.Result != "" {
				fmt.Fprintf(&b, "\n_Result: %s_\n", ex.Result)
			}
			b.WriteString("\n")
		}
	}

	if len(e.AgentNotes) > 0 {
		b.WriteString("## Agent notes\n\n")
		for _, n := range e.AgentNotes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		b.WriteString("\n")
	}

	if len(e.SeeAlso) > 0 {
		b.WriteString("## See also\n\n")
		for _, s := range e.SeeAlso {
			fmt.Fprintf(&b, "- `%s`\n", s)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func writeInputs(b *strings.Builder, inputs []InputSpec, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, in := range inputs {
		req := ""
		if in.Required {
			req = " (required)"
		}
		fmt.Fprintf(b, "%s- `%s`: `%s`%s", indent, in.Name, in.Type, req)
		if in.Description != "" {
			fmt.Fprintf(b, " — %s", in.Description)
		}
		if in.Default != nil {
			fmt.Fprintf(b, " _(default: %v)_", in.Default)
		}
		if len(in.Enum) > 0 {
			fmt.Fprintf(b, " _(one of: %s)_", strings.Join(in.Enum, ", "))
		}
		b.WriteString("\n")
		if len(in.Fields) > 0 {
			writeInputs(b, in.Fields, depth+1)
		}
		if len(in.OneOf) > 0 {
			fmt.Fprintf(b, "%s  oneOf:\n", indent)
			writeInputs(b, in.OneOf, depth+2)
		}
	}
}
