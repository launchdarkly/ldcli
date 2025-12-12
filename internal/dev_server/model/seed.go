package model

import (
	"context"
	"encoding/json"
	"os"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/pkg/errors"
)

// SeedData represents the JSON structure from the project endpoint
// matching the format from /dev/projects/{projectKey}?expand=overrides&expand=availableVariations
type SeedData struct {
	Context              ldcontext.Context           `json:"context"`
	SourceEnvironmentKey string                      `json:"sourceEnvironmentKey"`
	FlagsState           FlagsState                  `json:"flagsState"`
	Overrides            *FlagsState                 `json:"overrides,omitempty"`
	AvailableVariations  *map[string][]SeedVariation `json:"availableVariations,omitempty"`
}

// SeedVariation represents a variation in the seed data format
type SeedVariation struct {
	Id          string        `json:"_id"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Value       ldvalue.Value `json:"value"`
}

// ImportProjectFromSeed imports a project from seed data into the database.
// Returns an error if the database is not empty (contains any projects).
func ImportProjectFromSeed(ctx context.Context, projectKey string, seedData SeedData) error {
	store := StoreFromContext(ctx)

	// Validate database is empty
	existingKeys, err := store.GetDevProjectKeys(ctx)
	if err != nil {
		return errors.Wrap(err, "unable to check existing projects")
	}
	if len(existingKeys) > 0 {
		return errors.Errorf("database not empty (found %d project(s)), seeding only allowed on clean database", len(existingKeys))
	}

	// Create project from seed data
	project := Project{
		Key:                  projectKey,
		SourceEnvironmentKey: seedData.SourceEnvironmentKey,
		Context:              seedData.Context,
		AllFlagsState:        seedData.FlagsState,
		AvailableVariations:  []FlagVariation{},
	}

	// Convert available variations if present
	if seedData.AvailableVariations != nil {
		for flagKey, variations := range *seedData.AvailableVariations {
			for _, v := range variations {
				project.AvailableVariations = append(project.AvailableVariations, FlagVariation{
					FlagKey: flagKey,
					Variation: Variation{
						Id:          v.Id,
						Name:        v.Name,
						Description: v.Description,
						Value:       v.Value,
					},
				})
			}
		}
	}

	// Insert project into database
	err = store.InsertProject(ctx, project)
	if err != nil {
		return errors.Wrap(err, "unable to insert project")
	}

	// Import overrides if present
	if seedData.Overrides != nil {
		for flagKey, flagState := range *seedData.Overrides {
			// Use store directly instead of UpsertOverride to avoid observer notifications
			override := Override{
				ProjectKey: projectKey,
				FlagKey:    flagKey,
				Value:      flagState.Value,
				Active:     true,
				Version:    1,
			}
			_, err = store.UpsertOverride(ctx, override)
			if err != nil {
				return errors.Wrapf(err, "unable to import override for flag %s", flagKey)
			}
		}
	}

	return nil
}

// ImportProjectFromFile reads a JSON file and imports the project data.
func ImportProjectFromFile(ctx context.Context, projectKey, filepath string) error {
	// Read file
	data, err := os.ReadFile(filepath)
	if err != nil {
		return errors.Wrapf(err, "unable to read file %s", filepath)
	}

	// Parse JSON
	var seedData SeedData
	err = json.Unmarshal(data, &seedData)
	if err != nil {
		return errors.Wrap(err, "unable to parse JSON")
	}

	// Validate required fields
	if seedData.SourceEnvironmentKey == "" {
		return errors.New("sourceEnvironmentKey is required in seed data")
	}
	if seedData.FlagsState == nil {
		return errors.New("flagsState is required in seed data")
	}

	// Import the project
	return ImportProjectFromSeed(ctx, projectKey, seedData)
}
