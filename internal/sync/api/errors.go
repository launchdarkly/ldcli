package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	lderrors "github.com/launchdarkly/ldcli/internal/errors"
)

// IsNotFound reports whether a LaunchDarkly API error has a 404 status.
func IsNotFound(err error) bool {
	status, ok := responseStatusCode(err)
	return ok && status == http.StatusNotFound
}

// IsConflict reports whether a LaunchDarkly API error has a 409 status.
func IsConflict(err error) bool {
	status, ok := responseStatusCode(err)
	return ok && status == http.StatusConflict
}

// MutationMayHaveSucceeded reports whether a write failed before the client
// received a response with a status code. The write can still have succeeded,
// so the caller must read the resource to find its state.
func MutationMayHaveSucceeded(err error) bool {
	var mutation mutationError
	return errors.As(err, &mutation) && mutation.uncertain
}

type mutationError struct {
	err       error
	uncertain bool
}

func (err mutationError) Error() string { return err.err.Error() }
func (err mutationError) Unwrap() error { return err.err }

// newMutationError adds the write description to err. It also records whether
// the API sent a status code, which is a definite result.
func newMutationError(action, resource, key string, err error) error {
	_, definite := responseStatusCode(err)
	return mutationError{
		err:       contextualAPIError(err, fmt.Sprintf("%s %s %q", action, resource, key), ""),
		uncertain: !definite,
	}
}

// contextualAPIError adds a description to an API error. It keeps the
// structured fields of the response. If the resource is not found in
// projectKey, the error suggests a fix.
func contextualAPIError(err error, description, projectKey string) error {
	response, ok := responseError(err)
	if !ok {
		return fmt.Errorf("%s: %w", description, err)
	}

	status, _ := response["statusCode"].(float64)
	message, _ := response["message"].(string)
	switch {
	case int(status) == http.StatusNotFound && projectKey != "":
		response["message"] = description
		response["suggestion"] = fmt.Sprintf("Verify the resource key and that it belongs to project %q.", projectKey)
	case message != "":
		response["message"] = description + ": " + strings.ReplaceAll(message, "AI config", "config")
	default:
		response["message"] = description
	}
	body, _ := json.Marshal(response)
	return lderrors.NewErrorWrapped(string(body), err)
}

func responseStatusCode(err error) (int, bool) {
	response, ok := responseError(err)
	if !ok {
		return 0, false
	}
	status, ok := response["statusCode"].(float64)
	return int(status), ok && status != 0
}

// responseError finds the JSON response body in an error chain. The resources
// client reports an API failure as an error whose text is the response body.
func responseError(err error) (map[string]any, bool) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		var response map[string]any
		if json.Unmarshal([]byte(current.Error()), &response) == nil && len(response) != 0 {
			return response, true
		}
	}
	return nil, false
}
