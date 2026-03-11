package whoami

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

type callerIdentity struct {
	AccountID       string   `json:"accountId"`
	AuthKind        string   `json:"authKind"`
	ClientID        string   `json:"clientId"`
	EnvironmentID   string   `json:"environmentId"`
	EnvironmentName string   `json:"environmentName"`
	MemberID        string   `json:"memberId"`
	ProjectID       string   `json:"projectId"`
	ProjectName     string   `json:"projectName"`
	Scopes          []string `json:"scopes"`
	ServiceToken    bool     `json:"serviceToken"`
	TokenID         string   `json:"tokenId"`
	TokenKind       string   `json:"tokenKind"`
	TokenName       string   `json:"tokenName"`
}

type memberSummary struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
}

func NewWhoAmICmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  cobra.NoArgs,
		Long:  "Show information about the identity associated with the current access token.",
		RunE:  makeRequest(client),
		Short: "Show current caller identity",
		Use:   "whoami",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	// Hide flags that don't apply to whoami from its help output.
	// Access token and base URI are read from config; analytics opt-out is not relevant.
	hiddenInHelp := []string{
		cliflags.AccessTokenFlag,
		cliflags.BaseURIFlag,
		cliflags.AnalyticsOptOut,
	}
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		for _, name := range hiddenInHelp {
			if f := c.Root().PersistentFlags().Lookup(name); f != nil {
				f.Hidden = true
			}
		}
		defaultHelp(c, args)
		for _, name := range hiddenInHelp {
			if f := c.Root().PersistentFlags().Lookup(name); f != nil {
				f.Hidden = false
			}
		}
	})

	return cmd
}

func makeRequest(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		if accessToken == "" {
			return errors.NewError("no access token configured. Run `ldcli login` or set LD_ACCESS_TOKEN")
		}

		baseURI := viper.GetString(cliflags.BaseURIFlag)
		outputKind := viper.GetString(cliflags.OutputFlag)

		identityPath, _ := url.JoinPath(baseURI, "api/v2/caller-identity")
		identityRes, err := client.MakeRequest(accessToken, "GET", identityPath, "application/json", nil, nil, false)
		if err != nil {
			return output.NewCmdOutputError(err, outputKind)
		}

		// For JSON output, return the raw caller-identity response.
		if outputKind == "json" {
			out, err := output.CmdOutputSingular(outputKind, identityRes, output.ConfigPlaintextOutputFn)
			if err != nil {
				return errors.NewError(err.Error())
			}
			fmt.Fprint(cmd.OutOrStdout(), out+"\n")
			return nil
		}

		var identity callerIdentity
		if err := json.Unmarshal(identityRes, &identity); err != nil {
			return errors.NewError(err.Error())
		}

		// Fetch member info for a richer plaintext display.
		var member *memberSummary
		if identity.MemberID != "" {
			memberPath, _ := url.JoinPath(baseURI, "api/v2/members", identity.MemberID)
			memberRes, err := client.MakeRequest(accessToken, "GET", memberPath, "application/json", nil, nil, false)
			if err == nil {
				var m memberSummary
				if json.Unmarshal(memberRes, &m) == nil {
					member = &m
				}
			}
		}

		fmt.Fprint(cmd.OutOrStdout(), formatPlaintext(identity, member)+"\n")
		return nil
	}
}

func formatPlaintext(identity callerIdentity, member *memberSummary) string {
	var sb strings.Builder

	if member != nil {
		name := strings.TrimSpace(member.FirstName + " " + member.LastName)
		if name != "" {
			fmt.Fprintf(&sb, "%s <%s>\n", name, member.Email)
		} else {
			fmt.Fprintf(&sb, "%s\n", member.Email)
		}
		fmt.Fprintf(&sb, "Role:    %s\n", member.Role)
	}

	tokenKind := identity.TokenKind
	if identity.ServiceToken {
		tokenKind = "service token"
	}
	if identity.TokenName != "" {
		fmt.Fprintf(&sb, "Token:   %s (%s)\n", identity.TokenName, tokenKind)
	} else if identity.ClientID != "" {
		fmt.Fprintf(&sb, "Token:   %s (%s)\n", identity.ClientID, tokenKind)
	}

	if identity.AccountID != "" {
		fmt.Fprintf(&sb, "Account: %s\n", identity.AccountID)
	}

	return strings.TrimRight(sb.String(), "\n")
}
