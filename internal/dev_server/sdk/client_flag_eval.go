package sdk

import (
	"net/http"

	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

// flagEvalKind is the object kind for client-side FDv2 put-object events. Note the
// hyphen: the SDK-side contract spells this "flag-eval", not "flag_eval".
const flagEvalKind = subsystems.ObjectKind("flag-eval")

// clientFlagEval is the object carried by a put-object event of kind flag-eval: a
// flag result the server already evaluated, rather than the flag configuration that
// fdv2ServerObjects sends.
//
// Compared with the FDv1 client-side representation (clientFlag), this drops the
// payload version — the put-object envelope carries it — and gains samplingRatio
// and trackEvents. The flag key lives on the envelope too, so it is absent here.
type clientFlagEval struct {
	FlagVersion int           `json:"flagVersion,omitempty"`
	Value       ldvalue.Value `json:"value"`
	Variation   int           `json:"variation"`
	TrackEvents bool          `json:"trackEvents"`
	// Reason is only populated when the SDK asked for evaluation reasons.
	Reason *ldreason.EvaluationReason `json:"reason,omitempty"`
	// SamplingRatio is part of the protocol but the dev server never samples, so it
	// stays unset — SDKs treat an absent ratio as 1.
	SamplingRatio *int `json:"samplingRatio,omitempty"`
}

// fdv2ClientObjectsForRequest builds the client-side encoder for a request, honouring
// the withReasons query parameter.
func fdv2ClientObjectsForRequest(r *http.Request) fdv2ObjectEncoder {
	return fdv2ClientObjects(r.URL.Query().Get("withReasons") == "true")
}

// fdv2ClientObjects encodes flags for client-side SDKs as pre-evaluated results.
//
// The dev server does not evaluate targeting: it serves one variation per flag no
// matter who is evaluating. That variation is the only one it exposes, so it is
// always variation 0 and always the fallthrough for an on flag, which is what the
// server-side representation reports as well.
func fdv2ClientObjects(withReasons bool) fdv2ObjectEncoder {
	var reason *ldreason.EvaluationReason
	if withReasons {
		fallthroughReason := ldreason.NewEvalReasonFallthrough()
		reason = &fallthroughReason
	}
	return fdv2ObjectEncoder{
		kind: flagEvalKind,
		encode: func(_ string, flagState model.FlagState) any {
			return clientFlagEval{
				FlagVersion: flagState.Version,
				Value:       flagState.Value,
				Variation:   0,
				TrackEvents: flagState.TrackEvents,
				Reason:      reason,
			}
		},
	}
}
