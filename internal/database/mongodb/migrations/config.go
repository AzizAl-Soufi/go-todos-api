package migrations

import (
	"gopkg.in/yaml.v3"
	"os"
)

type IndexKey struct {
	Field string `yaml:"field"`
	Order int    `yaml:"order"`
}

type IndexDef struct {
	Keys   []IndexKey `yaml:"keys"`
	Unique bool       `yaml:"unique"`
}

type FieldDef struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	Bson string `yaml:"bson"`
}

type CollectionDef struct {
	Name    string     `yaml:"name"`
	Struct  string     `yaml:"struct"`
	Indexes []IndexDef `yaml:"indexes"`
	Schema  []FieldDef `yaml:"schema"`
}

type SeedFileDef struct {
	Collection string `yaml:"collection"`
	File       string `yaml:"file"`
}

type MigrationConfig struct {
	Version string `yaml:"version"`
	MongoDB struct {
		Gen struct {
			Out     string `yaml:"out"`
			Package string `yaml:"package"`
		} `yaml:"gen"`
		Migrations struct {
			Dir string `yaml:"dir"`
		} `yaml:"migrations"`
		Seeding struct {
			Dir   string        `yaml:"dir"`
			Files []SeedFileDef `yaml:"files"`
		} `yaml:"seeding"`
		Collections []CollectionDef `yaml:"collections"`
	} `yaml:"mongodb"`
}

func LoadConfig(path string) (*MigrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config MigrationConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
