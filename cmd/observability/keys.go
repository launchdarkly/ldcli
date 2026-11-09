package observability

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/observability"
)

const (
	keyFlag    = "key"
	searchFlag = "search"
)

func newKeysCmd(clients Clients) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "List the attribute keys of a product type, or the values of one key",
		Long: `List the attribute keys that the records of a product type have in a time range, with the number of records for each key.

Use the keys to write the --query, --group-by, and --column values of the other commands. Compare the counts to find the correct key, for example service_name and not serviceName.

To list the values of one key, most frequent first, use --key. Service names often have an environment prefix, so list the values before you filter on them.`,
		Example: `  ldcli observability keys --project default --product-type logs
  ldcli observability keys --project default --product-type logs --search service
  ldcli observability keys --project default --product-type logs --key service_name --search gonfalon`,
		Args: validators.Validate(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeys(cmd, clients)
		},
	}

	addTimeRangeFlags(cmd)
	cmd.Flags().String(productTypeFlag, "", "Product type: "+productTypeNames)
	_ = cmd.MarkFlagRequired(productTypeFlag)
	_ = cmd.Flags().SetAnnotation(productTypeFlag, "required", []string{"true"})
	cmd.Flags().String(keyFlag, "", "List the values of this key, for example service_name, instead of the keys")
	cmd.Flags().String(searchFlag, "", "Only list keys with this text in the name. With --key, only list values with this text")
	cmd.Flags().Int(limitFlag, 25, fmt.Sprintf("Number of keys or values to return, at most %d", maxLimit))

	return cmd
}

func runKeys(cmd *cobra.Command, clients Clients) error {
	productTypeValue, _ := cmd.Flags().GetString(productTypeFlag)
	productType, ok := productTypes[strings.ToLower(productTypeValue)]
	if !ok {
		return fmt.Errorf("--%s must be one of: %s", productTypeFlag, productTypeNames)
	}
	limit, _ := cmd.Flags().GetInt(limitFlag)
	if limit < 1 || limit > maxLimit {
		return fmt.Errorf("--%s must be from 1 to %d", limitFlag, maxLimit)
	}
	key, _ := cmd.Flags().GetString(keyFlag)
	key = strings.TrimSpace(key)
	search, _ := cmd.Flags().GetString(searchFlag)
	dr, err := dateRange(cmd, time.Now())
	if err != nil {
		return err
	}

	t, err := resolveTarget(cmd, clients.Resources)
	if err != nil {
		return err
	}

	variables := map[string]any{
		"project_id": t.projectID,
		"date_range": dr,
		"count":      limit,
	}
	if search != "" {
		variables["query"] = search
	}
	var query, field string
	if key != "" {
		query, field = valueSuggestionsQuery, "value_suggestions"
		variables["product_type"] = productType
		variables["key_name"] = key
	} else {
		k := keysQueries[productType]
		query, field = k.query, k.field
		if field == "keys" {
			variables["product_type"] = productType
		}
	}

	data, err := clients.Observability.Query(cmd.Context(), observability.Request{
		AccessToken: t.accessToken,
		Endpoint:    t.endpoint,
		ProjectID:   t.projectID,
		Query:       query,
		Variables:   variables,
	})
	if err != nil {
		return err
	}
	var result map[string][]struct {
		Name  string  `json:"name,omitempty"`
		Type  string  `json:"type,omitempty"`
		Value string  `json:"value,omitempty"`
		Count float64 `json:"count"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decoding keys: %w", err)
	}
	items := result[field]

	if wantsJSON(cmd) {
		if key != "" {
			return writeJSON(cmd, map[string]any{"key": key, "values": items})
		}
		return writeJSON(cmd, map[string]any{"keys": items})
	}

	out := cmd.OutOrStdout()
	if len(items) == 0 {
		if key != "" {
			fmt.Fprintf(out, "No values found for key %q. The key name can be wrong, or the records in this time range do not have the key. Use 'ldcli observability keys --%s %s --%s <part of the key>' to find the key.\n", key, productTypeFlag, productTypeValue, searchFlag)
		} else {
			fmt.Fprintln(out, "No keys found. Try a longer time range or another --search value.")
		}
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if key != "" {
		fmt.Fprintln(w, "VALUE\tCOUNT")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\n", oneLine(item.Value), formatNumber(item.Count))
		}
	} else {
		fmt.Fprintln(w, "KEY\tTYPE\tCOUNT")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\n", item.Name, item.Type, formatNumber(item.Count))
		}
	}

	return w.Flush()
}
