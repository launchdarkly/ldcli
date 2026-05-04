package model

// Event for individual flag overrides
type OverrideEvent struct {
	FlagKey        string
	ProjectKey     string
	FlagState      FlagState
	PayloadVersion int
}

// Event for full project sync
type SyncEvent struct {
	ProjectKey     string
	AllFlagsState  FlagsState
	PayloadVersion int
}
