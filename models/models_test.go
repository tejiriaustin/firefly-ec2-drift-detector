package models

import (
	flog "firefly-ec2-drift-detector/logger"
	"testing"
)

func newTestComparator(t *testing.T) *AttributeComparator {
	t.Helper()
	logger := flog.NewTestLogger()

	return NewAttributeComparator(logger)
}

func TestDriftReport_AddDrift(t *testing.T) {
	report := &DriftReport{
		InstanceID: "i-123",
	}

	report.AddDrift("InstanceType", "t3.micro", "t3.medium", DriftTypeValueMismatch)

	if !report.HasDrift {
		t.Fatalf("expected HasDrift to be true")
	}

	if len(report.Drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(report.Drifts))
	}

	drift := report.Drifts[0]
	if drift.AttributeName != "InstanceType" {
		t.Errorf("unexpected attribute name: %s", drift.AttributeName)
	}
}

func TestDriftReport_Summary(t *testing.T) {
	tests := []struct {
		name     string
		report   *DriftReport
		expected string
	}{
		{
			name: "no drift",
			report: &DriftReport{
				InstanceID: "i-1",
				HasDrift:   false,
			},
			expected: "Instance i-1: No drift detected",
		},
		{
			name: "with drift",
			report: &DriftReport{
				InstanceID: "i-2",
				HasDrift:   true,
				Drifts:     []AttributeDrift{{}, {}},
			},
			expected: "Instance i-2: Drift detected in 2 attribute(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.Summary(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCompareAttributes_NoDrift(t *testing.T) {
	comparator := newTestComparator(t)

	expected := &InstanceState{
		InstanceID:   "i-123",
		InstanceType: "t3.micro",
		Monitoring:   true,
	}

	actual := &InstanceState{
		InstanceID:   "i-123",
		InstanceType: "t3.micro",
		Monitoring:   true,
	}

	report := comparator.CompareAttributes(expected, actual, []string{
		"InstanceType",
		"Monitoring",
	})

	if report.HasDrift {
		t.Fatalf("expected no drift")
	}

	if len(report.Drifts) != 0 {
		t.Fatalf("expected 0 drifts, got %d", len(report.Drifts))
	}
}

func TestCompareAttributes_PrimitiveDrift(t *testing.T) {
	comparator := newTestComparator(t)

	expected := &InstanceState{
		InstanceType: "t3.micro",
	}

	actual := &InstanceState{
		InstanceID:   "i-123",
		InstanceType: "t3.medium",
	}

	report := comparator.CompareAttributes(expected, actual, []string{
		"InstanceType",
	})

	if !report.HasDrift {
		t.Fatalf("expected drift")
	}

	if len(report.Drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(report.Drifts))
	}

	drift := report.Drifts[0]
	if drift.DriftType != DriftTypeValueMismatch {
		t.Errorf("unexpected drift type: %s", drift.DriftType)
	}
}

func TestCompareAttributes_SliceComparison_OrderIndependent(t *testing.T) {
	comparator := newTestComparator(t)

	expected := &InstanceState{
		SecurityGroups: []string{"sg-1", "sg-2"},
	}

	actual := &InstanceState{
		InstanceID:     "i-123",
		SecurityGroups: []string{"sg-2", "sg-1"},
	}

	report := comparator.CompareAttributes(expected, actual, []string{
		"SecurityGroups",
	})

	if report.HasDrift {
		t.Fatalf("expected no drift for reordered slices")
	}
}

func TestCompareAttributes_MapDrift(t *testing.T) {
	comparator := newTestComparator(t)

	expected := &InstanceState{
		Tags: map[string]string{
			"Env": "prod",
		},
	}

	actual := &InstanceState{
		InstanceID: "i-123",
		Tags: map[string]string{
			"Env": "staging",
		},
	}

	report := comparator.CompareAttributes(expected, actual, []string{
		"Tags",
	})

	if !report.HasDrift {
		t.Fatalf("expected drift due to map value mismatch")
	}
}

func TestCompareAttributes_InvalidAttribute(t *testing.T) {
	comparator := newTestComparator(t)

	expected := &InstanceState{}
	actual := &InstanceState{InstanceID: "i-123"}

	report := comparator.CompareAttributes(expected, actual, []string{
		"DoesNotExist",
	})

	if report.HasDrift {
		t.Fatalf("did not expect drift for invalid attribute")
	}

	if len(report.Drifts) != 0 {
		t.Fatalf("expected no drifts, got %d", len(report.Drifts))
	}
}
