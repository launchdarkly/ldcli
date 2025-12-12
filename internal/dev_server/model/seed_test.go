package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestImportProjectFromSeed(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)

	projectKey := "test-project"
	seedData := model.SeedData{
		Context:              ldcontext.NewBuilder("user").Key("test-user").Build(),
		SourceEnvironmentKey: "test-env",
		FlagsState: model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 1},
			"flag-2": model.FlagState{Value: ldvalue.String("hello"), Version: 2},
		},
		AvailableVariations: &map[string][]model.SeedVariation{
			"flag-1": {
				{
					Id:          "var-1",
					Name:        lo.ToPtr("True"),
					Description: lo.ToPtr("True variation"),
					Value:       ldvalue.Bool(true),
				},
				{
					Id:    "var-2",
					Name:  lo.ToPtr("False"),
					Value: ldvalue.Bool(false),
				},
			},
			"flag-2": {
				{
					Id:    "var-3",
					Value: ldvalue.String("hello"),
				},
				{
					Id:    "var-4",
					Value: ldvalue.String("world"),
				},
			},
		},
		Overrides: &model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(false), Version: 1},
		},
	}

	t.Run("Returns error if database is not empty", func(t *testing.T) {
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{"existing-project"}, nil)

		err := model.ImportProjectFromSeed(ctx, projectKey, seedData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database not empty")
		assert.Contains(t, err.Error(), "found 1 project")
	})

	t.Run("Returns error if checking existing projects fails", func(t *testing.T) {
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return(nil, errors.New("db error"))

		err := model.ImportProjectFromSeed(ctx, projectKey, seedData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to check existing projects")
		assert.Contains(t, err.Error(), "db error")
	})

	t.Run("Returns error if insert project fails", func(t *testing.T) {
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(errors.New("insert failed"))

		err := model.ImportProjectFromSeed(ctx, projectKey, seedData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to insert project")
		assert.Contains(t, err.Error(), "insert failed")
	})

	t.Run("Returns error if upserting override fails", func(t *testing.T) {
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)
		store.EXPECT().UpsertOverride(gomock.Any(), gomock.Any()).Return(model.Override{}, errors.New("override failed"))

		err := model.ImportProjectFromSeed(ctx, projectKey, seedData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to import override")
		assert.Contains(t, err.Error(), "override failed")
	})

	t.Run("Successfully imports project without overrides", func(t *testing.T) {
		seedDataNoOverrides := model.SeedData{
			Context:              ldcontext.NewBuilder("user").Key("test-user").Build(),
			SourceEnvironmentKey: "test-env",
			FlagsState: model.FlagsState{
				"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 1},
			},
		}

		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, project model.Project) error {
				assert.Equal(t, projectKey, project.Key)
				assert.Equal(t, "test-env", project.SourceEnvironmentKey)
				assert.Equal(t, ldcontext.NewBuilder("user").Key("test-user").Build(), project.Context)
				assert.Equal(t, seedDataNoOverrides.FlagsState, project.AllFlagsState)
				return nil
			},
		)

		err := model.ImportProjectFromSeed(ctx, projectKey, seedDataNoOverrides)
		require.NoError(t, err)
	})

	t.Run("Successfully imports project with overrides and variations", func(t *testing.T) {
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, project model.Project) error {
				// Verify project fields
				assert.Equal(t, projectKey, project.Key)
				assert.Equal(t, "test-env", project.SourceEnvironmentKey)
				assert.Equal(t, seedData.Context, project.Context)
				assert.Equal(t, seedData.FlagsState, project.AllFlagsState)

				// Verify available variations were converted correctly
				assert.Len(t, project.AvailableVariations, 4) // 2 for flag-1, 2 for flag-2

				// Check that all variations are present
				foundFlags := make(map[string]int)
				for _, fv := range project.AvailableVariations {
					foundFlags[fv.FlagKey]++
				}
				assert.Equal(t, 2, foundFlags["flag-1"])
				assert.Equal(t, 2, foundFlags["flag-2"])

				return nil
			},
		)
		store.EXPECT().UpsertOverride(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, override model.Override) (model.Override, error) {
				assert.Equal(t, projectKey, override.ProjectKey)
				assert.Equal(t, "flag-1", override.FlagKey)
				assert.Equal(t, ldvalue.Bool(false), override.Value)
				assert.True(t, override.Active)
				return override, nil
			},
		)

		err := model.ImportProjectFromSeed(ctx, projectKey, seedData)
		require.NoError(t, err)
	})
}

func TestImportProjectFromFile(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)

	projectKey := "test-project"

	t.Run("Returns error if file does not exist", func(t *testing.T) {
		err := model.ImportProjectFromFile(ctx, projectKey, "/nonexistent/file.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to read file")
	})

	t.Run("Returns error if JSON is invalid", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "invalid-*.json")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString("{ invalid json }")
		require.NoError(t, err)
		tmpFile.Close()

		err = model.ImportProjectFromFile(ctx, projectKey, tmpFile.Name())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to parse JSON")
	})

	t.Run("Returns error if sourceEnvironmentKey is missing", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "missing-env-*.json")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"flagsState": map[string]interface{}{
				"flag-1": map[string]interface{}{
					"value":   true,
					"version": 1,
				},
			},
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		_, err = tmpFile.Write(data)
		require.NoError(t, err)
		tmpFile.Close()

		err = model.ImportProjectFromFile(ctx, projectKey, tmpFile.Name())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sourceEnvironmentKey is required")
	})

	t.Run("Returns error if flagsState is missing", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "missing-flags-*.json")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"sourceEnvironmentKey": "test-env",
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		_, err = tmpFile.Write(data)
		require.NoError(t, err)
		tmpFile.Close()

		err = model.ImportProjectFromFile(ctx, projectKey, tmpFile.Name())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flagsState is required")
	})

	t.Run("Successfully imports from valid JSON file", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "valid-*.json")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		seedData := map[string]interface{}{
			"context": map[string]interface{}{
				"kind": "user",
				"key":  "test-user",
			},
			"sourceEnvironmentKey": "test-env",
			"flagsState": map[string]interface{}{
				"flag-1": map[string]interface{}{
					"value":       true,
					"version":     1,
					"trackEvents": false,
				},
			},
			"availableVariations": map[string]interface{}{
				"flag-1": []interface{}{
					map[string]interface{}{
						"_id":   "var-1",
						"name":  "True",
						"value": true,
					},
					map[string]interface{}{
						"_id":   "var-2",
						"name":  "False",
						"value": false,
					},
				},
			},
		}

		data, err := json.Marshal(seedData)
		require.NoError(t, err)
		_, err = tmpFile.Write(data)
		require.NoError(t, err)
		tmpFile.Close()

		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, project model.Project) error {
				assert.Equal(t, projectKey, project.Key)
				assert.Equal(t, "test-env", project.SourceEnvironmentKey)
				assert.Len(t, project.AllFlagsState, 1)
				assert.Len(t, project.AvailableVariations, 2)
				return nil
			},
		)

		err = model.ImportProjectFromFile(ctx, projectKey, tmpFile.Name())
		require.NoError(t, err)
	})
}
