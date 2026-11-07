package migrations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type IndexKey struct {
	Field string `yaml:"field"`
	Order int    `yaml:"order"`
}

type CollectionSchema struct {
	Collection string `json:"collection"`
	Struct     string `json:"struct"`
	// DomainImport is the import path of the domain package that defines Struct.
	DomainImport string     `json:"domain_import"`
	Indexes      []IndexDef `json:"indexes"`
	Fields       []FieldDef `json:"fields"`
}

type IndexDef struct {
	Name string `json:"name"`
	Keys []struct {
		Field string `json:"field"`
		Order int    `json:"order"`
	} `json:"keys"`
	Unique bool `json:"unique,omitempty"`
	Sparse bool `json:"sparse,omitempty"`
}

type FieldDef struct {
	Name       string `json:"name"`
	DomainType string `json:"domain_type"`
	DBType     string `json:"db_type"`
	Bson       string `json:"bson"`
	IsID       bool   `json:"is_id,omitempty"`
}

type SeedFileDef struct {
	Collection string `yaml:"collection"`
	File       string `yaml:"file"`
}

type JSONIndexKey struct {
	Field string `json:"field"`
	Order int    `json:"order"`
}

type JSONIndex struct {
	Name   string         `json:"name"`
	Keys   []JSONIndexKey `json:"keys"`
	Unique bool           `json:"unique"`
	Sparse bool           `json:"sparse"`
}

type JSONField struct {
	Name       string `json:"name"`
	DomainType string `json:"domain_type"`
	DBType     string `json:"db_type"`
	Bson       string `json:"bson"`
	IsID       bool   `json:"is_id"`
}

type JSONSchema struct {
	CollectionName string      `json:"collection_name"`
	StructName     string      `json:"struct_name"`
	Fields         []JSONField `json:"fields"`
	Indexes        []JSONIndex `json:"indexes"`
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
		SchemaDir string `yaml:"schema"`
		// Collections []CollectionDef `yaml:"collections"`
	} `yaml:"mongodb"`
}

func LoadSchemas(dir string) ([]CollectionSchema, error) {
	var schemas []CollectionSchema

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema directory %s: %w", dir, err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue // Skip subdirectories and non-JSON files
		}

		path := filepath.Join(dir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema file %s: %w", path, err)
		}

		var schema CollectionSchema
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to parse schema file %s: %w", path, err)
		}

		schemas = append(schemas, schema)
	}

	return schemas, nil
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
