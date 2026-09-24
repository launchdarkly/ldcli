package awsdevops

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// TeardownOptions describes what to remove. The agent space has to be emptied
// before it can be deleted, so assets and associations always go first.
type TeardownOptions struct {
	AgentSpaceID string
	ServiceIDs   []string
	DeleteRoles  bool

	Logf func(format string, args ...any)
}

// Teardown removes the resources Setup created: every association and asset in
// the agent space, the agent space itself, the registered services, and
// optionally the IAM roles.
func Teardown(ctx context.Context, clients Clients, opts TeardownOptions) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if opts.AgentSpaceID == "" && !opts.DeleteRoles && len(opts.ServiceIDs) == 0 {
		return errors.New("nothing to tear down: pass --agent-space-id, --service-id or --delete-roles")
	}

	if opts.AgentSpaceID != "" {
		associations, err := clients.Agent.ListAssociations(ctx, &devopsagent.ListAssociationsInput{
			AgentSpaceId: aws.String(opts.AgentSpaceID),
		})
		if err != nil {
			return fmt.Errorf("unable to list associations for agent space %s: %w", opts.AgentSpaceID, err)
		}
		for _, association := range associations.Associations {
			if _, err := clients.Agent.DisassociateService(ctx, &devopsagent.DisassociateServiceInput{
				AgentSpaceId:  aws.String(opts.AgentSpaceID),
				AssociationId: association.AssociationId,
			}); err != nil {
				return fmt.Errorf("unable to disassociate %s: %w", aws.ToString(association.ServiceId), err)
			}
			logf("Disassociated %s", aws.ToString(association.ServiceId))
		}

		assets, err := clients.Agent.ListAssets(ctx, &devopsagent.ListAssetsInput{
			AgentSpaceId: aws.String(opts.AgentSpaceID),
		})
		if err != nil {
			return fmt.Errorf("unable to list assets for agent space %s: %w", opts.AgentSpaceID, err)
		}
		for _, asset := range sortAssetsForDeletion(assets.Items) {
			if _, err := clients.Agent.DeleteAsset(ctx, &devopsagent.DeleteAssetInput{
				AgentSpaceId: aws.String(opts.AgentSpaceID),
				AssetId:      asset.AssetId,
			}); err != nil {
				return fmt.Errorf("unable to delete asset %s: %w", aws.ToString(asset.AssetId), err)
			}
			logf("Deleted asset %s", aws.ToString(asset.AssetId))
		}

		if _, err := clients.Agent.DeleteAgentSpace(ctx, &devopsagent.DeleteAgentSpaceInput{
			AgentSpaceId: aws.String(opts.AgentSpaceID),
		}); err != nil {
			return fmt.Errorf("unable to delete agent space %s: %w", opts.AgentSpaceID, err)
		}
		logf("Deleted agent space %s", opts.AgentSpaceID)
	}

	for _, serviceID := range opts.ServiceIDs {
		if _, err := clients.Agent.DeregisterService(ctx, &devopsagent.DeregisterServiceInput{
			ServiceId: aws.String(serviceID),
		}); err != nil {
			return fmt.Errorf("unable to deregister service %s: %w", serviceID, err)
		}
		logf("Deregistered service %s", serviceID)
	}

	if opts.DeleteRoles {
		if err := deleteRole(ctx, clients.IAM, AgentSpaceRoleName, AgentSpacePolicyARN, ServiceLinkedRolePolicyName); err != nil {
			return err
		}
		logf("Deleted role %s", AgentSpaceRoleName)
		if err := deleteRole(ctx, clients.IAM, OperatorAppRoleName, OperatorAppPolicyARN, ""); err != nil {
			return err
		}
		logf("Deleted role %s", OperatorAppRoleName)
	}

	return nil
}

// sortAssetsForDeletion puts memory stores last, since AWS refuses to delete a
// store that still holds memory files.
func sortAssetsForDeletion(assets []agenttypes.Asset) []agenttypes.Asset {
	ordered := make([]agenttypes.Asset, 0, len(assets))
	var stores []agenttypes.Asset
	for _, asset := range assets {
		if aws.ToString(asset.AssetType) == memoryStoreAssetType {
			stores = append(stores, asset)

			continue
		}
		ordered = append(ordered, asset)
	}

	return append(ordered, stores...)
}

func deleteRole(ctx context.Context, client IAMAPI, name, policyARN, inlinePolicyName string) error {
	if _, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
		RoleName:  aws.String(name),
		PolicyArn: aws.String(policyARN),
	}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("unable to detach %s from %s: %w", policyARN, name, err)
	}
	if inlinePolicyName != "" {
		if _, err := client.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   aws.String(name),
			PolicyName: aws.String(inlinePolicyName),
		}); err != nil && !isNoSuchEntity(err) {
			return fmt.Errorf("unable to delete the %s policy on %s: %w", inlinePolicyName, name, err)
		}
	}
	if _, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String(name)}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("unable to delete the %s role: %w", name, err)
	}

	return nil
}
