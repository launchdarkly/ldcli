package sdkmeta

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

//go:embed data/names.json
var namesJSON []byte

// Names is a map of SDK IDs to display names.
var Names map[string]string

//go:embed data/repos.json
var reposJSON []byte

// Repo contains the location of an SDK.
type Repo struct {
	// GitHub is the GitHub repo path including the owner and repo name (e.g. launchdarkly/js-core).
	GitHub string `json:"github"`
}

// Repos is a map of SDK IDs to repository information.
var Repos map[string]Repo

//go:embed data/languages.json
var languagesJSON []byte

// Languages is a map of SDK IDs to supported languages.
var Languages map[string][]string

//go:embed data/types.json
var typesJSON []byte

// Type represents the most common use-case for an SDK.
type Type string

const (
	// ClientSideType is an SDK that runs in a client context.
	ClientSideType Type = "client-side"
	// ServerSideType is an SDK that runs in a server context.
	ServerSideType Type = "server-side"
	// EdgeType is an SDK that runs in an edge deployment scenario.
	EdgeType Type = "edge"
	// RelayType is Relay Proxy.
	RelayType Type = "relay"
)

// Types is a map of SDK IDs to SDK types.
var Types map[string]Type

//go:embed data/releases.json
var releasesJSON []byte

type Release struct {
	Major int        `json:"major"`
	Minor int        `json:"minor"`
	Date  time.Time  `json:"date"`
	EOL   *time.Time `json:"eol"`
}

// MajorMinor returns a version string comprised of the major and minor version. For example,
// '2.1'.
func (r Release) MajorMinor() string {
	return fmt.Sprintf("%d.%d", r.Major, r.Minor)
}

// IsLatest returns true if the release is the latest release, meaning there is no EOL date set.
func (r Release) IsLatest() bool {
	return r.EOL == nil
}

// IsEOL returns true if the release is not the latest release and the current time is after the EOL date. The parameter
// represents the current time.
func (r Release) IsEOL(now time.Time) bool {
	return !r.IsLatest() && now.After(*r.EOL)
}

// IsApproachingEOL returns true if the release is not the latest release and the EOL date is within the time period
// from now to now + thresholdPrior. This is only valid if IsEOL() returns false.
func (r Release) IsApproachingEOL(now time.Time, thresholdPrior time.Duration) bool {
	return !r.IsLatest() && now.Add(thresholdPrior).After(*r.EOL)
}

// ReleaseList is an ordered list of releases. The first item should be the most recent release, while the
// last item is the oldest release.
type ReleaseList []Release

var Releases map[string]ReleaseList

// Earliest returns the earliest release.
func (r ReleaseList) Earliest() Release {
	return r[len(r)-1]
}

// Latest returns the latest release.
func (r ReleaseList) Latest() Release {
	return r[0]
}

func panicOnError(err error) {
	if err != nil {
		panic("couldn't initialize SDK Metadata module: " + err.Error())
	}
}

func init() {
	panicOnError(json.Unmarshal(namesJSON, &Names))
	panicOnError(json.Unmarshal(reposJSON, &Repos))
	panicOnError(json.Unmarshal(languagesJSON, &Languages))
	panicOnError(json.Unmarshal(typesJSON, &Types))
	panicOnError(json.Unmarshal(releasesJSON, &Releases))
}
