package service

import (
	"context"
	"errors"
	"testing"

	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
)

type fakeParser struct {
	states map[string]*models.InstanceState
	err    error
}

func (f *fakeParser) ParseStateFile(_ string) (map[string]*models.InstanceState, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.states, nil
}

type fakeProvider struct {
	states map[string]*models.InstanceState
	errs   map[string]error
}

func (f *fakeProvider) GetInstanceState(_ context.Context, id string) (*models.InstanceState, error) {
	if err, ok := f.errs[id]; ok {
		return nil, err
	}
	if state, ok := f.states[id]; ok {
		return state, nil
	}
	return nil, errors.New("instance not found")
}

type fakeComparator struct {
	report *models.DriftReport
}

func (f *fakeComparator) CompareAttributes(_, actual *models.InstanceState, _ []string) *models.DriftReport {
	return f.report
}

func newTestLogger() *flog.Logger {
	return flog.NewTestLogger()
}

func TestDetectDrift_NoDrift(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			InstanceID: "i-1",
			HasDrift:   false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", []string{"i-1"}, []string{"InstanceType"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}

	if reports[0].HasDrift {
		t.Fatalf("did not expect drift")
	}
}

func TestDetectDrift_WithDrift(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {},
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			InstanceID: "i-1",
			HasDrift:   true,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", []string{"i-1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reports[0].HasDrift {
		t.Fatalf("expected drift")
	}
}

func TestDetectDrift_EmptyInstanceList(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {},
			"i-2": {},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {},
			"i-2": {},
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
}

func TestDetectDrift_PartialFailure(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {},
			"i-2": {},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {},
		},
		errs: map[string]error{
			"i-2": errors.New("boom"),
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{InstanceID: "i-1"},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", []string{"i-1", "i-2"}, nil)

	if err == nil {
		t.Fatalf("expected error")
	}

	if reports != nil {
		t.Fatalf("expected reports to be nil on partial failure")
	}
}

func TestDetectSingleDrift(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {},
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			InstanceID: "i-1",
			HasDrift:   false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	report, err := svc.DetectSingleDrift(ctx, "state.tf", "i-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.InstanceID != "i-1" {
		t.Fatalf("unexpected instance id")
	}
}

func TestDetectSingleDrift_NotInState(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{},
	}

	svc := NewDriftService(nil, parser, nil, newTestLogger())

	_, err := svc.DetectSingleDrift(ctx, "state.tf", "i-404", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
