package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const variationFingerprintSchema = "launchdarkly.config.variation/v1"

// FingerprintVariation returns a stable fingerprint for the variation fields
// supported by the existing config variation APIs.
func FingerprintVariation(projectKey, lookupKey string, variation Variation) (string, error) {
	if err := ValidateDirectAPIVariation(variation); err != nil {
		return "", err
	}

	normalized := variation
	if len(normalized.Model) == 0 {
		normalized.Model = nil
	}
	if len(normalized.Messages) == 0 {
		normalized.Messages = nil
	}
	switch normalized.Mode {
	case VariationModeAgent:
		normalized.Messages = nil
	case VariationModeCompletion:
		normalized.Instructions = ""
	}

	value := struct {
		Schema       string    `json:"schema"`
		ResourceKind Kind      `json:"resourceKind"`
		ProjectKey   string    `json:"projectKey"`
		LookupKey    string    `json:"lookupKey"`
		Variation    Variation `json:"variation"`
	}{
		Schema:       variationFingerprintSchema,
		ResourceKind: KindVariation,
		ProjectKey:   projectKey,
		LookupKey:    lookupKey,
		Variation:    normalized,
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode variation fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ValidateDirectAPIVariation rejects fields that the existing variation APIs
// cannot round-trip without the sync endpoints.
func ValidateDirectAPIVariation(variation Variation) error {
	switch {
	case !variation.Mode.Valid():
		return fmt.Errorf("unsupported variation mode %q", variation.Mode)
	case variation.Key == "":
		return fmt.Errorf("variation key is required")
	case variation.Name == "":
		return fmt.Errorf("variation name is required")
	case len(variation.OutputFormat) != 0:
		return fmt.Errorf("outputFormat is not supported by direct config variation APIs")
	}
	return nil
}
