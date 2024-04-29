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

	// unmarshal either a singular resource or a list of them
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
		// the response could have various properties we want to show
		outputFn := MultiplePlaintextOutputFn
		if _, ok := maybeResources.Items[0]["email"]; ok {
			outputFn = MultipleEmailPlaintextOutputFn
		}

		items := make([]string, 0, len(maybeResources.Items))
		for _, i := range maybeResources.Items {
			items = append(items, outputFn(i))
		}

		return plaintextOutput("\n"+strings.Join(items, "\n"), successMessage), nil
	}

	return plaintextOutput(SingularPlaintextOutputFn(maybeResource), successMessage), nil
}

func plaintextOutput(out string, successMessage string) string {
	if successMessage != "" {
		return fmt.Sprintf("%s %s", successMessage, out)
	}

	return out
}
