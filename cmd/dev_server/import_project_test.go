package dev_server_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func TestImportProjectCommand(t *testing.T) {
	t.Run("imports project successfully from valid JSON file", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")

		// Create test seed data file
		seedFile := filepath.Join(tmpDir, "seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"sourceEnvironmentKey": "production",
			"flagsState": map[string]interface{}{
				"feature-flag-1": map[string]interface{}{
					"value":       true,
					"version":     1,
					"trackEvents": false,
				},
				"feature-flag-2": map[string]interface{}{
					"value":       "enabled",
					"version":     2,
					"trackEvents": false,
				},
			},
			"availableVariations": map[string]interface{}{
				"feature-flag-1": []interface{}{
					map[string]interface{}{
						"_id":         "var-true",
						"name":        "On",
						"description": "Feature enabled",
						"value":       true,
					},
					map[string]interface{}{
						"_id":   "var-false",
						"name":  "Off",
						"value": false,
					},
				},
				"feature-flag-2": []interface{}{
					map[string]interface{}{
						"_id":   "var-enabled",
						"value": "enabled",
					},
					map[string]interface{}{
						"_id":   "var-disabled",
						"value": "disabled",
					},
				},
			},
			"overrides": map[string]interface{}{
				"feature-flag-1": map[string]interface{}{
					"value":   false,
					"version": 1,
				},
			},
		}

		data, err := json.MarshalIndent(seedData, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		// Test the seed functionality directly (not through cobra command to avoid CLI setup complexity)
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)

		ctx = model.ContextWithStore(ctx, sqlStore)

		// Import project from file
		err = model.ImportProjectFromFile(ctx, "test-project", seedFile)
		require.NoError(t, err)

		// Verify project was created
		project, err := sqlStore.GetDevProject(ctx, "test-project")
		require.NoError(t, err)
		require.NotNil(t, project)

		// Verify project fields
		assert.Equal(t, "test-project", project.Key)
		assert.Equal(t, "production", project.SourceEnvironmentKey)
		assert.Equal(t, ldcontext.NewBuilder("user").Key("test-user").Build(), project.Context)

		// Verify flags state
		assert.Len(t, project.AllFlagsState, 2)
		assert.Equal(t, ldvalue.Bool(true), project.AllFlagsState["feature-flag-1"].Value)
		assert.Equal(t, 1, project.AllFlagsState["feature-flag-1"].Version)
		assert.Equal(t, ldvalue.String("enabled"), project.AllFlagsState["feature-flag-2"].Value)
		assert.Equal(t, 2, project.AllFlagsState["feature-flag-2"].Version)

		// Verify available variations
		variations, err := sqlStore.GetAvailableVariationsForProject(ctx, "test-project")
		require.NoError(t, err)
		assert.Len(t, variations, 2)
		assert.Len(t, variations["feature-flag-1"], 2)
		assert.Len(t, variations["feature-flag-2"], 2)

		// Verify overrides
		overrides, err := sqlStore.GetOverridesForProject(ctx, "test-project")
		require.NoError(t, err)
		assert.Len(t, overrides, 1)
		assert.Equal(t, "feature-flag-1", overrides[0].FlagKey)
		assert.Equal(t, ldvalue.Bool(false), overrides[0].Value)
		assert.True(t, overrides[0].Active)
	})

	t.Run("rejects importing when project already exists", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		// Insert an existing project with the same key
		existingProject := model.Project{
			Key:                  "existing-project",
			SourceEnvironmentKey: "test",
			Context:              ldcontext.NewBuilder("user").Key("existing").Build(),
			AllFlagsState:        model.FlagsState{},
			AvailableVariations:  []model.FlagVariation{},
		}
		err = sqlStore.InsertProject(ctx, existingProject)
		require.NoError(t, err)

		// Create seed data file for the same project key
		seedFile := filepath.Join(tmpDir, "seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"sourceEnvironmentKey": "production",
			"flagsState": map[string]interface{}{
				"flag-1": map[string]interface{}{
					"value":   true,
					"version": 1,
				},
			},
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		// Attempt to import with same project key should fail
		err = model.ImportProjectFromFile(ctx, "existing-project", seedFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("allows importing different project when database has other projects", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		// Insert an existing project
		existingProject := model.Project{
			Key:                  "project-1",
			SourceEnvironmentKey: "test",
			Context:              ldcontext.NewBuilder("user").Key("existing").Build(),
			AllFlagsState:        model.FlagsState{},
			AvailableVariations:  []model.FlagVariation{},
		}
		err = sqlStore.InsertProject(ctx, existingProject)
		require.NoError(t, err)

		// Create seed data file for a DIFFERENT project
		seedFile := filepath.Join(tmpDir, "seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"sourceEnvironmentKey": "production",
			"flagsState": map[string]interface{}{
				"flag-1": map[string]interface{}{
					"value":   true,
					"version": 1,
				},
			},
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		// Import a different project should succeed
		err = model.ImportProjectFromFile(ctx, "project-2", seedFile)
		require.NoError(t, err)

		// Verify both projects exist
		project1, err := sqlStore.GetDevProject(ctx, "project-1")
		require.NoError(t, err)
		assert.Equal(t, "project-1", project1.Key)

		project2, err := sqlStore.GetDevProject(ctx, "project-2")
		require.NoError(t, err)
		assert.Equal(t, "project-2", project2.Key)
	})

	t.Run("validates required fields in import data", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		testCases := []struct {
			name        string
			seedData    map[string]interface{}
			expectedErr string
		}{
			{
				name: "missing sourceEnvironmentKey",
				seedData: map[string]interface{}{
					"context": map[string]interface{}{
						"kind": "user",
						"key":  "test",
					},
					"flagsState": map[string]interface{}{
						"flag": map[string]interface{}{
							"value":   true,
							"version": 1,
						},
					},
				},
				expectedErr: "sourceEnvironmentKey is required",
			},
			{
				name: "missing flagsState",
				seedData: map[string]interface{}{
					"context": map[string]interface{}{
						"kind": "user",
						"key":  "test",
					},
					"sourceEnvironmentKey": "test",
				},
				expectedErr: "flagsState is required",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				seedFile := filepath.Join(tmpDir, tc.name+".json")
				data, err := json.Marshal(tc.seedData)
				require.NoError(t, err)
				err = os.WriteFile(seedFile, data, 0644)
				require.NoError(t, err)

				err = model.ImportProjectFromFile(ctx, "test-project", seedFile)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			})
		}
	})

	t.Run("handles complex import data with all fields", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		// Create comprehensive seed data
		seedFile := filepath.Join(tmpDir, "comprehensive-seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind":  "user",
				"key":   "user-123",
				"email": "test@example.com",
				"name":  "Test User",
			},
			"sourceEnvironmentKey": "staging",
			"flagsState": map[string]interface{}{
				"bool-flag": map[string]interface{}{
					"value":       true,
					"version":     5,
					"trackEvents": true,
				},
				"string-flag": map[string]interface{}{
					"value":       "test-value",
					"version":     3,
					"trackEvents": false,
				},
				"number-flag": map[string]interface{}{
					"value":       42,
					"version":     1,
					"trackEvents": false,
				},
				"json-flag": map[string]interface{}{
					"value": map[string]interface{}{
						"nested": "object",
						"count":  10,
					},
					"version":     2,
					"trackEvents": false,
				},
			},
			"availableVariations": map[string]interface{}{
				"bool-flag": []interface{}{
					map[string]interface{}{
						"_id":         "bool-true",
						"name":        "True Variation",
						"description": "Boolean true",
						"value":       true,
					},
					map[string]interface{}{
						"_id":   "bool-false",
						"name":  "False Variation",
						"value": false,
					},
				},
				"string-flag": []interface{}{
					map[string]interface{}{
						"_id":   "string-1",
						"value": "test-value",
					},
					map[string]interface{}{
						"_id":   "string-2",
						"value": "other-value",
					},
				},
			},
			"overrides": map[string]interface{}{
				"bool-flag": map[string]interface{}{
					"value":   false,
					"version": 1,
				},
				"number-flag": map[string]interface{}{
					"value":   100,
					"version": 1,
				},
			},
		}

		data, err := json.MarshalIndent(seedData, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		// Import
		err = model.ImportProjectFromFile(ctx, "comprehensive-project", seedFile)
		require.NoError(t, err)

		// Verify all data was imported
		project, err := sqlStore.GetDevProject(ctx, "comprehensive-project")
		require.NoError(t, err)

		assert.Equal(t, "staging", project.SourceEnvironmentKey)
		assert.Len(t, project.AllFlagsState, 4)

		// Verify variations
		variations, err := sqlStore.GetAvailableVariationsForProject(ctx, "comprehensive-project")
		require.NoError(t, err)
		assert.Len(t, variations["bool-flag"], 2)
		assert.Len(t, variations["string-flag"], 2)

		// Verify overrides
		overrides, err := sqlStore.GetOverridesForProject(ctx, "comprehensive-project")
		require.NoError(t, err)
		assert.Len(t, overrides, 2)

		overrideMap := make(map[string]model.Override)
		for _, o := range overrides {
			overrideMap[o.FlagKey] = o
		}

		assert.Equal(t, ldvalue.Bool(false), overrideMap["bool-flag"].Value)
		assert.Equal(t, ldvalue.Int(100), overrideMap["number-flag"].Value)
	})

	t.Run("handles import data without optional fields", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		// Minimal seed data with only required fields
		seedFile := filepath.Join(tmpDir, "minimal-seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "minimal-user",
			},
			"sourceEnvironmentKey": "production",
			"flagsState": map[string]interface{}{
				"simple-flag": map[string]interface{}{
					"value":   "simple-value",
					"version": 1,
				},
			},
			// No availableVariations
			// No overrides
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		// Import
		err = model.ImportProjectFromFile(ctx, "minimal-project", seedFile)
		require.NoError(t, err)

		// Verify
		project, err := sqlStore.GetDevProject(ctx, "minimal-project")
		require.NoError(t, err)
		assert.Equal(t, "minimal-project", project.Key)
		assert.Len(t, project.AllFlagsState, 1)

		// Verify no variations or overrides
		variations, err := sqlStore.GetAvailableVariationsForProject(ctx, "minimal-project")
		require.NoError(t, err)
		assert.Empty(t, variations)

		overrides, err := sqlStore.GetOverridesForProject(ctx, "minimal-project")
		require.NoError(t, err)
		assert.Empty(t, overrides)
	})

	t.Run("preserves variation metadata", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "import-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		ctx := context.Background()
		sqlStore, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		ctx = model.ContextWithStore(ctx, sqlStore)

		seedFile := filepath.Join(tmpDir, "variations-seed.json")
		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test",
			},
			"sourceEnvironmentKey": "test",
			"flagsState": map[string]interface{}{
				"documented-flag": map[string]interface{}{
					"value":   "default",
					"version": 1,
				},
			},
			"availableVariations": map[string]interface{}{
				"documented-flag": []interface{}{
					map[string]interface{}{
						"_id":         "var-1",
						"name":        "Default Variation",
						"description": "This is the default variation used in most cases",
						"value":       "default",
					},
					map[string]interface{}{
						"_id":         "var-2",
						"name":        "Alternative",
						"description": "Alternative variation for special cases",
						"value":       "alternative",
					},
				},
			},
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		err = os.WriteFile(seedFile, data, 0644)
		require.NoError(t, err)

		err = model.ImportProjectFromFile(ctx, "metadata-project", seedFile)
		require.NoError(t, err)

		// Verify variation metadata is preserved
		variations, err := sqlStore.GetAvailableVariationsForProject(ctx, "metadata-project")
		require.NoError(t, err)
		require.Len(t, variations["documented-flag"], 2)

		var defaultVar, altVar *model.Variation
		for _, v := range variations["documented-flag"] {
			if v.Id == "var-1" {
				defaultVar = lo.ToPtr(v)
			} else if v.Id == "var-2" {
				altVar = lo.ToPtr(v)
			}
		}

		require.NotNil(t, defaultVar)
		require.NotNil(t, altVar)

		assert.Equal(t, "Default Variation", *defaultVar.Name)
		assert.Equal(t, "This is the default variation used in most cases", *defaultVar.Description)
		assert.Equal(t, ldvalue.String("default"), defaultVar.Value)

		assert.Equal(t, "Alternative", *altVar.Name)
		assert.Equal(t, "Alternative variation for special cases", *altVar.Description)
		assert.Equal(t, ldvalue.String("alternative"), altVar.Value)
	})
}
