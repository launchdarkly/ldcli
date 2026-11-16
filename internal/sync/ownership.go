package sync

// OwnershipAction is what sync does with a variation's ownership before it
// pushes or pulls the variation.
type OwnershipAction int

const (
	// OwnershipNothingToDo means that an exact rule already makes the
	// variation code-owned.
	OwnershipNothingToDo OwnershipAction = iota
	// OwnershipClaim means that sync writes an exact code_canonical rule.
	OwnershipClaim
	// OwnershipManagedInLaunchDarkly means that an explicit LaunchDarkly rule
	// owns the variation: sync pulls it, and never pushes or claims it.
	OwnershipManagedInLaunchDarkly
)

// DecideOwnership decides what sync does with the ownership of one variation.
func DecideOwnership(id ResourceID, ownership Ownership) OwnershipAction {
	if !ownership.HasRule() {
		return OwnershipClaim
	}
	switch ownership.Direction {
	case SyncDirectionServerCanonical:
		return OwnershipManagedInLaunchDarkly
	case SyncDirectionCodeCanonical:
		if ownership.HasExactRule(id) {
			return OwnershipNothingToDo
		}
	}
	return OwnershipClaim
}

// ManagedInLaunchDarklyMessage tells the user that LaunchDarkly owns a
// variation and how to move it to code.
func ManagedInLaunchDarklyMessage(id ResourceID) string {
	return id.LookupKey + " is managed in LaunchDarkly; pulled, not pushed. To manage it in code, switch it to Code in LaunchDarkly."
}
