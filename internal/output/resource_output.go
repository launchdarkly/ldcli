package output

import (
	"encoding/json"
	"fmt"
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

// CmdOutputCreateResource returns a response from a resource create action formatted based on the
// output flag.
func CmdOutputCreateResource(outputKind string, input []byte) (string, error) {
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
}

// CmdOutputDeleteResource returns a response from a resource delete action formatted based on the
// output flag.
func CmdOutputDeleteResource(outputKind string, input []byte) (string, error) {
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
}

// CmdOutputUpdateResource returns a response from a resource update action formatted based on the
// output flag.
func CmdOutputUpdateResource(outputKind string, input []byte) (string, error) {
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
