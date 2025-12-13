package model

import (
	"context"
	"encoding/json"
	"os"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/pkg/errors"
)

// ImportData represents the JSON structure from the project endpoint
// matching the format from /dev/projects/{projectKey}?expand=overrides&expand=availableVariations
type ImportData struct {
	Context              ldcontext.Context             `json:"context"`
	SourceEnvironmentKey string                        `json:"sourceEnvironmentKey"`
	FlagsState           FlagsState                    `json:"flagsState"`
	Overrides            *FlagsState                   `json:"overrides,omitempty"`
	AvailableVariations  *map[string][]ImportVariation `json:"availableVariations,omitempty"`
}

// ImportVariation represents a variation in the import data format
type ImportVariation struct {
	Id          string        `json:"_id"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Value       ldvalue.Value `json:"value"`
}

// ImportProject imports a project from import data into the database.
// Returns an error if the project already exists.
func ImportProject(ctx context.Context, projectKey string, importData ImportData) error {
	store := StoreFromContext(ctx)

	// Check if project already exists
	existingProject, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		// ErrNotFound is expected - it means the project doesn't exist yet, which is what we want
		if _, ok := err.(ErrNotFound); !ok {
			return errors.Wrap(err, "unable to check if project exists")
		}
		// Project doesn't exist, continue with import
	} else if existingProject != nil {
		// Project exists, cannot import
		return errors.Errorf("project '%s' already exists, cannot import", projectKey)
	}

	// Create project from import data
	project := Project{
		Key:                  projectKey,
		SourceEnvironmentKey: importData.SourceEnvironmentKey,
		Context:              importData.Context,
		AllFlagsState:        importData.FlagsState,
		AvailableVariations:  []FlagVariation{},
	}

	// Convert available variations if present
	if importData.AvailableVariations != nil {
		for flagKey, variations := range *importData.AvailableVariations {
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
	if importData.Overrides != nil {
		for flagKey, flagState := range *importData.Overrides {
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
	var importData ImportData
	err = json.Unmarshal(data, &importData)
	if err != nil {
		return errors.Wrap(err, "unable to parse JSON")
	}

	// Validate required fields
	if importData.SourceEnvironmentKey == "" {
		return errors.New("sourceEnvironmentKey is required in import data")
	}
	if importData.FlagsState == nil {
		return errors.New("flagsState is required in import data")
	}

	// Import the project
	return ImportProject(ctx, projectKey, importData)
}
