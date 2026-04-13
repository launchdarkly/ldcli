package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	errs "github.com/launchdarkly/ldcli/internal/errors"
)

// CmdOutputOpts configures optional behavior for CmdOutput.
type CmdOutputOpts struct {
	Fields       []string
	ResourceName string
}

// CmdOutput returns a response from a resource action formatted based on the output flag along with
// an optional message based on the action. When fields is non-empty and outputKind is "json",
// only the specified top-level fields are included in the output. When resourceName matches a
// registered resource, list output uses table formatting and singular output uses key-value pairs.
func CmdOutput(action string, outputKind string, input []byte, fields []string, opts ...CmdOutputOpts) (string, error) {
	var resourceName string
	if len(opts) > 0 {
		resourceName = opts[0].ResourceName
		if len(fields) == 0 {
			fields = opts[0].Fields
		}
	}

	if outputKind == "json" {
		if len(fields) > 0 {
			filtered, err := filterFields(input, fields)
			if err != nil {
				return string(input), nil
			}
			return string(filtered), nil
		}
		return string(input), nil
	}

	var (
		maybeResource      resource
		maybeResources     resources
		maybeResourcesList resourcesList
		isMultipleResponse bool
	)

	// unmarshal singular resource, or a list of resources, or a list of scalar values
	err := json.Unmarshal(input, &maybeResource)
	_, isMultipleResponse = maybeResource["items"]
	if err != nil || isMultipleResponse {
		err := json.Unmarshal(input, &maybeResources)
		if err != nil {
			err := json.Unmarshal(input, &maybeResourcesList)
			if err != nil {
				return "", err
			}
			maybeResources.Items = make([]resource, 0, len(maybeResources.Items))
			maybeResources.TotalCount = maybeResourcesList.TotalCount
			for _, i := range maybeResourcesList.Items {
				maybeResources.Items = append(maybeResources.Items, resource{
					"key": i,
				})
			}
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
		if cols := GetSingularColumns(resourceName); cols != nil {
			kv := KeyValueOutput(maybeResource, cols)
			if strings.TrimSpace(successMessage) != "" {
				return successMessage + "\n\n" + kv, nil
			}
			return kv, nil
		}
		return plaintextOutput(SingularPlaintextOutputFn(maybeResource), successMessage+" "), nil
	}

	if len(maybeResources.Items) == 0 {
		return "No items found", nil
	}

	var body string
	if cols := GetListColumns(resourceName); cols != nil {
		body = TableOutput(maybeResources.Items, cols)
	} else {
		items := make([]string, 0, len(maybeResources.Items))
		for _, i := range maybeResources.Items {
			items = append(items, MultiplePlaintextOutputFn(i))
		}
		body = strings.Join(items, "\n")
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
		if maxResults == 0 {
			maxResults = maybeResources.TotalCount
		}
		pagination = fmt.Sprintf(
			"\nShowing results %d - %d of %d.",
			offset+1,
			maxResults,
			maybeResources.TotalCount,
		)
		if maxResults < maybeResources.TotalCount {
			pagination += fmt.Sprintf(" Use --offset %d for additional results.", offset+limit)
		}
	}

	if successMessage != "" {
		successMessage += "\n"
	}
	return plaintextOutput(body, successMessage) + pagination, nil
}

func plaintextOutput(out string, successMessage string) string {
	if strings.TrimSpace(successMessage) != "" {
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

// NewCmdOutputError builds error output based on the error and output kind.
func NewCmdOutputError(err error, outputKind string) error {
	return errs.NewError(CmdOutputError(outputKind, err))
}

func filterFields(input []byte, fields []string) ([]byte, error) {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			fieldSet[trimmed] = true
		}
	}
	if len(fieldSet) == 0 {
		return input, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, err
	}

	if items, ok := raw["items"]; ok {
		if itemList, ok := items.([]interface{}); ok {
			filtered := make([]interface{}, 0, len(itemList))
			for _, item := range itemList {
				if m, ok := item.(map[string]interface{}); ok {
					filtered = append(filtered, filterMap(m, fieldSet))
				} else {
					filtered = append(filtered, item)
				}
			}
			result := map[string]interface{}{"items": filtered}
			if tc, ok := raw["totalCount"]; ok {
				result["totalCount"] = tc
			}
			if links, ok := raw["_links"]; ok {
				result["_links"] = links
			}
			return json.MarshalIndent(result, "", "  ")
		}
	}

	return json.MarshalIndent(filterMap(raw, fieldSet), "", "  ")
}

func filterMap(m map[string]interface{}, fields map[string]bool) map[string]interface{} {
	result := make(map[string]interface{}, len(fields))
	for k, v := range m {
		if fields[k] {
			result[k] = v
		}
	}
	return result
}

func errJSON(s string) string {
	return fmt.Sprintf(
		`{
			"message": %q
		}`,
		s,
	)
}
