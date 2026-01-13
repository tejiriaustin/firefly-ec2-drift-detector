package terraform

type StateFile struct {
	Version   int        `json:"version"`
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Type      string     `json:"type"`
	Name      string     `json:"name"`
	Mode      string     `json:"mode"`
	Instances []Instance `json:"instances"`
}

type Instance struct {
	Attributes Attributes `json:"attributes"`
}

type Attributes struct {
	ID                  string            `json:"id"`
	InstanceType        string            `json:"instance_type"`
	AvailabilityZone    string            `json:"availability_zone"`
	VpcSecurityGroupIds []string          `json:"vpc_security_group_ids"`
	Tags                map[string]string `json:"tags"`
	SubnetID            string            `json:"subnet_id"`
	AMI                 string            `json:"ami"`
	KeyName             string            `json:"key_name"`
	Monitoring          bool              `json:"monitoring"`
}
