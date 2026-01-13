package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"

	"firefly-ec2-drift-detector/models"
)

type (
	EC2Client interface {
		DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	}
)

type EC2StateProvider struct {
	client *AWSClient
}

func NewStateProvider(client *AWSClient) *EC2StateProvider {
	return &EC2StateProvider{
		client: client,
	}
}

func (p *EC2StateProvider) GetInstanceState(ctx context.Context, instanceID string) (*models.InstanceState, error) {
	p.client.logger.Info("fetching instance state from AWS",
		zap.String("instance_id", instanceID),
	)

	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}

	result, err := p.client.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		p.client.logger.Error("failed to fetch instance from AWS",
			zap.String("instance_id", instanceID),
		)
		return nil, fmt.Errorf("AWS API error for instance %s", instanceID)
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		p.client.logger.Warn("instance not found in AWS",
			zap.String("instance_id", instanceID),
		)
		return nil, fmt.Errorf("instance %s not found in AWS", instanceID)
	}

	instance := result.Reservations[0].Instances[0]
	state := p.mapToInstanceState(instance)

	p.client.logger.Info("successfully retrieved instance state",
		zap.String("instance_id", instanceID),
		zap.String("instance_type", state.InstanceType),
	)

	return state, nil
}

func (p *EC2StateProvider) mapToInstanceState(instance types.Instance) *models.InstanceState {
	if instance.Placement == nil {
		instance.Placement = &types.Placement{}
	}
	state := &models.InstanceState{
		InstanceID:       aws.ToString(instance.InstanceId),
		InstanceType:     string(instance.InstanceType),
		AvailabilityZone: aws.ToString(instance.Placement.AvailabilityZone),
		SecurityGroups:   p.extractSecurityGroups(instance.SecurityGroups),
		Tags:             p.extractTags(instance.Tags),
		SubnetID:         aws.ToString(instance.SubnetId),
		ImageID:          aws.ToString(instance.ImageId),
		KeyName:          aws.ToString(instance.KeyName),
	}

	if instance.Monitoring != nil {
		state.Monitoring = instance.Monitoring.State == types.MonitoringStateEnabled
	}

	return state
}

func (p *EC2StateProvider) extractSecurityGroups(groups []types.GroupIdentifier) []string {
	result := make([]string, 0, len(groups))
	for _, g := range groups {
		result = append(result, aws.ToString(g.GroupId))
	}
	return result
}

func (p *EC2StateProvider) extractTags(tags []types.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}
