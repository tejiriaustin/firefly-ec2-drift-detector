package models

import (
	"fmt"
	"reflect"
	"sort"

	"go.uber.org/zap"

	flog "firefly-ec2-drift-detector/logger"
)

const (
	DriftTypeValueMismatch      DriftType = "VALUE_MISMATCH"
	DriftTypeMissingInstance    DriftType = "MISSING_IN_INSTANCE"
	DriftTypeExtraInInstance    DriftType = "EXTRA_IN_INSTANCE"
	DriftTypeMissingInTerraform DriftType = "MISSING_IN_TERRAFORM"
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
		Details       string
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

func (d *DriftReport) AddDriftWithDetails(attr string, expected, actual interface{}, driftType DriftType, details string) {
	d.Drifts = append(d.Drifts, AttributeDrift{
		AttributeName: attr,
		ExpectedValue: expected,
		ActualValue:   actual,
		DriftType:     driftType,
		Details:       details,
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
		driftType, details := c.determineDriftType(expectedVal, actualVal)

		c.logger.Debug("drift found in attribute",
			zap.String("attribute", attr),
			zap.Any("expected", expectedVal),
			zap.Any("actual", actualVal),
			zap.String("drift_type", string(driftType)),
		)

		if details != "" {
			report.AddDriftWithDetails(attr, expectedVal, actualVal, driftType, details)
		} else {
			report.AddDrift(attr, expectedVal, actualVal, driftType)
		}
	}
}

func (c *AttributeComparator) determineDriftType(expected, actual interface{}) (DriftType, string) {
	if expected == nil && actual != nil {
		return DriftTypeMissingInTerraform, "attribute present in instance but not in terraform"
	}

	if expected != nil && actual == nil {
		return DriftTypeMissingInstance, "attribute present in terraform but not in instance"
	}

	switch exp := expected.(type) {
	case []string:
		act, ok := actual.([]string)
		if !ok {
			return DriftTypeValueMismatch, "type mismatch"
		}
		return c.analyzeSliceDrift(exp, act)

	case map[string]string:
		act, ok := actual.(map[string]string)
		if !ok {
			return DriftTypeValueMismatch, "type mismatch"
		}
		return c.analyzeMapDrift(exp, act)
	}

	return DriftTypeValueMismatch, ""
}

func (c *AttributeComparator) analyzeSliceDrift(expected, actual []string) (DriftType, string) {
	expectedSet := make(map[string]bool)
	for _, v := range expected {
		expectedSet[v] = true
	}

	actSet := make(map[string]bool)
	for _, v := range actual {
		actSet[v] = true
	}

	var missingInExpectation, extraFromInstance []string

	for v := range expectedSet {
		if !actSet[v] {
			missingInExpectation = append(missingInExpectation, v)
		}
	}

	for v := range actSet {
		if !expectedSet[v] {
			extraFromInstance = append(extraFromInstance, v)
		}
	}

	if len(missingInExpectation) > 0 && len(extraFromInstance) == 0 {
		return DriftTypeMissingInstance, fmt.Sprintf("missing values: %v", missingInExpectation)
	}

	if len(extraFromInstance) > 0 && len(missingInExpectation) == 0 {
		return DriftTypeExtraInInstance, fmt.Sprintf("extra values: %v", extraFromInstance)
	}

	if len(missingInExpectation) > 0 && len(extraFromInstance) > 0 {
		return DriftTypeValueMismatch, fmt.Sprintf("missing: %v, extra: %v", missingInExpectation, extraFromInstance)
	}

	return DriftTypeValueMismatch, ""
}

func (c *AttributeComparator) analyzeMapDrift(expected, actual map[string]string) (DriftType, string) {
	var missingKeys, extraKeys, differentValues []string

	for k := range expected {
		if _, exists := actual[k]; !exists {
			missingKeys = append(missingKeys, k)
		} else if expected[k] != actual[k] {
			differentValues = append(differentValues, k)
		}
	}

	for k := range actual {
		if _, exists := expected[k]; !exists {
			extraKeys = append(extraKeys, k)
		}
	}

	if len(missingKeys) > 0 && len(extraKeys) == 0 && len(differentValues) == 0 {
		return DriftTypeMissingInstance, fmt.Sprintf("missing keys: %v", missingKeys)
	}

	if len(extraKeys) > 0 && len(missingKeys) == 0 && len(differentValues) == 0 {
		return DriftTypeExtraInInstance, fmt.Sprintf("extra keys: %v", extraKeys)
	}

	details := ""
	if len(missingKeys) > 0 {
		details += fmt.Sprintf("missing keys: %v", missingKeys)
	}
	if len(extraKeys) > 0 {
		if details != "" {
			details += ", "
		}
		details += fmt.Sprintf("extra keys: %v", extraKeys)
	}
	if len(differentValues) > 0 {
		if details != "" {
			details += ", "
		}
		details += fmt.Sprintf("different values: %v", differentValues)
	}

	return DriftTypeValueMismatch, details
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

	if len(expected) == 0 {
		return true
	}

	expSorted := make([]string, len(expected))
	actSorted := make([]string, len(actual))

	copy(expSorted, expected)
	copy(actSorted, actual)

	sort.Strings(expSorted)
	sort.Strings(actSorted)

	for i := range expSorted {
		if expSorted[i] != actSorted[i] {
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
