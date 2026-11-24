package sync

// ManagedInLaunchDarklyMessage tells the user that LaunchDarkly owns a
// variation and how to move it to code.
func ManagedInLaunchDarklyMessage(id ResourceID) string {
	return id.LookupKey + " is managed in LaunchDarkly; pulled, not pushed. To manage it in code, switch it to Code in LaunchDarkly."
}
