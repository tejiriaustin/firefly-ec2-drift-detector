package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
)

type TerraformClient struct {
	_         struct{}
	logger    *flog.Logger
	hclParser *HCLParser
}

func NewTerraformClient(logger *flog.Logger) *TerraformClient {
	return &TerraformClient{
		logger:    logger,
		hclParser: NewHCLParser(logger),
	}
}

func (p *TerraformClient) ParseStateFile(path string) (map[string]*models.InstanceState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	if info.IsDir() {
		p.logger.Info("detected directory, parsing HCL files",
			zap.String("path", path),
		)
		return p.hclParser.ParseHCLDirectory(path)
	}

	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".tf" {
		p.logger.Info("detected HCL file, parsing with HCL parser",
			zap.String("filepath", path),
		)
		return p.hclParser.ParseHCLFile(path)
	}

	p.logger.Info("parsing as JSON terraform state file",
		zap.String("filepath", path),
	)
	return p.parseJSONStateFile(path)
}

func (p *TerraformClient) parseJSONStateFile(filepath string) (map[string]*models.InstanceState, error) {
	p.logger.Info("parsing terraform state file",
		zap.String("filepath", filepath),
	)

	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	p.logger.Debug("terraform state loaded",
		zap.Int("version", state.Version),
		zap.Int("resource_count", len(state.Resources)),
	)

	instances := make(map[string]*models.InstanceState)
	for _, resource := range state.Resources {
		if resource.Type == "aws_instance" {
			for _, inst := range resource.Instances {
				instanceState := p.mapToInstanceState(inst.Attributes)
				instances[instanceState.InstanceID] = instanceState

				p.logger.Debug("parsed instance from state",
					zap.String("instance_id", instanceState.InstanceID),
					zap.String("resource_name", resource.Name),
					zap.String("instance_type", instanceState.InstanceType),
				)
			}
		}
	}

	p.logger.Info("successfully parsed terraform state",
		zap.String("filepath", filepath),
		zap.Int("instance_count", len(instances)),
	)

	return instances, nil
}

func (p *TerraformClient) mapToInstanceState(attrs Attributes) *models.InstanceState {
	return &models.InstanceState{
		InstanceID:       attrs.ID,
		InstanceType:     attrs.InstanceType,
		AvailabilityZone: attrs.AvailabilityZone,
		SecurityGroups:   attrs.VpcSecurityGroupIds,
		Tags:             attrs.Tags,
		SubnetID:         attrs.SubnetID,
		ImageID:          attrs.AMI,
		KeyName:          attrs.KeyName,
		Monitoring:       attrs.Monitoring,
	}
}
