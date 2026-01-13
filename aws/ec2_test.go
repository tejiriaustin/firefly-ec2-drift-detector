package aws

import (
	"context"
	"errors"
	"testing"

	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// MockEC2Client implements the EC2Client interface for testing
type MockEC2Client struct {
	DescribeInstancesFunc func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

func (m *MockEC2Client) DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return m.DescribeInstancesFunc(ctx, params, optFns...)
}

// Helper function to create AWSClient with mock EC2Client
func newTestAWSClient(ec2Client EC2Client) *AWSClient {
	logger, _ := flog.NewLogger(flog.Config{
		LogLevel:    "error",
		DevMode:     false,
		ServiceName: "test",
	})
	client, _ := NewAWSClient(context.Background(), "us-east-1", ec2Client, logger)
	return client
}

func TestNewStateProvider(t *testing.T) {
	mockClient := &MockEC2Client{}
	awsClient := newTestAWSClient(mockClient)

	provider := NewStateProvider(awsClient)

	if provider == nil {
		t.Fatal("Expected NewStateProvider to return non-nil provider")
	}
}

func TestEC2StateProvider_GetInstanceState_Success(t *testing.T) {
	tests := []struct {
		name          string
		instanceID    string
		mockResponse  *ec2.DescribeInstancesOutput
		expectedState *models.InstanceState
	}{
		{
			name:       "complete instance with all attributes",
			instanceID: "i-1234567890abcdef0",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{
							{
								InstanceId:   aws.String("i-1234567890abcdef0"),
								InstanceType: types.InstanceTypeT3Medium,
								Placement: &types.Placement{
									AvailabilityZone: aws.String("us-east-1a"),
								},
								SecurityGroups: []types.GroupIdentifier{
									{GroupId: aws.String("sg-12345")},
									{GroupId: aws.String("sg-67890")},
								},
								Tags: []types.Tag{
									{Key: aws.String("Name"), Value: aws.String("test-instance")},
									{Key: aws.String("Environment"), Value: aws.String("production")},
								},
								SubnetId: aws.String("subnet-12345"),
								ImageId:  aws.String("ami-12345"),
								KeyName:  aws.String("my-key"),
								Monitoring: &types.Monitoring{
									State: types.MonitoringStateEnabled,
								},
							},
						},
					},
				},
			},
			expectedState: &models.InstanceState{
				InstanceID:       "i-1234567890abcdef0",
				InstanceType:     "t3.medium",
				AvailabilityZone: "us-east-1a",
				SecurityGroups:   []string{"sg-12345", "sg-67890"},
				Tags: map[string]string{
					"Name":        "test-instance",
					"Environment": "production",
				},
				SubnetID:   "subnet-12345",
				ImageID:    "ami-12345",
				KeyName:    "my-key",
				Monitoring: true,
			},
		},
		{
			name:       "instance with monitoring disabled",
			instanceID: "i-0987654321fedcba0",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{
							{
								InstanceId:   aws.String("i-0987654321fedcba0"),
								InstanceType: types.InstanceTypeT2Micro,
								Placement: &types.Placement{
									AvailabilityZone: aws.String("us-west-2b"),
								},
								SecurityGroups: []types.GroupIdentifier{},
								Tags:           []types.Tag{},
								SubnetId:       aws.String("subnet-67890"),
								ImageId:        aws.String("ami-67890"),
								KeyName:        aws.String("test-key"),
								Monitoring: &types.Monitoring{
									State: types.MonitoringStateDisabled,
								},
							},
						},
					},
				},
			},
			expectedState: &models.InstanceState{
				InstanceID:       "i-0987654321fedcba0",
				InstanceType:     "t2.micro",
				AvailabilityZone: "us-west-2b",
				SecurityGroups:   []string{},
				Tags:             map[string]string{},
				SubnetID:         "subnet-67890",
				ImageID:          "ami-67890",
				KeyName:          "test-key",
				Monitoring:       false,
			},
		},
		{
			name:       "instance without monitoring field",
			instanceID: "i-nomonitoring123",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{
							{
								InstanceId:   aws.String("i-nomonitoring123"),
								InstanceType: types.InstanceTypeT3Large,
								Placement: &types.Placement{
									AvailabilityZone: aws.String("us-east-1c"),
								},
								SecurityGroups: []types.GroupIdentifier{
									{GroupId: aws.String("sg-99999")},
								},
								Tags:       []types.Tag{},
								SubnetId:   aws.String("subnet-99999"),
								ImageId:    aws.String("ami-99999"),
								KeyName:    aws.String("key-99999"),
								Monitoring: nil,
							},
						},
					},
				},
			},
			expectedState: &models.InstanceState{
				InstanceID:       "i-nomonitoring123",
				InstanceType:     "t3.large",
				AvailabilityZone: "us-east-1c",
				SecurityGroups:   []string{"sg-99999"},
				Tags:             map[string]string{},
				SubnetID:         "subnet-99999",
				ImageID:          "ami-99999",
				KeyName:          "key-99999",
				Monitoring:       false,
			},
		},
		{
			name:       "instance with multiple security groups",
			instanceID: "i-multisg123",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{
							{
								InstanceId:   aws.String("i-multisg123"),
								InstanceType: types.InstanceTypeM5Xlarge,
								Placement: &types.Placement{
									AvailabilityZone: aws.String("eu-west-1a"),
								},
								SecurityGroups: []types.GroupIdentifier{
									{GroupId: aws.String("sg-web")},
									{GroupId: aws.String("sg-app")},
									{GroupId: aws.String("sg-db")},
								},
								Tags:     []types.Tag{},
								SubnetId: aws.String("subnet-private"),
								ImageId:  aws.String("ami-ubuntu"),
								KeyName:  aws.String("prod-key"),
								Monitoring: &types.Monitoring{
									State: types.MonitoringStateEnabled,
								},
							},
						},
					},
				},
			},
			expectedState: &models.InstanceState{
				InstanceID:       "i-multisg123",
				InstanceType:     "m5.xlarge",
				AvailabilityZone: "eu-west-1a",
				SecurityGroups:   []string{"sg-web", "sg-app", "sg-db"},
				Tags:             map[string]string{},
				SubnetID:         "subnet-private",
				ImageID:          "ami-ubuntu",
				KeyName:          "prod-key",
				Monitoring:       true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockEC2Client{
				DescribeInstancesFunc: func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
					if len(params.InstanceIds) != 1 || params.InstanceIds[0] != tt.instanceID {
						t.Errorf("Expected instance ID %s, got %v", tt.instanceID, params.InstanceIds)
					}
					return tt.mockResponse, nil
				},
			}

			awsClient := newTestAWSClient(mockClient)
			provider := NewStateProvider(awsClient)

			ctx := context.Background()
			state, err := provider.GetInstanceState(ctx, tt.instanceID)

			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			if state == nil {
				t.Fatal("Expected state to be non-nil")
			}

			// Verify all fields
			if state.InstanceID != tt.expectedState.InstanceID {
				t.Errorf("InstanceID: expected %s, got %s", tt.expectedState.InstanceID, state.InstanceID)
			}
			if state.InstanceType != tt.expectedState.InstanceType {
				t.Errorf("InstanceType: expected %s, got %s", tt.expectedState.InstanceType, state.InstanceType)
			}
			if state.AvailabilityZone != tt.expectedState.AvailabilityZone {
				t.Errorf("AvailabilityZone: expected %s, got %s", tt.expectedState.AvailabilityZone, state.AvailabilityZone)
			}
			if state.SubnetID != tt.expectedState.SubnetID {
				t.Errorf("SubnetID: expected %s, got %s", tt.expectedState.SubnetID, state.SubnetID)
			}
			if state.ImageID != tt.expectedState.ImageID {
				t.Errorf("ImageID: expected %s, got %s", tt.expectedState.ImageID, state.ImageID)
			}
			if state.KeyName != tt.expectedState.KeyName {
				t.Errorf("KeyName: expected %s, got %s", tt.expectedState.KeyName, state.KeyName)
			}
			if state.Monitoring != tt.expectedState.Monitoring {
				t.Errorf("Monitoring: expected %v, got %v", tt.expectedState.Monitoring, state.Monitoring)
			}

			// Verify security groups
			if len(state.SecurityGroups) != len(tt.expectedState.SecurityGroups) {
				t.Errorf("SecurityGroups length: expected %d, got %d", len(tt.expectedState.SecurityGroups), len(state.SecurityGroups))
			} else {
				for i, sg := range tt.expectedState.SecurityGroups {
					if state.SecurityGroups[i] != sg {
						t.Errorf("SecurityGroups[%d]: expected %s, got %s", i, sg, state.SecurityGroups[i])
					}
				}
			}

			// Verify tags
			if len(state.Tags) != len(tt.expectedState.Tags) {
				t.Errorf("Tags length: expected %d, got %d", len(tt.expectedState.Tags), len(state.Tags))
			} else {
				for key, expectedValue := range tt.expectedState.Tags {
					if actualValue, ok := state.Tags[key]; !ok {
						t.Errorf("Tag %s: expected to exist", key)
					} else if actualValue != expectedValue {
						t.Errorf("Tag %s: expected %s, got %s", key, expectedValue, actualValue)
					}
				}
			}
		})
	}
}

func TestEC2StateProvider_GetInstanceState_InstanceNotFound(t *testing.T) {
	tests := []struct {
		name         string
		instanceID   string
		mockResponse *ec2.DescribeInstancesOutput
	}{
		{
			name:       "empty reservations",
			instanceID: "i-notfound123",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{},
			},
		},
		{
			name:       "empty instances array",
			instanceID: "i-notfound456",
			mockResponse: &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockEC2Client{
				DescribeInstancesFunc: func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
					return tt.mockResponse, nil
				},
			}

			awsClient := newTestAWSClient(mockClient)
			provider := NewStateProvider(awsClient)

			ctx := context.Background()
			state, err := provider.GetInstanceState(ctx, tt.instanceID)

			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if state != nil {
				t.Errorf("Expected state to be nil, got %v", state)
			}

			expectedError := "not found in AWS"
			if !contains(err.Error(), expectedError) {
				t.Errorf("Expected error to contain '%s', got: %v", expectedError, err)
			}
		})
	}
}

func TestEC2StateProvider_GetInstanceState_APIError(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		mockError  error
	}{
		{
			name:       "aws api error",
			instanceID: "i-error123",
			mockError:  errors.New("AWS API error: rate limit exceeded"),
		},
		{
			name:       "network error",
			instanceID: "i-network456",
			mockError:  errors.New("connection timeout"),
		},
		{
			name:       "authentication error",
			instanceID: "i-auth789",
			mockError:  errors.New("authentication failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockEC2Client{
				DescribeInstancesFunc: func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
					return nil, tt.mockError
				},
			}

			awsClient := newTestAWSClient(mockClient)
			provider := NewStateProvider(awsClient)

			ctx := context.Background()
			state, err := provider.GetInstanceState(ctx, tt.instanceID)

			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if state != nil {
				t.Errorf("Expected state to be nil, got %v", state)
			}

			expectedError := "AWS API error"
			if !contains(err.Error(), expectedError) {
				t.Errorf("Expected error to contain '%s', got: %v", expectedError, err)
			}
		})
	}
}

func TestEC2StateProvider_ExtractSecurityGroups(t *testing.T) {
	tests := []struct {
		name     string
		groups   []types.GroupIdentifier
		expected []string
	}{
		{
			name:     "empty security groups",
			groups:   []types.GroupIdentifier{},
			expected: []string{},
		},
		{
			name: "single security group",
			groups: []types.GroupIdentifier{
				{GroupId: aws.String("sg-12345")},
			},
			expected: []string{"sg-12345"},
		},
		{
			name: "multiple security groups",
			groups: []types.GroupIdentifier{
				{GroupId: aws.String("sg-web")},
				{GroupId: aws.String("sg-app")},
				{GroupId: aws.String("sg-db")},
			},
			expected: []string{"sg-web", "sg-app", "sg-db"},
		},
		{
			name: "security groups with nil IDs",
			groups: []types.GroupIdentifier{
				{GroupId: aws.String("sg-valid")},
				{GroupId: nil},
			},
			expected: []string{"sg-valid", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockEC2Client{}
			awsClient := newTestAWSClient(mockClient)
			provider := NewStateProvider(awsClient)

			// Create a mock instance with the test security groups
			instance := types.Instance{
				InstanceId:     aws.String("i-test"),
				InstanceType:   types.InstanceTypeT2Micro,
				SecurityGroups: tt.groups,
			}

			state := provider.mapToInstanceState(instance)

			if len(state.SecurityGroups) != len(tt.expected) {
				t.Errorf("Expected %d security groups, got %d", len(tt.expected), len(state.SecurityGroups))
			}

			for i, expected := range tt.expected {
				if i >= len(state.SecurityGroups) {
					t.Errorf("Missing security group at index %d", i)
					continue
				}
				if state.SecurityGroups[i] != expected {
					t.Errorf("Security group at index %d: expected %s, got %s", i, expected, state.SecurityGroups[i])
				}
			}
		})
	}
}

func TestEC2StateProvider_ExtractTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []types.Tag
		expected map[string]string
	}{
		{
			name:     "empty tags",
			tags:     []types.Tag{},
			expected: map[string]string{},
		},
		{
			name: "single tag",
			tags: []types.Tag{
				{Key: aws.String("Name"), Value: aws.String("test-instance")},
			},
			expected: map[string]string{
				"Name": "test-instance",
			},
		},
		{
			name: "multiple tags",
			tags: []types.Tag{
				{Key: aws.String("Name"), Value: aws.String("web-server")},
				{Key: aws.String("Environment"), Value: aws.String("production")},
				{Key: aws.String("Team"), Value: aws.String("platform")},
			},
			expected: map[string]string{
				"Name":        "web-server",
				"Environment": "production",
				"Team":        "platform",
			},
		},
		{
			name: "tags with empty values",
			tags: []types.Tag{
				{Key: aws.String("Name"), Value: aws.String("")},
				{Key: aws.String("Environment"), Value: aws.String("dev")},
			},
			expected: map[string]string{
				"Name":        "",
				"Environment": "dev",
			},
		},
		{
			name: "tags with nil values",
			tags: []types.Tag{
				{Key: aws.String("Name"), Value: nil},
				{Key: aws.String("Valid"), Value: aws.String("value")},
			},
			expected: map[string]string{
				"Name":  "",
				"Valid": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockEC2Client{}
			awsClient := newTestAWSClient(mockClient)
			provider := NewStateProvider(awsClient)

			// Create a mock instance with the test tags
			instance := types.Instance{
				InstanceId:   aws.String("i-test"),
				InstanceType: types.InstanceTypeT2Micro,
				Tags:         tt.tags,
			}

			// Use mapToInstanceState which internally calls extractTags
			state := provider.mapToInstanceState(instance)

			if len(state.Tags) != len(tt.expected) {
				t.Errorf("Expected %d tags, got %d", len(tt.expected), len(state.Tags))
			}

			for key, expectedValue := range tt.expected {
				actualValue, ok := state.Tags[key]
				if !ok {
					t.Errorf("Expected tag %s to exist", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Tag %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}

			// Verify no extra tags
			for key := range state.Tags {
				if _, ok := tt.expected[key]; !ok {
					t.Errorf("Unexpected tag %s with value %s", key, state.Tags[key])
				}
			}
		})
	}
}

func TestEC2StateProvider_MapToInstanceState_NilFields(t *testing.T) {
	mockClient := &MockEC2Client{}
	awsClient := newTestAWSClient(mockClient)
	provider := NewStateProvider(awsClient)

	instance := types.Instance{
		InstanceId:     nil,
		InstanceType:   types.InstanceTypeT2Micro,
		Placement:      nil,
		SecurityGroups: nil,
		Tags:           nil,
		SubnetId:       nil,
		ImageId:        nil,
		KeyName:        nil,
		Monitoring:     nil,
	}

	state := provider.mapToInstanceState(instance)

	if state == nil {
		t.Fatal("Expected state to be non-nil")
	}

	// Verify that nil fields are handled gracefully
	if state.InstanceID != "" {
		t.Errorf("Expected empty InstanceID, got %s", state.InstanceID)
	}
	if state.AvailabilityZone != "" {
		t.Errorf("Expected empty AvailabilityZone, got %s", state.AvailabilityZone)
	}
	if state.Monitoring != false {
		t.Errorf("Expected Monitoring to be false, got %v", state.Monitoring)
	}
	if state.SecurityGroups == nil {
		t.Error("Expected SecurityGroups to be empty slice, not nil")
	}
	if state.Tags == nil {
		t.Error("Expected Tags to be empty map, not nil")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
