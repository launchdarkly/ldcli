package rollouts

import "time"

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
