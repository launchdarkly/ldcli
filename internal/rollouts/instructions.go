package rollouts

// SemanticPatch is the JSON envelope used by LaunchDarkly's flag semantic-patch endpoint. The
// `Instructions` slice carries one or more typed instruction structs; the upstream API matches
// each instruction by its "kind" tag (see StartInstruction, StopInstruction, etc.).
//
// Phase 1: declared as a skeleton only — the Client interface does not yet expose any mutation
// method (D-08). Phase 2 fleshes out StartInstruction; Phase 4 fleshes out the rest.
type SemanticPatch struct {
	Comment      string        `json:"comment,omitempty"`
	Instructions []interface{} `json:"instructions"`
}

// StartInstruction kicks off an automated rollout. Phase 1 declares the struct shape so
// Phase 2 only needs to fill in the field set + wire it through the client.
type StartInstruction struct {
	Kind string `json:"kind"`
}

// StopInstruction terminates an in-progress rollout. Phase 4 fleshes this out.
type StopInstruction struct {
	Kind string `json:"kind"`
}

// DismissRegressionInstruction clears a detected-regression flag on a guarded rollout so it
// can resume. Phase 4 fleshes this out.
type DismissRegressionInstruction struct {
	Kind string `json:"kind"`
}
