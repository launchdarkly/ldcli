package dev_server

const (
	ContextFlag           = "context"
	OverrideFlag          = "override"
	SourceEnvironmentFlag = "source"

	StreamFlagStartupFlag        = "stream-flag-startup"
	StreamFlagStartupDescription = "Load flag values from the streaming connection at startup and resolve variation " +
		"display names from REST in the background. Speeds up startup on large projects (the health check passes in " +
		"~1s) at the cost of variation names appearing in the UI a few seconds later."
)
