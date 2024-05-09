package output

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	errs "ldcli/internal/errors"
)

// CmdOutput returns a response from a resource action formatted based on the output flag along with
// an optional message based on the action.
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

	if !isMultipleResponse {
		return plaintextOutput(SingularPlaintextOutputFn(maybeResource), successMessage), nil
	}

	if len(maybeResources.Items) == 0 {
		return "No items found", nil
	}

	// the response could have various properties we want to show
	keyExists := func(key string) bool { _, ok := maybeResources.Items[0][key]; return ok }
	outputFn := MultiplePlaintextOutputFn
	switch {
	case keyExists("email"):
		outputFn = MultipleEmailPlaintextOutputFn
	case keyExists("_id"):
		outputFn = MultipleIDPlaintextOutputFn
	}

	items := make([]string, 0, len(maybeResources.Items))
	for _, i := range maybeResources.Items {
		items = append(items, outputFn(i))
	}

	// fmt.Println(">>> checking", maybeResources.TotalCount)
	// spew.Dump(maybeResources.Links)
	var (
		pagination string
		limit      int
		offset     int
	)
	if self, ok := maybeResources.Links["self"]; ok {
		selfURL, _ := url.Parse(self["href"])
		limit, _ = strconv.Atoi(selfURL.Query().Get("limit"))
		offset, _ = strconv.Atoi(selfURL.Query().Get("offset"))
		fmt.Println(">>> found", limit, offset)
	}

	pagination = fmt.Sprintf(
		"Showing results %d - %d of %d. Use --offset %d for additional results.",
		offset+1,
		offset+limit,
		maybeResources.TotalCount,
		offset+limit,
	)

	return plaintextOutput("\n"+strings.Join(items, "\n"), successMessage) + "\n" + pagination, nil
}

func plaintextOutput(out string, successMessage string) string {
	if successMessage != "" {
		return fmt.Sprintf("%s %s", successMessage, out)
	}

	return out
}

// CmdOutputError returns a response from a resource action error.
func CmdOutputError(outputKind string, err error) string {
	var output string
	jsonErr := &json.UnmarshalTypeError{}
	switch {
	case errors.As(err, &jsonErr):
		output = errJSON("invalid JSON")
	case errors.As(err, &errs.Error{}):
		output = err.Error()
	default:
		output = errJSON(err.Error())
	}

	var r resource
	_ = json.Unmarshal([]byte(output), &r)

	if outputKind == "json" {
		// convert to a well-formatted output
		formattedOutput, _ := json.Marshal(r)

		return string(formattedOutput)
	}

	return ErrorPlaintextOutputFn(r)
}

func errJSON(s string) string {
	return fmt.Sprintf(
		`{
			"message": %q
		}`,
		s,
	)
}
