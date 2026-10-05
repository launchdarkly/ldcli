package awsdevops

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// TeardownOptions narrows what to remove: only AgentSpaceID and ServiceIDs do
// that, while DeleteRoles adds the IAM roles to a narrowed teardown.
// With no selector set, Teardown removes everything setup can create. The
// agent space has to be emptied before it can be deleted, so assets and
// associations always go first.
type TeardownOptions struct {
	AgentSpaceID string
	ServiceIDs   []string
	DeleteRoles  bool

	Logf func(format string, args ...any)
}

// RemovesEverything reports whether nothing narrows the teardown, in which
// case it takes every agent space, registered service and role in the account.
func (o TeardownOptions) RemovesEverything() bool {
	return o.AgentSpaceID == "" && len(o.ServiceIDs) == 0
}

// Teardown removes the resources Setup created: every association and asset in
// an agent space, the agent space itself, the registered services, and the IAM
// roles. Options narrow that; with none it covers the whole account.
func Teardown(ctx context.Context, clients Clients, opts TeardownOptions) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	all := opts.RemovesEverything()

	agentSpaceIDs := []string{}
	if opts.AgentSpaceID != "" {
		agentSpaceIDs = append(agentSpaceIDs, opts.AgentSpaceID)
	}
	if all {
		var err error
		if agentSpaceIDs, err = listAgentSpaceIDs(ctx, clients); err != nil {
			return err
		}
	}

	for _, agentSpaceID := range agentSpaceIDs {
		if err := teardownAgentSpace(ctx, clients, agentSpaceID, logf); err != nil {
			return err
		}
	}

	serviceIDs := opts.ServiceIDs
	if all {
		registered, err := listServiceIDs(ctx, clients)
		if err != nil {
			return err
		}
		serviceIDs = registered
	}

	for _, serviceID := range serviceIDs {
		if _, err := DisassociateEverywhere(ctx, clients, serviceID, logf); err != nil {
			return err
		}
		if _, err := clients.Agent.DeregisterService(ctx, &devopsagent.DeregisterServiceInput{
			ServiceId: aws.String(serviceID),
		}); err != nil {
			return fmt.Errorf("unable to deregister service %s: %w", serviceID, err)
		}
		logf("Deregistered service %s", serviceID)
	}

	if opts.DeleteRoles || all {
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

// DisassociateEverywhere removes a service from every agent space that uses
// it, which AWS requires before the service can be deregistered. It returns
// the agent spaces it was removed from, so a caller re-registering the service
// can reconnect them.
func DisassociateEverywhere(
	ctx context.Context,
	clients Clients,
	serviceID string,
	logf func(string, ...any),
) ([]string, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}

	var detached []string
	for _, space := range spaces {
		agentSpaceID := aws.ToString(space.AgentSpaceId)
		associationID, err := FindAssociation(ctx, clients, agentSpaceID, serviceID)
		if err != nil {
			return nil, err
		}
		if associationID == "" {
			continue
		}
		if _, err := clients.Agent.DisassociateService(ctx, &devopsagent.DisassociateServiceInput{
			AgentSpaceId:  aws.String(agentSpaceID),
			AssociationId: aws.String(associationID),
		}); err != nil {
			return nil, fmt.Errorf("unable to disassociate service %s from agent space %s: %w", serviceID, agentSpaceID, err)
		}
		logf("Disassociated service %s from agent space %s", serviceID, agentSpaceID)
		detached = append(detached, agentSpaceID)
	}

	return detached, nil
}

func teardownAgentSpace(ctx context.Context, clients Clients, agentSpaceID string, logf func(string, ...any)) error {
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return err
	}
	for _, association := range associations {
		if _, err := clients.Agent.DisassociateService(ctx, &devopsagent.DisassociateServiceInput{
			AgentSpaceId:  aws.String(agentSpaceID),
			AssociationId: association.AssociationId,
		}); err != nil {
			return fmt.Errorf("unable to disassociate %s: %w", aws.ToString(association.ServiceId), err)
		}
		logf("Disassociated %s", aws.ToString(association.ServiceId))
	}

	assets, err := listAssets(ctx, clients, agentSpaceID)
	if err != nil {
		return err
	}
	for _, asset := range sortAssetsForDeletion(assets) {
		if _, err := clients.Agent.DeleteAsset(ctx, &devopsagent.DeleteAssetInput{
			AgentSpaceId: aws.String(agentSpaceID),
			AssetId:      asset.AssetId,
		}); err != nil {
			return fmt.Errorf("unable to delete asset %s: %w", aws.ToString(asset.AssetId), err)
		}
		logf("Deleted asset %s", aws.ToString(asset.AssetId))
	}

	if _, err := clients.Agent.DeleteAgentSpace(ctx, &devopsagent.DeleteAgentSpaceInput{
		AgentSpaceId: aws.String(agentSpaceID),
	}); err != nil {
		return fmt.Errorf("unable to delete agent space %s: %w", agentSpaceID, err)
	}
	logf("Deleted agent space %s", agentSpaceID)

	return nil
}

func listAgentSpaceIDs(ctx context.Context, clients Clients) ([]string, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(spaces))
	for _, space := range spaces {
		ids = append(ids, aws.ToString(space.AgentSpaceId))
	}

	return ids, nil
}

func listServiceIDs(ctx context.Context, clients Clients) ([]string, error) {
	services, err := listServices(ctx, clients)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, aws.ToString(service.ServiceId))
	}

	return ids, nil
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
