package model

// Event for individual flag overrides
type OverrideEvent struct {
	FlagKey    string
	ProjectKey string
	FlagState  FlagState
}

// Event for full project sync
type SyncEvent struct {
	ProjectKey    string
	AllFlagsState FlagsState
}
