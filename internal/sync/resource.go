package sync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const RootDir = ".launchdarkly"

type Kind string

const (
	KindVariation Kind = "variation"
	KindTool      Kind = "tool"
)

type Fingerprint string

func Hash(payload []byte) Fingerprint {
	sum := sha256.Sum256(payload)

	return Fingerprint("sha256." + hex.EncodeToString(sum[:]))
}

type SyncedResource struct {
	Kind        Kind
	ProjectKey  string
	LookupKey   string
	Payload     json.RawMessage
	Fingerprint Fingerprint
	Upsert      bool
}

func marshalPayload(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
