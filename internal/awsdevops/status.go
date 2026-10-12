package awsdevops

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// Status is a read-only view of what setup has provisioned so far.
type Status struct {
	AccountID    string             `json:"accountId"`
	Region       string             `json:"region"`
	Roles        []RoleStatus       `json:"roles"`
	Services     []ServiceStatus    `json:"services,omitempty"`
	AgentSpaces  []AgentSpaceStatus `json:"agentSpaces"`
	MissingSetup []string           `json:"missingSetup,omitempty"`
}

type RoleStatus struct {
	Name   string `json:"name"`
	ARN    string `json:"arn,omitempty"`
	Exists bool   `json:"exists"`
}

type ServiceStatus struct {
	ServiceID   string `json:"serviceId"`
	ServiceType string `json:"serviceType"`
	Name        string `json:"name,omitempty"`
}

type AgentSpaceStatus struct {
	AgentSpaceID   string              `json:"agentSpaceId"`
	Name           string              `json:"name"`
	OperatorAppURL string              `json:"operatorAppUrl"`
	Associations   []AssociationStatus `json:"associations,omitempty"`
}

type AssociationStatus struct {
	AssociationID string `json:"associationId"`
	ServiceID     string `json:"serviceId"`
	Status        string `json:"status,omitempty"`
}

// GetStatus reports the IAM roles and agent spaces in the caller's account. It
// inspects one agent space when agentSpaceID is set, and every space otherwise.
func GetStatus(ctx context.Context, clients Clients, agentSpaceID string) (Status, error) {
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return Status{}, err
	}

	status := Status{AccountID: accountID, Region: clients.Region}
	for _, name := range []string{AgentSpaceRoleName, OperatorAppRoleName} {
		role, err := clients.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
		if err != nil {
			status.Roles = append(status.Roles, RoleStatus{Name: name})
			status.MissingSetup = append(status.MissingSetup, fmt.Sprintf("IAM role %s does not exist", name))

			continue
		}
		status.Roles = append(status.Roles, RoleStatus{
			Name:   name,
			ARN:    aws.ToString(role.Role.Arn),
			Exists: true,
		})
	}

	services, err := listServices(ctx, clients)
	if err != nil {
		return status, err
	}
	for _, service := range services {
		status.Services = append(status.Services, ServiceStatus{
			ServiceID:   aws.ToString(service.ServiceId),
			ServiceType: string(service.ServiceType),
			Name:        serviceName(service),
		})
	}

	spaceIDs := []string{agentSpaceID}
	if agentSpaceID == "" {
		spaces, err := listAgentSpaces(ctx, clients)
		if err != nil {
			return status, err
		}
		spaceIDs = nil
		for _, space := range spaces {
			spaceIDs = append(spaceIDs, aws.ToString(space.AgentSpaceId))
		}
	}

	for _, id := range spaceIDs {
		space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(id)})
		if err != nil {
			return status, fmt.Errorf("unable to read agent space %s: %w", id, err)
		}
		spaceStatus := AgentSpaceStatus{
			AgentSpaceID:   id,
			Name:           aws.ToString(space.AgentSpace.Name),
			OperatorAppURL: OperatorAppURL(id),
		}

		associations, err := listAssociations(ctx, clients, id)
		if err != nil {
			return status, err
		}
		for _, association := range associations {
			spaceStatus.Associations = append(spaceStatus.Associations, AssociationStatus{
				AssociationID: aws.ToString(association.AssociationId),
				ServiceID:     aws.ToString(association.ServiceId),
				Status:        string(association.Status),
			})
		}

		status.AgentSpaces = append(status.AgentSpaces, spaceStatus)
	}

	return status, nil
}
