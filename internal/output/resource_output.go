package output

import (
	"encoding/json"
	"fmt"
	"math"
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
		return plaintextOutput(SingularPlaintextOutputFn(maybeResource), successMessage+" "), nil
	}

	if len(maybeResources.Items) == 0 {
		return "No items found", nil
	}

	items := make([]string, 0, len(maybeResources.Items))
	for _, i := range maybeResources.Items {
		items = append(items, MultiplePlaintextOutputFn(i))
	}

	var (
		pagination string
		limit      int
		offset     int
	)
	self, ok := maybeResources.Links["self"]
	if ok && maybeResources.TotalCount > 0 {
		selfURL, _ := url.Parse(self["href"])
		limit, _ = strconv.Atoi(selfURL.Query().Get("limit"))
		offset, _ = strconv.Atoi(selfURL.Query().Get("offset"))
		maxResults := int(math.Min(float64(offset+limit), float64(maybeResources.TotalCount)))
		pagination = fmt.Sprintf(
			"\nShowing results %d - %d of %d.",
			offset+1,
			maxResults,
			maybeResources.TotalCount,
		)
		if offset+limit < maybeResources.TotalCount {
			pagination += fmt.Sprintf(" Use --offset %d for additional results.", offset+limit)
		}
	}

	if successMessage != "" {
		successMessage += "\n"
	}
	return plaintextOutput(strings.Join(items, "\n"), successMessage) + pagination, nil
}

func plaintextOutput(out string, successMessage string) string {
	if successMessage != "" {
		return fmt.Sprintf("%s%s", successMessage, out)
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
