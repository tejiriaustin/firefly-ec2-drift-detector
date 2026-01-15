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
	states      map[string]*models.InstanceState
	errs        map[string]error
	batchStates map[string]*models.InstanceState
	batchErr    error
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

func (f *fakeProvider) GetInstanceStatesBatch(_ context.Context, instanceIDs []string) (map[string]*models.InstanceState, error) {
	if f.batchErr != nil {
		return nil, f.batchErr
	}

	if f.batchStates != nil {
		return f.batchStates, nil
	}

	result := make(map[string]*models.InstanceState)
	for _, id := range instanceIDs {
		if state, ok := f.states[id]; ok {
			result[id] = state
		}
	}
	return result, nil
}

type fakeComparator struct {
	report *models.DriftReport
}

func (f *fakeComparator) CompareAttributes(_, actual *models.InstanceState, _ []string) *models.DriftReport {
	if f.report != nil {
		reportCopy := *f.report
		reportCopy.InstanceID = actual.InstanceID
		return &reportCopy
	}
	return &models.DriftReport{
		InstanceID: actual.InstanceID,
		HasDrift:   false,
	}
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
			HasDrift: false,
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
			HasDrift: true,
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

func TestDetectDrift_EmptyInstanceList_SmallCount(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
			"i-2": {InstanceID: "i-2"},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
			"i-2": {InstanceID: "i-2"},
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			HasDrift: false,
		},
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

func TestDetectDrift_EmptyInstanceList_LargeCount_UsesBatchMode(t *testing.T) {
	ctx := context.Background()

	states := make(map[string]*models.InstanceState)
	for i := 0; i < 15; i++ {
		id := "i-" + string(rune('a'+i))
		states[id] = &models.InstanceState{InstanceID: id}
	}

	parser := &fakeParser{
		states: states,
	}

	provider := &fakeProvider{
		batchStates: states,
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			HasDrift: false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reports) != 15 {
		t.Fatalf("expected 15 reports, got %d", len(reports))
	}
}

func TestDetectDrift_PartialFailure_ConcurrentMode(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
			"i-2": {InstanceID: "i-2"},
		},
	}

	provider := &fakeProvider{
		states: map[string]*models.InstanceState{
			"i-1": {InstanceID: "i-1"},
		},
		errs: map[string]error{
			"i-2": errors.New("boom"),
		},
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			HasDrift: false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", []string{"i-1", "i-2"}, nil)

	if err == nil {
		t.Fatal("expected error for partial failure")
	}

	if len(reports) != 1 {
		t.Fatalf("expected 1 successful report despite error, got %d", len(reports))
	}

	if reports[0].InstanceID != "i-1" {
		t.Errorf("expected report for i-1, got %s", reports[0].InstanceID)
	}
}

func TestDetectDrift_PartialFailure_BatchMode(t *testing.T) {
	ctx := context.Background()

	states := make(map[string]*models.InstanceState)
	for i := 0; i < 15; i++ {
		id := "i-" + string(rune('a'+i))
		states[id] = &models.InstanceState{InstanceID: id}
	}

	parser := &fakeParser{
		states: states,
	}

	batchStates := make(map[string]*models.InstanceState)
	for id := range states {
		if id != "i-e" {
			batchStates[id] = states[id]
		}
	}

	provider := &fakeProvider{
		batchStates: batchStates,
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			HasDrift: false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", nil, nil)

	if err == nil {
		t.Fatal("expected error for missing instance")
	}

	if len(reports) != 14 {
		t.Fatalf("expected 14 reports, got %d", len(reports))
	}
}

func TestDetectSingleDrift(t *testing.T) {
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
			HasDrift: false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	report, err := svc.DetectSingleDrift(ctx, "state.tf", "i-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.InstanceID != "i-1" {
		t.Fatalf("unexpected instance id: %s", report.InstanceID)
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
		t.Fatal("expected error for instance not in state")
	}
}

func TestDetectDrift_ParserError(t *testing.T) {
	ctx := context.Background()

	parser := &fakeParser{
		err: errors.New("failed to parse state file"),
	}

	svc := NewDriftService(nil, parser, nil, newTestLogger())

	_, err := svc.DetectDrift(ctx, "state.tf", nil, nil)
	if err == nil {
		t.Fatal("expected error from parser")
	}
}

func TestDetectDrift_InstanceNotInExpectedState(t *testing.T) {
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

	comparator := &fakeComparator{}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", []string{"i-1", "i-999"}, nil)

	if err == nil {
		t.Fatal("expected error for instance not in expected state")
	}

	if len(reports) != 1 {
		t.Fatalf("expected 1 successful report, got %d", len(reports))
	}
}

func TestDetectDrift_BatchModeTrigger(t *testing.T) {
	ctx := context.Background()

	states := make(map[string]*models.InstanceState)
	instanceIDs := make([]string, 11)
	for i := 0; i < 11; i++ {
		id := "i-" + string(rune('0'+i))
		instanceIDs[i] = id
		states[id] = &models.InstanceState{InstanceID: id}
	}

	parser := &fakeParser{
		states: states,
	}

	provider := &fakeProvider{
		batchStates: states,
	}

	comparator := &fakeComparator{
		report: &models.DriftReport{
			HasDrift: false,
		},
	}

	svc := NewDriftService(provider, parser, comparator, newTestLogger())

	reports, err := svc.DetectDrift(ctx, "state.tf", instanceIDs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reports) != 11 {
		t.Fatalf("expected 11 reports, got %d", len(reports))
	}
}
