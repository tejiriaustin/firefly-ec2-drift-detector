package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	awspkg "firefly-ec2-drift-detector/aws"
	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
)

type StateProvider interface {
	GetInstanceState(ctx context.Context, instanceID string) (*models.InstanceState, error)
	GetInstanceStatesBatch(ctx context.Context, instanceIDs []string) (map[string]*models.InstanceState, error)
}

type StateParser interface {
	ParseStateFile(filepath string) (map[string]*models.InstanceState, error)
}

type DriftService struct {
	awsProvider StateProvider
	tfParser    StateParser
	comparator  models.DriftDetector
	logger      *flog.Logger
}

func NewDriftService(provider StateProvider, parser StateParser, comparator models.DriftDetector, logger *flog.Logger) *DriftService {
	return &DriftService{
		awsProvider: provider,
		tfParser:    parser,
		comparator:  comparator,
		logger:      logger,
	}
}

func (s *DriftService) DetectDrift(ctx context.Context, tfStatePath string, instanceIDs []string, attrs []string) ([]*models.DriftReport, error) {
	s.logger.Info("starting drift detection",
		zap.String("terraform_state", tfStatePath),
		zap.Strings("instance_ids", instanceIDs),
		zap.Strings("attributes", attrs),
	)

	startTime := time.Now()

	expectedStates, err := s.tfParser.ParseStateFile(tfStatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse terraform state: %w", err)
	}

	if len(instanceIDs) == 0 {
		for id := range expectedStates {
			instanceIDs = append(instanceIDs, id)
		}
		s.logger.Info("checking all instances from state file",
			zap.Int("instance_count", len(instanceIDs)),
		)
	}

	var (
		reports      []*models.DriftReport
		detectionErr error
	)

	if len(instanceIDs) > 10 {
		s.logger.Info("using batch mode for large instance count",
			zap.Int("instance_count", len(instanceIDs)),
		)
		reports, detectionErr = s.detectDriftBatch(ctx, expectedStates, instanceIDs, attrs)
	} else {
		reports, detectionErr = s.detectDriftConcurrent(ctx, expectedStates, instanceIDs, attrs)
	}

	duration := time.Since(startTime)

	if detectionErr != nil {
		s.logger.Error("drift detection encountered errors",
			zap.Duration("duration", duration),
			zap.Error(detectionErr),
		)
	}

	driftCount := 0
	for _, report := range reports {
		if report.HasDrift {
			driftCount++
		}
	}

	s.logger.Info("drift detection completed",
		zap.Duration("duration", duration),
		zap.Int("total_instances", len(reports)),
		zap.Int("instances_with_drift", driftCount),
	)

	return reports, detectionErr
}

func (s *DriftService) detectDriftBatch(ctx context.Context, expectedStates map[string]*models.InstanceState, instanceIDs []string, attrs []string) ([]*models.DriftReport, error) {
	s.logger.Info("fetching instances in batch mode",
		zap.Int("instance_count", len(instanceIDs)),
	)

	actualStates, err := s.awsProvider.GetInstanceStatesBatch(ctx, instanceIDs)
	if err != nil {
		s.logger.Warn("batch fetch encountered errors",
			zap.Error(err),
			zap.Int("successful_fetches", len(actualStates)),
		)
	}

	reports := make([]*models.DriftReport, 0, len(instanceIDs))
	var errorMessages []string

	for _, instanceID := range instanceIDs {
		expected, existsInExpected := expectedStates[instanceID]
		if !existsInExpected {
			s.logger.Warn("instance not in terraform state",
				zap.String("instance_id", instanceID),
			)
			errorMessages = append(errorMessages, fmt.Sprintf("instance %s not in terraform state", instanceID))
			continue
		}

		actual, existsInActual := actualStates[instanceID]
		if !existsInActual {
			s.logger.Warn("instance not fetched from AWS",
				zap.String("instance_id", instanceID),
			)
			errorMessages = append(errorMessages, fmt.Sprintf("instance %s not found in AWS", instanceID))
			continue
		}

		report := s.comparator.CompareAttributes(expected, actual, attrs)
		reports = append(reports, report)
	}

	if len(errorMessages) > 0 {
		summary := fmt.Sprintf("%d instance(s) failed", len(errorMessages))
		return reports, errors.New(summary)
	}

	return reports, nil
}

func (s *DriftService) detectDriftConcurrent(ctx context.Context, expectedStates map[string]*models.InstanceState, instanceIDs []string, attrs []string) ([]*models.DriftReport, error) {
	s.logger.Info("checking instances concurrently",
		zap.Int("instance_count", len(instanceIDs)),
	)

	type result struct {
		report *models.DriftReport
		err    error
	}

	results := make(chan result, len(instanceIDs))
	var wg sync.WaitGroup

	for _, instanceID := range instanceIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			expected, exists := expectedStates[id]
			if !exists {
				s.logger.Warn("instance not in terraform state",
					zap.String("instance_id", id),
				)
				results <- result{
					err: fmt.Errorf("instance %s not in terraform state", id),
				}
				return
			}

			actual, err := s.awsProvider.GetInstanceState(ctx, id)
			if err != nil {
				if awspkg.IsAuthError(err) {
					s.logger.Error("authentication error - check AWS credentials",
						zap.String("instance_id", id),
						zap.Error(err),
					)
				}
				results <- result{err: err}
				return
			}

			report := s.comparator.CompareAttributes(expected, actual, attrs)
			results <- result{report: report}
		}(instanceID)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	reports := make([]*models.DriftReport, 0, len(instanceIDs))
	var errorMessages []string
	var authErrors int

	for res := range results {
		if res.err != nil {
			if awspkg.IsAuthError(res.err) {
				authErrors++
			}
			errorMessages = append(errorMessages, res.err.Error())
		} else {
			reports = append(reports, res.report)
		}
	}

	if len(errorMessages) > 0 {
		s.logger.Warn("some instances could not be checked",
			zap.Int("error_count", len(errorMessages)),
			zap.Int("success_count", len(reports)),
			zap.Int("auth_errors", authErrors),
		)

		summary := fmt.Sprintf("%d instance(s) failed", len(errorMessages))
		if len(errorMessages) == 1 {
			summary = errorMessages[0]
		} else if authErrors > 0 {
			summary = fmt.Sprintf("%d instance(s) failed (%d authentication errors)", len(errorMessages), authErrors)
		}

		return reports, errors.New(summary)
	}

	return reports, nil
}

func (s *DriftService) DetectSingleDrift(ctx context.Context, tfStatePath, instanceID string, attrs []string) (*models.DriftReport, error) {
	s.logger.Info("detecting drift for single instance",
		zap.String("instance_id", instanceID),
		zap.Strings("attributes", attrs),
	)

	expectedStates, err := s.tfParser.ParseStateFile(tfStatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse terraform state: %w", err)
	}

	expected, exists := expectedStates[instanceID]
	if !exists {
		s.logger.Error("instance not found in terraform state",
			zap.String("instance_id", instanceID),
		)
		return nil, fmt.Errorf("instance %s not found in terraform state", instanceID)
	}

	actual, err := s.awsProvider.GetInstanceState(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return s.comparator.CompareAttributes(expected, actual, attrs), nil
}
