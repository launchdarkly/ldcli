package output

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CmdOutput returns a response from a resource create action formatted based on the
// output flag along with an optional message based on the action.
func CmdOutput(action string, outputKind string, input []byte) (string, error) {
	if outputKind == "json" {
		return string(input), nil
	}

	var (
		maybeResource      resource
		maybeResources     resources
		isMultipleResponse bool
	)

	err := json.Unmarshal(input, &maybeResource)
	_, isMultipleResponse = maybeResource["items"]
	if err != nil || isMultipleResponse {
		err := json.Unmarshal(input, &maybeResources)
		if err != nil {
			return "", err
		}
	}

	var successMessage string
	switch action {
	case "create":
		successMessage = "Successfully created"
	case "delete":
		successMessage = "Successfully deleted"
	case "update":
		successMessage = "Successfully updated"
	default:
		// no success message
	}

	if isMultipleResponse {
		outputFn := MultiplePlaintextOutputFn
		if _, ok := maybeResources.Items[0]["email"]; ok {
			outputFn = MultipleEmailPlaintextOutputFn
		}

		items := make([]string, 0, len(maybeResources.Items))
		for _, i := range maybeResources.Items {
			items = append(items, outputFn(i))
		}

		if successMessage != "" {
			return fmt.Sprintf("%s %s", successMessage, strings.Join(items, "\n")), nil
		}

		return strings.Join(items, "\n"), nil
	}

	if successMessage != "" {
		return fmt.Sprintf("%s %s", successMessage, SingularPlaintextOutputFn(maybeResource)), nil
	}

	return SingularPlaintextOutputFn(maybeResource), nil
}
