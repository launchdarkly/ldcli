package manifest

import (
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Baseline is the sync baseline of one working copy.
//
// Lock is the state that this working copy last agreed on with LaunchDarkly.
// Sync compares the local files and LaunchDarkly with Lock to find which side
// changed. The remote manifest is shared by every working copy of the
// repository. It supplies the version of each entry, which LaunchDarkly uses
// to reject two saves of one entry at the same time.
type Baseline struct {
	Lock           Manifest
	remote         Manifest
	lockFileExists bool
}

// HasLockFile reports whether the working copy has a sync.lock file. A
// working copy without one uses the remote manifest as its lock.
func (baseline Baseline) HasLockFile() bool {
	return baseline.lockFileExists
}

// Stale reports whether another working copy synced a different state of the
// resource after this working copy wrote its lock.
//
// The fingerprints decide, not the versions. A newer version with the same
// fingerprint means that LaunchDarkly came back to the state of the lock, so
// there is nothing to pull. A missing remote entry means that another working
// copy stopped syncing the resource, which does not change this working copy.
func (baseline Baseline) Stale(id syncdomain.ResourceID) bool {
	lockIndex, remoteIndex := baseline.Lock.index(id), baseline.remote.index(id)
	if lockIndex < 0 || remoteIndex < 0 {
		return false
	}
	return baseline.Lock.Resources[lockIndex].Fingerprint != baseline.remote.Resources[remoteIndex].Fingerprint
}

// Baselines loads and saves the baseline of a working copy. BaselineStore
// implements it.
type Baselines interface {
	Load(projectKeys []string) (Baseline, error)
	Save(current Baseline, next Manifest) (Baseline, error)
}

// BaselineStore keeps the baseline in the sync.lock file and the entry
// versions in the remote manifest.
type BaselineStore struct {
	remote Store
	lock   LockFile
}

var _ Baselines = BaselineStore{}

// NewBaselineStore creates a store for one working copy.
func NewBaselineStore(remote Store, lock LockFile) BaselineStore {
	return BaselineStore{remote: remote, lock: lock}
}

// Load reads the sync.lock file and the remote manifest of each project that
// projectKeys or the lock names. A working copy without a sync.lock file uses
// the remote manifest as its lock. Its next save then writes the file.
func (store BaselineStore) Load(projectKeys []string) (Baseline, error) {
	lock, hasLock, err := ReadLock(store.lock)
	if err != nil {
		return Baseline{}, err
	}
	remote, err := store.remote.Load(append(slices.Clone(projectKeys), lock.ProjectKeys()...))
	if err != nil {
		return Baseline{}, err
	}
	if !hasLock {
		lock = remote.Clone()
	}
	return Baseline{Lock: lock, remote: remote, lockFileExists: hasLock}, nil
}

// Save records next as the new baseline. It sends the entries that changed
// from current.Lock to next to the remote manifest, with the versions that
// sync read, and then writes next to the sync.lock file. If the remote save
// fails, the lock file does not change.
func (store BaselineStore) Save(current Baseline, next Manifest) (Baseline, error) {
	remote, err := store.remote.Update(current.remote, current.remote.WithChanges(current.Lock, next))
	if err != nil {
		return Baseline{}, err
	}
	saved := Baseline{Lock: next.Clone(), remote: remote, lockFileExists: true}
	saved.Lock.Sort()
	if err := writeLock(store.lock, saved.Lock); err != nil {
		return Baseline{}, err
	}
	return saved, nil
}
