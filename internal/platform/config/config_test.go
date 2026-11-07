package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

// LoadConfig uses the global viper instance and reads ".env" from the
// working directory, so each test runs in its own temp dir with a fresh viper.
func setup(t *testing.T, envFile string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	// Ignore the outer environment (e.g. `make` exports the project .env), so
	// only what each test sets is seen. t.Setenv restores the value afterwards.
	for _, s := range settings() {
		t.Setenv(s.key, "")
		os.Unsetenv(s.key)
	}

	dir := t.TempDir()
	if envFile != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envFile), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}
	}
	t.Chdir(dir)
}

// defaultConfig is what LoadConfig returns when nothing is set.
func defaultConfig() Config {
	return Config{
		App:  AppConfig{BaseURL: "http://localhost:8000"},
		DB:   DatabaseConfig{DBType: "memory", MongoMigrationsConfig: "./cmd/migrate/mongo.yaml"},
		Auth: AuthConfig{RateLimitPerMinute: 20, RateLimitBurst: 10},
		SMTP: SMTPConfig{Port: 587},
	}
}

func TestLoadConfig_FromEnvFile(t *testing.T) {
	setup(t, `PORT=8080
DEBUG=true
DATABASE_URI=mongodb://localhost:27017
DATABASE_NAME=todos
DB_TYPE=mongo
JWT_PRIVATE_KEY_PATH=secrets/private.pem
JWT_PUBLIC_KEY_PATH=secrets/public.pem
`)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := defaultConfig()
	want.App.Port, want.App.Debug = 8080, true
	want.DB.DatabaseURI, want.DB.DatabaseName, want.DB.DBType = "mongodb://localhost:27017", "todos", "mongo"
	want.JWT = JWTConfig{PrivateKeyPath: "secrets/private.pem", PublicKeyPath: "secrets/public.pem"}
	if *cfg != want {
		t.Fatalf("config = %+v, want %+v", *cfg, want)
	}
}

func TestLoadConfig_EnvOverridesFile(t *testing.T) {
	setup(t, "PORT=8080\nDB_TYPE=mongo\n")
	t.Setenv("PORT", "9090")
	t.Setenv("DB_TYPE", "postgres")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.App.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.App.Port)
	}
	if cfg.DB.DBType != "postgres" {
		t.Errorf("DBType = %q, want postgres", cfg.DB.DBType)
	}
}

func TestLoadConfig_MissingEnvFileIsNotAnError(t *testing.T) {
	setup(t, "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected a non-nil config")
	}
}

// Containers have no .env file; configuration must come from the environment alone.
func TestLoadConfig_EnvOnlyWithoutEnvFile(t *testing.T) {
	setup(t, "")
	t.Setenv("PORT", "9090")
	t.Setenv("DEBUG", "true")
	t.Setenv("DB_TYPE", "postgres")
	t.Setenv("DATABASE_URI", "postgres://db/todos")
	t.Setenv("DATABASE_NAME", "todos")
	t.Setenv("JWT_PRIVATE_KEY_PATH", "/run/secrets/private.pem")
	t.Setenv("JWT_PUBLIC_KEY_PATH", "/run/secrets/public.pem")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := defaultConfig()
	want.App.Port, want.App.Debug = 9090, true
	want.DB.DatabaseURI, want.DB.DatabaseName, want.DB.DBType = "postgres://db/todos", "todos", "postgres"
	want.JWT = JWTConfig{PrivateKeyPath: "/run/secrets/private.pem", PublicKeyPath: "/run/secrets/public.pem"}
	if *cfg != want {
		t.Fatalf("config = %+v, want %+v", *cfg, want)
	}
}

// Every Config field must be settable from the environment. The keys are
// derived from the struct, so this guards the derivation itself.
func TestEveryFieldIsBoundToTheEnvironment(t *testing.T) {
	setup(t, "")
	for _, s := range settings() {
		t.Setenv(s.key, "1")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	var check func(path string, v reflect.Value)
	check = func(path string, v reflect.Value) {
		if v.Kind() == reflect.Struct {
			for i := range v.NumField() {
				check(path+"."+v.Type().Field(i).Name, v.Field(i))
			}
			return
		}
		if v.IsZero() {
			t.Errorf("%s was not populated from the environment", path)
		}
	}
	check("Config", reflect.ValueOf(*cfg))
}

func TestDefaults(t *testing.T) {
	setup(t, "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if *cfg != defaultConfig() {
		t.Fatalf("config = %+v, want defaults %+v", *cfg, defaultConfig())
	}
}

func TestLoadConfig_InvalidTypeReturnsError(t *testing.T) {
	setup(t, "PORT=not-a-number\n")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected an error for a non-numeric PORT")
	}
}
