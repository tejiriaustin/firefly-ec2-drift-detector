package terraform

import (
	flog "firefly-ec2-drift-detector/logger"
	"os"
	"path/filepath"
	"testing"
)

func newTestLogger() *flog.Logger {
	return flog.NewTestLogger()
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "state.tfstate")

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	return path
}

func TestParseStateFile_Success(t *testing.T) {
	tfState := `
{
  "version": 4,
  "resources": [
    {
      "type": "aws_instance",
      "name": "web",
      "instances": [
        {
          "attributes": {
            "id": "i-123",
            "instance_type": "t3.micro",
            "availability_zone": "us-east-1a",
            "vpc_security_group_ids": ["sg-1", "sg-2"],
            "tags": {"Name": "web"},
            "subnet_id": "subnet-1",
            "ami": "ami-123",
            "key_name": "my-key",
            "monitoring": true
          }
        }
      ]
    }
  ]
}`

	path := writeTempFile(t, tfState)

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	inst := instances["i-123"]
	if inst == nil {
		t.Fatalf("instance i-123 not found")
	}

	if inst.InstanceType != "t3.micro" {
		t.Errorf("unexpected instance type: %s", inst.InstanceType)
	}

	if inst.Monitoring != true {
		t.Errorf("expected monitoring enabled")
	}
}

func TestParseStateFile_IgnoresNonEC2Resources(t *testing.T) {
	tfState := `
{
  "version": 4,
  "resources": [
    {
      "type": "aws_s3_bucket",
      "name": "bucket",
      "instances": []
    }
  ]
}`

	path := writeTempFile(t, tfState)

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 0 {
		t.Fatalf("expected no instances, got %d", len(instances))
	}
}

func TestParseStateFile_MultipleInstances(t *testing.T) {
	tfState := `
{
  "version": 4,
  "resources": [
    {
      "type": "aws_instance",
      "name": "app",
      "instances": [
        { "attributes": { "id": "i-1" } },
        { "attributes": { "id": "i-2" } }
      ]
    }
  ]
}`

	path := writeTempFile(t, tfState)

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
}

func TestParseStateFile_FileNotFound(t *testing.T) {
	client := NewTerraformClient(newTestLogger())

	_, err := client.ParseStateFile("does-not-exist.tfstate")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseStateFile_InvalidJSON(t *testing.T) {
	path := writeTempFile(t, "{ invalid json")

	client := NewTerraformClient(newTestLogger())

	_, err := client.ParseStateFile(path)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestMapToInstanceState(t *testing.T) {
	client := NewTerraformClient(newTestLogger())

	state := client.mapToInstanceState(Attributes{
		ID:           "i-123",
		InstanceType: "t3.nano",
		AMI:          "ami-xyz",
	})

	if state.InstanceID != "i-123" {
		t.Fatalf("unexpected instance id")
	}

	if state.ImageID != "ami-xyz" {
		t.Fatalf("unexpected AMI")
	}
}
