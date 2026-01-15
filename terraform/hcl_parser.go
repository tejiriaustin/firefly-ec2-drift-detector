package terraform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"go.uber.org/zap"

	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
)

type HCLParser struct {
	logger *flog.Logger
}

func NewHCLParser(logger *flog.Logger) *HCLParser {
	return &HCLParser{
		logger: logger,
	}
}

func (p *HCLParser) ParseHCLFile(filepath string) (map[string]*models.InstanceState, error) {
	p.logger.Info("parsing HCL terraform file",
		zap.String("filepath", filepath),
	)

	content, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read HCL file: %w", err)
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(content, filepath)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL: %s", diags.Error())
	}

	instances := make(map[string]*models.InstanceState)

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type")
	}

	for _, block := range body.Blocks {
		if block.Type != "resource" {
			continue
		}

		if len(block.Labels) < 2 {
			continue
		}

		resourceType := block.Labels[0]
		resourceName := block.Labels[1]

		if resourceType != "aws_instance" {
			continue
		}

		p.logger.Debug("found aws_instance resource",
			zap.String("resource_name", resourceName),
		)

		instanceState, err := p.parseInstanceBlock(block, resourceName)
		if err != nil {
			p.logger.Warn("failed to parse instance block",
				zap.String("resource_name", resourceName),
				zap.Error(err),
			)
			continue
		}

		instances[instanceState.InstanceID] = instanceState
	}

	p.logger.Info("successfully parsed HCL file",
		zap.String("filepath", filepath),
		zap.Int("instance_count", len(instances)),
	)

	return instances, nil
}

func (p *HCLParser) ParseHCLDirectory(dirPath string) (map[string]*models.InstanceState, error) {
	p.logger.Info("parsing HCL terraform directory",
		zap.String("directory", dirPath),
	)

	instances := make(map[string]*models.InstanceState)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".tf") {
			return nil
		}

		p.logger.Debug("processing terraform file",
			zap.String("file", path),
		)

		fileInstances, err := p.ParseHCLFile(path)
		if err != nil {
			p.logger.Warn("failed to parse file",
				zap.String("file", path),
				zap.Error(err),
			)
			return nil
		}

		for id, state := range fileInstances {
			instances[id] = state
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	p.logger.Info("successfully parsed HCL directory",
		zap.String("directory", dirPath),
		zap.Int("instance_count", len(instances)),
	)

	return instances, nil
}

func (p *HCLParser) parseInstanceBlock(block *hclsyntax.Block, resourceName string) (*models.InstanceState, error) {
	state := &models.InstanceState{
		InstanceID: fmt.Sprintf("hcl:%s", resourceName),
		Tags:       make(map[string]string),
	}

	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to get attributes: %s", diags.Error())
	}

	for name, attr := range attrs {
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			p.logger.Debug("failed to evaluate attribute",
				zap.String("attribute", name),
				zap.String("error", diags.Error()),
			)
			continue
		}

		switch name {
		case "instance_type":
			if value.Type() == cty.String {
				state.InstanceType = value.AsString()
			}

		case "availability_zone":
			if value.Type() == cty.String {
				state.AvailabilityZone = value.AsString()
			}

		case "ami":
			if value.Type() == cty.String {
				state.ImageID = value.AsString()
			}

		case "key_name":
			if value.Type() == cty.String {
				state.KeyName = value.AsString()
			}

		case "subnet_id":
			if value.Type() == cty.String {
				state.SubnetID = value.AsString()
			}

		case "vpc_security_group_ids":
			if value.Type().IsListType() || value.Type().IsSetType() || value.Type().IsTupleType() {
				state.SecurityGroups = p.extractStringList(value)
			}

		case "security_groups":
			if value.Type().IsListType() || value.Type().IsSetType() || value.Type().IsTupleType() {
				if len(state.SecurityGroups) == 0 {
					state.SecurityGroups = p.extractStringList(value)
				}
			}

		case "monitoring":
			if value.Type() == cty.Bool {
				state.Monitoring = value.True()
			}

		case "tags":
			if value.Type().IsMapType() || value.Type().IsObjectType() {
				state.Tags = p.extractStringMap(value)
			}
		}
	}

	for _, nestedBlock := range block.Body.Blocks {
		if nestedBlock.Type == "tags" {
			tags, err := p.parseTagsBlock(nestedBlock)
			if err == nil {
				state.Tags = tags
			}
		}
	}

	return state, nil
}

func (p *HCLParser) parseTagsBlock(block *hclsyntax.Block) (map[string]string, error) {
	tags := make(map[string]string)

	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to get tag attributes: %s", diags.Error())
	}

	for name, attr := range attrs {
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			continue
		}

		if value.Type() == cty.String {
			tags[name] = value.AsString()
		}
	}

	return tags, nil
}

func (p *HCLParser) extractStringList(value cty.Value) []string {
	if value.IsNull() || !value.IsKnown() {
		return []string{}
	}

	var result []string

	it := value.ElementIterator()
	for it.Next() {
		_, val := it.Element()
		if val.Type() == cty.String {
			result = append(result, val.AsString())
		}
	}

	return result
}

func (p *HCLParser) extractStringMap(value cty.Value) map[string]string {
	if value.IsNull() || !value.IsKnown() {
		return make(map[string]string)
	}

	result := make(map[string]string)

	it := value.ElementIterator()
	for it.Next() {
		key, val := it.Element()
		if key.Type() == cty.String && val.Type() == cty.String {
			result[key.AsString()] = val.AsString()
		}
	}

	return result
}
