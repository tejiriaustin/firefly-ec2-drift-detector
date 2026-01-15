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

func writeTempHCLFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.tf")

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp HCL file: %v", err)
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

func TestParseStateFile_HCLFile(t *testing.T) {
	hcl := `
resource "aws_instance" "web" {
  instance_type = "t3.micro"
  ami           = "ami-12345678"
  key_name      = "my-key"
  
  tags = {
    Name = "web-server"
    Env  = "prod"
  }
}
`

	path := writeTempHCLFile(t, hcl)

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	inst := instances["hcl:web"]
	if inst == nil {
		t.Fatalf("instance hcl:web not found")
	}

	if inst.InstanceType != "t3.micro" {
		t.Errorf("unexpected instance type: %s", inst.InstanceType)
	}

	if inst.ImageID != "ami-12345678" {
		t.Errorf("unexpected AMI: %s", inst.ImageID)
	}

	if len(inst.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(inst.Tags))
	}
}

func TestParseStateFile_HCLDirectory(t *testing.T) {
	dir := t.TempDir()

	file1 := `
resource "aws_instance" "web1" {
  instance_type = "t3.micro"
  ami           = "ami-123"
}
`

	file2 := `
resource "aws_instance" "web2" {
  instance_type = "t3.small"
  ami           = "ami-456"
}
`

	if err := os.WriteFile(filepath.Join(dir, "web1.tf"), []byte(file1), 0644); err != nil {
		t.Fatalf("failed to write web1.tf: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "web2.tf"), []byte(file2), 0644); err != nil {
		t.Fatalf("failed to write web2.tf: %v", err)
	}

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}

	if instances["hcl:web1"] == nil {
		t.Error("expected hcl:web1 not found")
	}

	if instances["hcl:web2"] == nil {
		t.Error("expected hcl:web2 not found")
	}
}

func TestParseStateFile_AutoDetection(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		filename    string
		expectCount int
		expectID    string
	}{
		{
			name: "JSON state file",
			content: `{
				"version": 4,
				"resources": [{
					"type": "aws_instance",
					"name": "test",
					"instances": [{"attributes": {"id": "i-json"}}]
				}]
			}`,
			filename:    "state.tfstate",
			expectCount: 1,
			expectID:    "i-json",
		},
		{
			name: "HCL file",
			content: `
resource "aws_instance" "test" {
  instance_type = "t3.micro"
  ami           = "ami-123"
}
`,
			filename:    "main.tf",
			expectCount: 1,
			expectID:    "hcl:test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.filename)

			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write file: %v", err)
			}

			client := NewTerraformClient(newTestLogger())

			instances, err := client.ParseStateFile(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(instances) != tt.expectCount {
				t.Errorf("expected %d instances, got %d", tt.expectCount, len(instances))
			}

			if instances[tt.expectID] == nil {
				t.Errorf("expected instance %s not found", tt.expectID)
			}
		})
	}
}

func TestBackwardCompatibility_WithJSONStateFile(t *testing.T) {
	tfState := `{
		"version": 4,
		"resources": [{
			"type": "aws_instance",
			"name": "compat_test",
			"instances": [{
				"attributes": {
					"id": "i-compat",
					"instance_type": "t3.micro"
				}
			}]
		}]
	}`

	path := writeTempFile(t, tfState)

	client := NewTerraformClient(newTestLogger())

	instances, err := client.ParseStateFile(path)
	if err != nil {
		t.Fatalf("backward compatibility broken: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("expected 1 instance")
	}

	if instances["i-compat"] == nil {
		t.Fatal("expected instance not found")
	}
}
