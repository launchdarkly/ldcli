package output

import (
	"encoding/json"
	"fmt"
)

type resourceOutputDelete struct {
	outputFn PlaintextOutputFn
	resource resource
}
type resourceOutputCreate struct {
	input    []byte
	outputFn PlaintextOutputFn
	resource resource
}
type resourceOutputUpdate struct {
	input    []byte
	outputFn PlaintextOutputFn
	resource resource
}

func (r resourceOutputCreate) JSON() string {
	return string(r.input)
}
func (r resourceOutputDelete) JSON() string {
	return ""
}
func (r resourceOutputUpdate) JSON() string {
	return string(r.input)
}

func (r resourceOutputCreate) String() string {
	return fmt.Sprintf("%s %s", r.successMessage(), r.plaintext())
}
func (r resourceOutputDelete) String() string {
	return fmt.Sprintf("%s %s", r.successMessage(), r.plaintext())
}
func (r resourceOutputUpdate) String() string {
	return fmt.Sprintf("%s %s", r.successMessage(), r.plaintext())
}

func (r resourceOutputCreate) successMessage() string {
	return "Successfully created"
}
func (r resourceOutputDelete) successMessage() string {
	return "Successfully deleted"
}
func (r resourceOutputUpdate) successMessage() string {
	return "Successfully updated"
}

func (r resourceOutputCreate) plaintext() string {
	return r.outputFn(r.resource)
}
func (r resourceOutputDelete) plaintext() string {
	return r.outputFn(r.resource)
}
func (r resourceOutputUpdate) plaintext() string {
	return r.outputFn(r.resource)
}

func CmdOutputCreateResource(outputKind string, input []byte) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	o := resourceOutputCreate{
		input:    input,
		outputFn: SingularPlaintextOutputFn,
		resource: r,
	}

	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
}

func CmdOutputDeleteResource(outputKind string, input []byte) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	o := resourceOutputDelete{
		outputFn: SingularPlaintextOutputFn,
		resource: r,
	}

	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
}

func CmdOutputUpdateResource(outputKind string, input []byte) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	o := resourceOutputUpdate{
		input:    input,
		outputFn: SingularPlaintextOutputFn,
		resource: r,
	}

	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
}
