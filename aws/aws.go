package aws

import (
	"context"
	flog "firefly-ec2-drift-detector/logger"
)

type AWSClient struct {
	_         struct{}
	region    string
	logger    *flog.Logger
	ec2Client EC2Client
}

func NewAWSClient(ctx context.Context, region string, ec2Client EC2Client, logger *flog.Logger) (*AWSClient, error) {
	return &AWSClient{
		region:    region,
		logger:    logger,
		ec2Client: ec2Client,
	}, nil
}
