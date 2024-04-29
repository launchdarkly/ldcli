package output

import (
	"encoding/json"
	"fmt"
)

type ActionKind string

var (
	ActionKindCreate = ActionKind("create")
	ActionKindDelete = ActionKind("delete")
	ActionKindGet    = ActionKind("get")
	ActionKindList   = ActionKind("list")
	ActionKindUpdate = ActionKind("update")
)

type resourceOutput struct {
	input          []byte
	outputFn       PlaintextOutputFn
	resource       resource
	successMessage string
}

func (r resourceOutput) JSON() string {
	return string(r.input)
}

func (r resourceOutput) String() string {
	return fmt.Sprintf("%s %s", r.successMessage, r.plaintext())
}

func (r resourceOutput) plaintext() string {
	return r.outputFn(r.resource)
}

type resourceOutputFn func(input []byte, outputFn PlaintextOutputFn, r resource) resourceOutput

// CmdOutput returns a response from a resource create action formatted based on the
// output flag along with an optional message based on the action.
func CmdOutput(action string, outputKind string, input []byte) (string, error) {
	switch action {
	case "create":
		return cmdOutputResource(
			outputKind,
			input,
			func(input []byte, outputFn PlaintextOutputFn, r resource) resourceOutput {
				return resourceOutput{
					input:          input,
					outputFn:       SingularPlaintextOutputFn,
					resource:       r,
					successMessage: "Successfully created",
				}
			},
		)
	case "delete":
		return cmdOutputResource(
			outputKind,
			input,
			func(input []byte, outputFn PlaintextOutputFn, r resource) resourceOutput {
				return resourceOutput{
					outputFn:       SingularPlaintextOutputFn,
					resource:       r,
					successMessage: "Successfully deleted",
				}
			},
		)
	case "get":
		return cmdOutputResource(
			outputKind,
			input,
			func(input []byte, outputFn PlaintextOutputFn, r resource) resourceOutput {
				return resourceOutput{
					input:    input,
					outputFn: SingularPlaintextOutputFn,
					resource: r,
				}
			},
		)
	case "list":
		var r resources
		err := json.Unmarshal(input, &r)
		if err != nil {
			return "", err
		}

		return outputFromKind(outputKind, MultipleOutputter{
			outputFn:     MultiplePlaintextOutputFn,
			resources:    r,
			resourceJSON: input,
		})
	case "update":
		return cmdOutputResource(
			outputKind,
			input,
			func(input []byte, outputFn PlaintextOutputFn, r resource) resourceOutput {
				return resourceOutput{
					input:          input,
					outputFn:       SingularPlaintextOutputFn,
					resource:       r,
					successMessage: "Successfully updated",
				}
			},
		)
	default:
		return "", ErrInvalidActionKind
	}
}

func cmdOutputResource(outputKind string, input []byte, constructor resourceOutputFn) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	o := constructor(input, SingularPlaintextOutputFn, r)

	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
}
