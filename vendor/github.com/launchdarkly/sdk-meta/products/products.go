package products

import (
	_ "embed"
	"encoding/json"
	"time"
)

//go:embed names.json
var namesJSON []byte

// Names is a map of SDK IDs to display names.
var Names map[string]string

//go:embed repos.json
var reposJSON []byte

// Repo contains the location of an SDK.
type Repo struct {
	// GitHub is the GitHub repo path including the owner and repo name (e.g. launchdarkly/js-core).
	GitHub string `json:"github"`
}

// Repos is a map of SDK IDs to repository information.
var Repos map[string]Repo

//go:embed languages.json
var languagesJSON []byte

// Languages is a map of SDK IDs to supported languages.
var Languages map[string][]string

//go:embed types.json
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

//go:embed releases.json
var releasesJSON []byte

type Release struct {
	Major int        `json:"major"`
	Minor int        `json:"minor"`
	Date  time.Time  `json:"date"`
	EOL   *time.Time `json:"eol"`
}

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
