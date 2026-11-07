package migrations

import (
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ---- registry ----

// withCleanRegistry swaps the global registry for an empty one for the test.
func withCleanRegistry(t *testing.T) {
	t.Helper()
	saved := registry
	registry = make(map[int]MigrationEntry)
	t.Cleanup(func() { registry = saved })
}

func noop(*mongo.Database) error { return nil }

func TestRegistry_EmptyState(t *testing.T) {
	withCleanRegistry(t)

	if got := GetLatestVersion(); got != 0 {
		t.Fatalf("GetLatestVersion() = %d, want 0", got)
	}
	if got := GetMigrations(); len(got) != 0 {
		t.Fatalf("GetMigrations() = %v, want empty", got)
	}
}

func TestRegistry_RegisterSortsAndTracksLatest(t *testing.T) {
	withCleanRegistry(t)

	Register(3, "third", noop, noop)
	Register(1, "first", noop, noop)
	Register(10, "tenth", noop, noop)
	Register(2, "second", noop, noop)

	got := GetMigrations()
	var versions []int
	for _, m := range got {
		versions = append(versions, m.Version)
	}
	if fmt.Sprint(versions) != "[1 2 3 10]" {
		t.Fatalf("versions = %v, want [1 2 3 10]", versions)
	}
	if got[0].Name != "first" || got[0].Migrate == nil || got[0].Rollback == nil {
		t.Fatalf("unexpected entry: %+v", got[0])
	}
	if latest := GetLatestVersion(); latest != 10 {
		t.Fatalf("GetLatestVersion() = %d, want 10", latest)
	}
}

func TestRegistry_DuplicateVersionPanics(t *testing.T) {
	withCleanRegistry(t)
	Register(1, "a", noop, noop)

	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "Migration version 1 already exists") {
			t.Fatalf("recover() = %v, want duplicate-version panic", r)
		}
	}()
	Register(1, "b", noop, noop)
}

// ---- generator ----

func TestToCamelCase(t *testing.T) {
	tests := map[string]string{
		"create_todos":           "CreateTodos",
		"add_index_to_users":     "AddIndexToUsers",
		"single":                 "Single",
		"":                       "",
		"double__underscore":     "DoubleUnderscore",
		"_leading_and_trailing_": "LeadingAndTrailing",
		"Already_Camel":          "AlreadyCamel",
	}
	for in, want := range tests {
		if got := toCamelCase(in); got != want {
			t.Errorf("toCamelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func configWithMigrationsDir(dir string) *MigrationConfig {
	cfg := &MigrationConfig{}
	cfg.MongoDB.Migrations.Dir = dir
	return cfg
}

func TestGenerateMigrationFile(t *testing.T) {
	withCleanRegistry(t)
	Register(1, "init", noop, noop)
	Register(2, "second", noop, noop)

	dir := filepath.Join(t.TempDir(), "versions") // does not exist yet
	if err := GenerateMigrationFile(configWithMigrationsDir(dir), "Add User Index"); err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}

	path := filepath.Join(dir, "003_add_user_index.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to be written: %v", path, err)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors); err != nil {
		t.Fatalf("generated migration is not valid Go: %v\n%s", err, src)
	}
	for _, want := range []string{
		"package versions",
		`migrations.Register(3, "add_user_index", UpAddUserIndex, DownAddUserIndex)`,
		"func UpAddUserIndex(db *mongo.Database) error",
		"func DownAddUserIndex(db *mongo.Database) error",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated file missing %q", want)
		}
	}

	// Generating the same name again at the same version must not overwrite.
	err = GenerateMigrationFile(configWithMigrationsDir(dir), "add_user_index")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second GenerateMigrationFile err = %v, want already-exists error", err)
	}
}

func TestGenerateMigrationFile_DefaultDir(t *testing.T) {
	withCleanRegistry(t)
	t.Chdir(t.TempDir())

	if err := GenerateMigrationFile(&MigrationConfig{}, "init"); err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join("migrations", "versions", "001_init.go")); err != nil {
		t.Fatalf("expected file in default dir: %v", err)
	}
}

// ---- config ----

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mongo.yaml")
	yaml := `version: "0.1"
mongodb:
  schema: "./schema"
  gen:
    out: "./generated"
    package: "generated"
  migrations:
    dir: "./versions"
  seeding:
    dir: "./seeds"
    files:
      - collection: "users"
        file: "users.json"
      - collection: "todos"
        file: "todos.json"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	m := cfg.MongoDB
	if cfg.Version != "0.1" || m.SchemaDir != "./schema" || m.Gen.Out != "./generated" ||
		m.Gen.Package != "generated" || m.Migrations.Dir != "./versions" || m.Seeding.Dir != "./seeds" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(m.Seeding.Files) != 2 || m.Seeding.Files[1] != (SeedFileDef{Collection: "todos", File: "todos.json"}) {
		t.Fatalf("unexpected seed files: %+v", m.Seeding.Files)
	}
}

func TestLoadConfig_ProjectFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "..", "cmd", "migration", "mongo.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig(cmd/migration/mongo.yaml): %v", err)
	}
	if cfg.MongoDB.SchemaDir == "" || cfg.MongoDB.Gen.Out == "" || cfg.MongoDB.Gen.Package == "" {
		t.Fatalf("project config is missing required fields: %+v", cfg.MongoDB)
	}
}

func TestLoadConfig_Errors(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("expected error for a missing file")
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte("mongodb: [unclosed"), 0o600)
	if _, err := LoadConfig(bad); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// ---- schemas ----

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSchemas_SkipsNonJSONAndDirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.json"), `{"collection":"a","struct":"A"}`)
	writeFile(t, filepath.Join(dir, "notes.txt"), `not json`)
	writeFile(t, filepath.Join(dir, "nested", "b.json"), `{"collection":"b"}`)

	schemas, err := LoadSchemas(dir)
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Collection != "a" || schemas[0].Struct != "A" {
		t.Fatalf("schemas = %+v, want only a.json", schemas)
	}
}

func TestLoadSchemas_Errors(t *testing.T) {
	if _, err := LoadSchemas(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for a missing directory")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.json"), `{"collection":`)
	_, err := LoadSchemas(dir)
	if err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Errorf("err = %v, want parse error naming broken.json", err)
	}
}

// TestProjectSchemas validates the real schema files shipped in ./schema
// against the format LoadSchemas understands.
func TestProjectSchemas(t *testing.T) {
	schemas, err := LoadSchemas("schema")
	if err != nil {
		t.Fatalf("LoadSchemas(schema): %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("expected at least one schema file")
	}

	seen := map[string]bool{}
	for i, s := range schemas {
		if s.Collection == "" {
			t.Errorf("schema #%d has no \"collection\" (did it use a different key, e.g. \"collection_name\"?)", i)
			continue
		}
		if seen[s.Collection] {
			t.Errorf("collection %q defined more than once", s.Collection)
		}
		seen[s.Collection] = true

		if s.Struct == "" {
			t.Errorf("%s: missing \"struct\"; code generation will skip it", s.Collection)
		}
		idFields := 0
		for _, f := range s.Fields {
			if f.Name == "" || f.DBType == "" || f.Bson == "" {
				t.Errorf("%s: incomplete field %+v", s.Collection, f)
			}
			if f.Name == "ID" && f.IsID {
				idFields++
			}
		}
		if idFields != 1 {
			t.Errorf("%s: expected exactly one ID field marked is_id, got %d", s.Collection, idFields)
		}
		for _, idx := range s.Indexes {
			if idx.Name == "" || len(idx.Keys) == 0 {
				t.Errorf("%s: index needs a name and keys: %+v", s.Collection, idx)
			}
			for _, k := range idx.Keys {
				if k.Order != 1 && k.Order != -1 {
					t.Errorf("%s/%s: key %q order must be 1 or -1, got %d", s.Collection, idx.Name, k.Field, k.Order)
				}
			}
		}
	}

	for _, want := range []string{"users", "todos"} {
		if !seen[want] {
			t.Errorf("missing schema for collection %q", want)
		}
	}
}

// ---- codegen ----

func codegenConfig(schemaDir, outDir string) *MigrationConfig {
	cfg := &MigrationConfig{}
	cfg.MongoDB.SchemaDir = schemaDir
	cfg.MongoDB.Gen.Out = outDir
	cfg.MongoDB.Gen.Package = "generated"
	return cfg
}

func TestGenerateQueries(t *testing.T) {
	schemaDir := t.TempDir()
	writeFile(t, filepath.Join(schemaDir, "widgets.json"), `{
	  "collection": "widgets",
	  "struct": "Widget",
	  "indexes": [
	    {"name": "idx_owner", "keys": [{"field": "ownerId", "order": 1}, {"field": "createdAt", "order": -1}], "unique": true, "sparse": true}
	  ],
	  "fields": [
	    {"name": "ID", "db_type": "bson.ObjectID", "bson": "_id,omitempty", "is_id": true},
	    {"name": "OwnerID", "db_type": "bson.ObjectID", "bson": "ownerId", "is_id": true},
	    {"name": "Label", "db_type": "string", "bson": "label"}
	  ]
	}`)
	writeFile(t, filepath.Join(schemaDir, "noindex.json"), `{
	  "collection": "plain", "struct": "Plain",
	  "fields": [{"name": "ID", "db_type": "bson.ObjectID", "bson": "_id,omitempty", "is_id": true}]
	}`)
	// Schemas without struct or fields must be skipped.
	writeFile(t, filepath.Join(schemaDir, "nostruct.json"), `{"collection":"x","fields":[{"name":"A","db_type":"string","bson":"a"}]}`)
	writeFile(t, filepath.Join(schemaDir, "nofields.json"), `{"collection":"y","struct":"Y"}`)

	outDir := filepath.Join(t.TempDir(), "out")
	if err := GenerateQueries(codegenConfig(schemaDir, outDir)); err != nil {
		t.Fatalf("GenerateQueries: %v", err)
	}

	entries, _ := os.ReadDir(outDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if fmt.Sprint(names) != "[plain_repo.go widget_repo.go]" {
		t.Fatalf("generated files = %v, want [plain_repo.go widget_repo.go]", names)
	}

	widget := readAndParse(t, filepath.Join(outDir, "widget_repo.go"))
	for _, want := range []string{
		"// Code generated by mongo-gen. DO NOT EDIT.",
		"package generated",
		"type WidgetRepository interface",
		"func NewMongoWidgetRepository(db mongodb.MongoDBClient) WidgetRepository",
		`coll: db.Database().Collection("widgets")`,
		"OwnerID bson.ObjectID `bson:\"ownerId\"`",
		"ID: dbModel.ID.Hex(),",
		"OwnerID: dbModel.OwnerID.Hex(),",
		"Label: dbModel.Label,",
		`{Key: "ownerId", Value: 1},`,
		`{Key: "createdAt", Value: -1},`,
		`SetName("idx_owner")`,
		".SetUnique(true)",
		".SetSparse(true)",
		"item.ID = dbModel.ID.Hex() // Backfill the domain model",
	} {
		// Compare with whitespace collapsed: gofmt aligns fields and comments.
		if !strings.Contains(collapseSpace(widget), collapseSpace(want)) {
			t.Errorf("widget_repo.go missing %q", want)
		}
	}
	if formatted, err := format.Source([]byte(widget)); err != nil || string(formatted) != widget {
		t.Error("widget_repo.go is not gofmt-formatted")
	}

	if strings.Contains(widget, `"time"`) {
		t.Error(`widget_repo.go has no time fields and must not import "time"`)
	}

	plain := readAndParse(t, filepath.Join(outDir, "plain_repo.go"))
	if strings.Contains(plain, "CreateMany") {
		t.Error("plain_repo.go should not create indexes when none are defined")
	}
}

func TestFieldsNeedTime(t *testing.T) {
	if fieldsNeedTime([]FieldDef{{DBType: "string"}, {DBType: "bson.ObjectID"}}) {
		t.Error("expected false without time fields")
	}
	if !fieldsNeedTime([]FieldDef{{DBType: "string"}, {DBType: "time.Time"}}) {
		t.Error("expected true with a time.Time field")
	}
	if !fieldsNeedTime([]FieldDef{{DBType: "*time.Time"}}) {
		t.Error("expected true with a *time.Time field")
	}
}

func TestGenerateQueries_ProjectSchemas(t *testing.T) {
	outDir := t.TempDir()
	if err := GenerateQueries(codegenConfig("schema", outDir)); err != nil {
		t.Fatalf("GenerateQueries: %v", err)
	}
	readAndParse(t, filepath.Join(outDir, "todo_repo.go"))
	readAndParse(t, filepath.Join(outDir, "user_repo.go"))
}

// TestGenerateQueries_ProjectSchemasCompile type-checks the generated code
// against the real module dependencies (e.g. mongo-driver API changes).
func TestGenerateQueries_ProjectSchemasCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go toolchain")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}

	// The output must live inside the module to resolve its imports; the "_"
	// prefix keeps the directory out of ./... patterns.
	outDir, err := os.MkdirTemp(".", "_codegen_check_")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outDir) })

	if err := GenerateQueries(codegenConfig("schema", outDir)); err != nil {
		t.Fatalf("GenerateQueries: %v", err)
	}

	cmd := exec.Command(goBin, "vet", "./"+filepath.Base(outDir))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated code does not compile:\n%s", out)
	}
}

func TestGenerateQueries_Errors(t *testing.T) {
	if err := GenerateQueries(codegenConfig("schema", "")); err == nil || !strings.Contains(err.Error(), "gen.out") {
		t.Errorf("err = %v, want gen.out error", err)
	}
	if err := GenerateQueries(codegenConfig(filepath.Join(t.TempDir(), "missing"), t.TempDir())); err == nil {
		t.Error("expected error for a missing schema dir")
	}
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func readAndParse(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors); err != nil {
		t.Fatalf("%s is not valid Go: %v\n%s", filepath.Base(path), err, src)
	}
	return string(src)
}

// ---- indexes ----

func TestIsIndexConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"code 85", mongo.CommandError{Code: 85}, true},
		{"name message", mongo.CommandError{Code: 86, Message: "Index with name: idx_email already exists with different options"}, true},
		{"wrapped", fmt.Errorf("create: %w", mongo.CommandError{Code: 85}), true},
		{"other command error", mongo.CommandError{Code: 11000, Message: "duplicate key"}, false},
		{"plain error", errors.New("Index with name"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIndexConflictError(tt.err); got != tt.want {
				t.Fatalf("isIndexConflictError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
