package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const migrationTemplate = `package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations" 
)

func init() {
	migrations.Register({{.Version}}, "{{.Description}}", Up{{.FuncName}}, Down{{.FuncName}})
}

func Up{{.FuncName}}(db *mongo.Database) error {
	// TODO: Implement UP migration logic
	// ctx := context.Background()
	
	return nil
}

func Down{{.FuncName}}(db *mongo.Database) error {
	// TODO: Implement DOWN/ROLLBACK logic
	// ctx := context.Background()

	return nil
}
`

type MigrationData struct {
	Version     int
	Description string
	FuncName    string // Added for the Up/Down function names
}

func GenerateMigrationFile(config *MigrationConfig, name string) error {
	// Convert "create_todos" to "create_todos" for the file name
	normalizedName := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	nextVersion := GetLatestVersion() + 1

	migrationsDir := config.MongoDB.Migrations.Dir
	if migrationsDir == "" {
		migrationsDir = "./migrations/versions"
	}

	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create migrations directory: %w", err)
	}

	filename := fmt.Sprintf("%03d_%s.go", nextVersion, normalizedName)
	fullPath := filepath.Join(migrationsDir, filename)

	if _, err := os.Stat(fullPath); err == nil {
		return fmt.Errorf("migration file already exists: %s", fullPath)
	}

	data := MigrationData{
		Version:     nextVersion,
		Description: normalizedName,
		FuncName:    toCamelCase(normalizedName),
	}

	tmpl, err := template.New("migration").Parse(migrationTemplate)
	if err != nil {
		return err
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p != "" {
			// Capitalize the first letter of each part
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
