package dev_server

const (
	ContextFlag           = "context"
	OverrideFlag          = "override"
	SourceEnvironmentFlag = "source"

	StreamFlagStartupFlag        = "stream-flag-startup"
	StreamFlagStartupDescription = "Load flag values from the streaming connection at startup and resolve variation " +
		"display names from REST in the background. Speeds up startup on large projects (the health check passes in " +
		"~1s) at the cost of variation names appearing in the UI a few seconds later."

	SdkInitTimeoutFlag        = "sdk-init-timeout"
	SdkInitTimeoutDescription = "How long to wait for the LaunchDarkly SDK to connect to the stream and receive the " +
		"full initial flag payload when syncing a source environment, e.g. 15s or 1m. Increase this for large " +
		"projects or slow links. Must be greater than zero."
)
