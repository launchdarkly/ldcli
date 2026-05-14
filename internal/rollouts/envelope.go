package rollouts

import (
	"strings"
	"time"
)

// NewListEnvelope wraps a *RolloutList into the v1beta1 envelope with `kind: "RolloutList"`
// and a fresh `meta.fetchedAt` timestamp.
func NewListEnvelope(list *RolloutList) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersionV1Beta1,
		Kind:          "RolloutList",
		Data:          list,
		Meta: &EnvelopeMeta{
			FetchedAt: time.Now().UTC(),
		},
	}
}

// NewRolloutEnvelope wraps a single *Rollout into the v1beta1 envelope with `kind: "Rollout"`.
// Used by the start and (future) status commands. Kind "Rollout" matches Phase 3's status
// command so consumers do not need to special-case envelope kinds across verbs.
func NewRolloutEnvelope(r *Rollout) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersionV1Beta1,
		Kind:          "Rollout",
		Data:          r,
		Meta: &EnvelopeMeta{
			FetchedAt: time.Now().UTC(),
		},
	}
}

// BuildUIURL constructs an LD UI permalink for a rollout. Shape verified during Plan 04-03
// real-staging smoke; if the shape is wrong, this helper is the single fix-point.
// Returns empty string when any required component is empty (defensive).
func BuildUIURL(baseURI, projKey, flagKey, envKey, rolloutID string) string {
	if baseURI == "" || projKey == "" || flagKey == "" || envKey == "" {
		return ""
	}
	_ = rolloutID // included for future anchor use; not yet part of the URL path
	return strings.TrimRight(baseURI, "/") + "/" + projKey + "/" + envKey + "/features/" + flagKey + "/targeting"
}

// NewRolloutEnvelopeWithUI wraps a *Rollout into the v1beta1 envelope with kind="Rollout"
// AND a populated meta.uiURL. Used by mutation commands (stop, dismiss-regression) per
// Phase 4 SC#4.
func NewRolloutEnvelopeWithUI(r *Rollout, uiURL string) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersionV1Beta1,
		Kind:          "Rollout",
		Data:          r,
		Meta: &EnvelopeMeta{
			FetchedAt: time.Now().UTC(),
			UIURL:     uiURL,
		},
	}
}

// NewErrorEnvelope wraps an `error.code` / message / nextAction triple into the v1beta1
// envelope with `kind: "Error"` and no data payload.
func NewErrorEnvelope(code, message, nextAction string) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersionV1Beta1,
		Kind:          "Error",
		Error: &EnvelopeError{
			Code:       code,
			Message:    message,
			NextAction: nextAction,
		},
	}
}
