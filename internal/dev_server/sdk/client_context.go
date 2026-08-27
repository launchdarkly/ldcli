package sdk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/pkg/errors"
)

// maxClientContextBytes caps how much of a POST or REPORT body is read while looking
// for the evaluation context. Real contexts are orders of magnitude smaller.
const maxClientContextBytes = 1 << 20

// contextPathVar is the mux variable holding the base64url-encoded context that GET
// variants of the client-side FDv2 endpoints carry in their path.
const contextPathVar = "context"

// ParseClientContext validates the evaluation context a client-side SDK sends with an
// FDv2 request: base64url-encoded in the path for GET, or as the raw request body for
// POST and REPORT.
//
// The dev server does not support targeting. It serves the same variation of a flag no
// matter who is evaluating, so the context is parsed only to reject malformed requests
// the way the real service would, and is then discarded. See
// https://launchdarkly.com/docs/guides/flags/ldcli-dev-server-reference.
func ParseClientContext(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := validateClientContext(writer, request); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		handler.ServeHTTP(writer, request)
	})
}

func validateClientContext(writer http.ResponseWriter, request *http.Request) error {
	if encoded, ok := mux.Vars(request)[contextPathVar]; ok {
		decoded, err := decodeBase64Context(encoded)
		if err != nil {
			return errors.Wrap(err, "context in path is not valid base64")
		}
		return parseClientContext(decoded)
	}

	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxClientContextBytes))
	if err != nil {
		return errors.Wrap(err, "unable to read context from request body")
	}
	// The body is consumed here but the handlers downstream don't need the context, so
	// hand back a replayable copy rather than an exhausted reader.
	request.Body = io.NopCloser(bytes.NewReader(body))
	return parseClientContext(body)
}

// decodeBase64Context decodes the context segment of a GET request path. Client-side
// SDKs base64url-encode the context and strip the padding, but standard base64 and
// explicit padding are accepted too so that no SDK's encoding choice can lock it out.
func decodeBase64Context(encoded string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(encoded)
	if remainder := len(normalized) % 4; remainder != 0 {
		normalized += strings.Repeat("=", 4-remainder)
	}
	return base64.StdEncoding.DecodeString(normalized)
}

func parseClientContext(data []byte) error {
	var context ldcontext.Context
	if err := json.Unmarshal(data, &context); err != nil {
		return errors.Wrap(err, "unable to parse evaluation context")
	}
	return errors.Wrap(context.Err(), "invalid evaluation context")
}
