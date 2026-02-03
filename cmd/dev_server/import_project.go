package dev_server

import (
	"context"
	"fmt"
	"log"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

const ImportFileFlag = "file"

func NewImportProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long: `Import a project into the dev server database from a JSON file.

The JSON file format matches the output from:
  ldcli dev-server get-project --project=<key> \
    --expand=overrides --expand=availableVariations

Examples:
  # Export project data (while dev server is running)
  ldcli dev-server get-project --project=my-project \
    --expand=overrides --expand=availableVariations > backup.json

  # Later, import the project from backup
  ldcli dev-server import-project --project=my-project --file=backup.json`,
		RunE:  importProject(),
		Short: "import project from file",
		Use:   "import-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key to create")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(ImportFileFlag, "", "Path to JSON file containing project data")
	_ = cmd.MarkFlagRequired(ImportFileFlag)
	_ = cmd.Flags().SetAnnotation(ImportFileFlag, "required", []string{"true"})
	_ = viper.BindPFlag(ImportFileFlag, cmd.Flags().Lookup(ImportFileFlag))

	return cmd
}

func importProject() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		projectKey := viper.GetString(cliflags.ProjectFlag)
		filepath := viper.GetString(ImportFileFlag)

		// Get database path (same logic as dev_server.go)
		dbFilePath, err := xdg.StateFile("ldcli/dev_server.db")
		if err != nil {
			return fmt.Errorf("unable to get database path: %w", err)
		}

		// Open database
		sqlStore, err := db.NewSqlite(ctx, dbFilePath)
		if err != nil {
			return fmt.Errorf("unable to open database: %w", err)
		}

		// Set store on context
		ctx = model.ContextWithStore(ctx, sqlStore)

		// Import project from file
		err = model.ImportProjectFromFile(ctx, projectKey, filepath)
		if err != nil {
			return fmt.Errorf("unable to import project: %w", err)
		}

		log.Printf("Successfully imported project '%s' from %s", projectKey, filepath)
		fmt.Fprintf(cmd.OutOrStdout(), "Successfully imported project '%s' from %s\n", projectKey, filepath)

		return nil
	}
}
