package models

import (
	"fmt"
	"reflect"

	"go.uber.org/zap"

	flog "firefly-ec2-drift-detector/logger"
)

const (
	DriftTypeValueMismatch DriftType = "VALUE_MISMATCH"
	DriftTypeMissing       DriftType = "MISSING_IN_ACTUAL"
	DriftTypeExtra         DriftType = "EXTRA_IN_ACTUAL"
)

type (
	InstanceState struct {
		InstanceID       string            // "i-1234567890abcdef0"
		InstanceType     string            // "t3.medium"
		AvailabilityZone string            // "us-east-1a"
		SecurityGroups   []string          // ["sg-12345", "sg-67890"]
		Tags             map[string]string // {"Name": "web", "Env": "prod"}
		SubnetID         string
		ImageID          string
		KeyName          string
		Monitoring       bool
	}

	AttributeDrift struct {
		AttributeName string
		ExpectedValue interface{}
		ActualValue   interface{}
		DriftType     DriftType
	}

	DriftType string

	DriftReport struct {
		InstanceID   string
		HasDrift     bool
		Drifts       []AttributeDrift
		CheckedAttrs []string
	}
)

func (d *DriftReport) AddDrift(attr string, expected, actual interface{}, driftType DriftType) {
	d.Drifts = append(d.Drifts, AttributeDrift{
		AttributeName: attr,
		ExpectedValue: expected,
		ActualValue:   actual,
		DriftType:     driftType,
	})
	d.HasDrift = true
}

func (d *DriftReport) Summary() string {
	if !d.HasDrift {
		return fmt.Sprintf("Instance %s: No drift detected", d.InstanceID)
	}
	return fmt.Sprintf("Instance %s: Drift detected in %d attribute(s)", d.InstanceID, len(d.Drifts))
}

type DriftDetector interface {
	CompareAttributes(expected, actual *InstanceState, attrs []string) *DriftReport
}

type AttributeComparator struct {
	logger *flog.Logger
}

func NewAttributeComparator(logger *flog.Logger) *AttributeComparator {
	return &AttributeComparator{
		logger: logger,
	}
}

func (c *AttributeComparator) CompareAttributes(expected, actual *InstanceState, attrs []string) *DriftReport {
	c.logger.Info("starting attribute comparison",
		zap.String("instance_id", actual.InstanceID),
		zap.Strings("attributes", attrs),
	)

	report := &DriftReport{
		InstanceID:   actual.InstanceID,
		HasDrift:     false,
		Drifts:       []AttributeDrift{},
		CheckedAttrs: attrs,
	}

	for _, attr := range attrs {
		c.compareAttribute(attr, expected, actual, report)
	}

	if report.HasDrift {
		c.logger.Warn("drift detected",
			zap.String("instance_id", actual.InstanceID),
			zap.Int("drift_count", len(report.Drifts)),
		)
	} else {
		c.logger.Info("no drift detected",
			zap.String("instance_id", actual.InstanceID),
		)
	}

	return report
}

func (c *AttributeComparator) compareAttribute(attr string, expected, actual *InstanceState, report *DriftReport) {
	expectedVal := c.getAttributeValue(attr, expected)
	actualVal := c.getAttributeValue(attr, actual)

	if !c.areEqual(expectedVal, actualVal) {
		c.logger.Debug("drift found in attribute",
			zap.String("attribute", attr),
			zap.Any("expected", expectedVal),
			zap.Any("actual", actualVal),
		)
		report.AddDrift(attr, expectedVal, actualVal, DriftTypeValueMismatch)
	}
}

func (c *AttributeComparator) getAttributeValue(attr string, state *InstanceState) interface{} {
	v := reflect.ValueOf(state).Elem()
	field := v.FieldByName(attr)

	if !field.IsValid() {
		c.logger.Warn("invalid attribute name",
			zap.String("attribute", attr),
		)
		return nil
	}

	return field.Interface()
}

func (c *AttributeComparator) areEqual(expected, actual interface{}) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		return false
	}

	switch exp := expected.(type) {
	case []string:
		act, ok := actual.([]string)
		if !ok {
			return false
		}
		return c.compareStringSlices(exp, act)
	case map[string]string:
		act, ok := actual.(map[string]string)
		if !ok {
			return false
		}
		return c.compareMaps(exp, act)
	default:
		return reflect.DeepEqual(expected, actual)
	}
}

func (c *AttributeComparator) compareStringSlices(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}

	expMap := make(map[string]bool)
	for _, v := range expected {
		expMap[v] = true
	}

	for _, v := range actual {
		if !expMap[v] {
			return false
		}
	}

	return true
}

func (c *AttributeComparator) compareMaps(expected, actual map[string]string) bool {
	if len(expected) != len(actual) {
		return false
	}

	for k, v := range expected {
		if actual[k] != v {
			return false
		}
	}

	return true
}
