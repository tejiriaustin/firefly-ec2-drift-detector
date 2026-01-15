package aws

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"

	"firefly-ec2-drift-detector/models"
)

const (
	maxBatchSize       = 1000
	maxRetries         = 5
	initialBackoff     = 1 * time.Second
	maxBackoff         = 32 * time.Second
	rateLimitPerSecond = 10
)

type (
	EC2Client interface {
		DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	}

	EC2Error struct {
		InstanceID  string
		Err         error
		IsRetryable bool
		ErrorType   EC2ErrorType
	}

	EC2ErrorType string
)

const (
	ErrorTypeThrottling     EC2ErrorType = "THROTTLING"
	ErrorTypeAuthentication EC2ErrorType = "AUTHENTICATION"
	ErrorTypeNotFound       EC2ErrorType = "NOT_FOUND"
	ErrorTypeNetwork        EC2ErrorType = "NETWORK"
	ErrorTypeUnknown        EC2ErrorType = "UNKNOWN"
)

func (e *EC2Error) Error() string {
	return fmt.Sprintf("EC2 error for instance %s [%s]: %v", e.InstanceID, e.ErrorType, e.Err)
}

type EC2StateProvider struct {
	client      *AWSClient
	rateLimiter *time.Ticker
}

func NewStateProvider(client *AWSClient) *EC2StateProvider {
	return &EC2StateProvider{
		client:      client,
		rateLimiter: time.NewTicker(time.Second / rateLimitPerSecond),
	}
}

func (p *EC2StateProvider) GetInstanceState(ctx context.Context, instanceID string) (*models.InstanceState, error) {
	p.client.logger.Info("fetching instance state from AWS",
		zap.String("instance_id", instanceID),
	)

	var lastErr error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.client.logger.Info("retrying instance fetch",
				zap.String("instance_id", instanceID),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)

			select {
			case <-ctx.Done():
				return nil, &EC2Error{
					InstanceID:  instanceID,
					Err:         ctx.Err(),
					IsRetryable: false,
					ErrorType:   ErrorTypeNetwork,
				}
			case <-time.After(backoff):
			}
		}

		<-p.rateLimiter.C

		state, err := p.fetchInstanceState(ctx, instanceID)
		if err == nil {
			return state, nil
		}

		ec2Err, ok := err.(*EC2Error)
		if !ok {
			ec2Err = classifyError(instanceID, err)
		}

		lastErr = ec2Err

		if !ec2Err.IsRetryable {
			p.client.logger.Error("non-retryable error fetching instance",
				zap.String("instance_id", instanceID),
				zap.String("error_type", string(ec2Err.ErrorType)),
				zap.Error(ec2Err.Err),
			)
			return nil, ec2Err
		}

		p.client.logger.Warn("retryable error fetching instance",
			zap.String("instance_id", instanceID),
			zap.String("error_type", string(ec2Err.ErrorType)),
			zap.Error(ec2Err.Err),
		)

		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	p.client.logger.Error("max retries exceeded",
		zap.String("instance_id", instanceID),
		zap.Int("attempts", maxRetries+1),
	)

	return nil, &EC2Error{
		InstanceID:  instanceID,
		Err:         fmt.Errorf("max retries exceeded: %w", lastErr),
		IsRetryable: false,
		ErrorType:   ErrorTypeUnknown,
	}
}

func (p *EC2StateProvider) fetchInstanceState(ctx context.Context, instanceID string) (*models.InstanceState, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}

	result, err := p.client.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, classifyError(instanceID, err)
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		p.client.logger.Warn("instance not found in AWS",
			zap.String("instance_id", instanceID),
		)
		return nil, &EC2Error{
			InstanceID:  instanceID,
			Err:         fmt.Errorf("instance not found in AWS"),
			IsRetryable: false,
			ErrorType:   ErrorTypeNotFound,
		}
	}

	instance := result.Reservations[0].Instances[0]
	state := p.mapToInstanceState(instance)

	p.client.logger.Info("successfully retrieved instance state",
		zap.String("instance_id", instanceID),
		zap.String("instance_type", state.InstanceType),
	)

	return state, nil
}

func (p *EC2StateProvider) GetInstanceStatesBatch(ctx context.Context, instanceIDs []string) (map[string]*models.InstanceState, error) {
	p.client.logger.Info("fetching instance states in batches",
		zap.Int("total_instances", len(instanceIDs)),
	)

	var (
		allErrors []error
		states    = make(map[string]*models.InstanceState)
	)

	for i := 0; i < len(instanceIDs); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(instanceIDs) {
			end = len(instanceIDs)
		}

		batch := instanceIDs[i:end]

		p.client.logger.Debug("processing batch",
			zap.Int("batch_start", i),
			zap.Int("batch_end", end),
			zap.Int("batch_size", len(batch)),
		)

		batchStates, err := p.fetchInstanceStatesBatch(ctx, batch)
		if err != nil {
			allErrors = append(allErrors, err)
			continue
		}

		for id, state := range batchStates {
			states[id] = state
		}
	}

	if len(allErrors) > 0 {
		return states, fmt.Errorf("batch fetch encountered %d error(s)", len(allErrors))
	}

	return states, nil
}

func (p *EC2StateProvider) fetchInstanceStatesBatch(ctx context.Context, instanceIDs []string) (map[string]*models.InstanceState, error) {
	<-p.rateLimiter.C

	input := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}

	result, err := p.client.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, classifyError("batch", err)
	}

	states := make(map[string]*models.InstanceState)
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			state := p.mapToInstanceState(instance)
			states[state.InstanceID] = state
		}
	}

	return states, nil
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

func classifyError(instanceID string, err error) *EC2Error {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	errStrLower := strings.ToLower(errStr)

	ec2Err := &EC2Error{
		InstanceID: instanceID,
		Err:        err,
	}

	switch {
	case strings.Contains(errStrLower, "throttling") ||
		strings.Contains(errStrLower, "requestlimitexceeded") ||
		strings.Contains(errStrLower, "too many requests"):
		ec2Err.ErrorType = ErrorTypeThrottling
		ec2Err.IsRetryable = true

	case strings.Contains(errStrLower, "authfailure") ||
		strings.Contains(errStrLower, "unauthorizedoperation") ||
		strings.Contains(errStrLower, "access denied") ||
		strings.Contains(errStrLower, "validate the provided access credentials"):
		ec2Err.ErrorType = ErrorTypeAuthentication
		ec2Err.IsRetryable = false

	case strings.Contains(errStrLower, "does not exist") ||
		strings.Contains(errStrLower, "notfound") ||
		strings.Contains(errStrLower, "invalidinstanceid"):
		ec2Err.ErrorType = ErrorTypeNotFound
		ec2Err.IsRetryable = false

	case strings.Contains(errStrLower, "timeout") ||
		strings.Contains(errStrLower, "connection") ||
		strings.Contains(errStrLower, "network"):
		ec2Err.ErrorType = ErrorTypeNetwork
		ec2Err.IsRetryable = true

	default:
		ec2Err.ErrorType = ErrorTypeUnknown
		ec2Err.IsRetryable = false
	}

	return ec2Err
}

func IsAuthError(err error) bool {
	var ec2Err *EC2Error
	if errors.As(err, &ec2Err) {
		return ec2Err.ErrorType == ErrorTypeAuthentication
	}
	return false
}
