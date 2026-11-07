package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// LoadConfig uses the global viper instance and reads ".env" from the
// working directory, so each test runs in its own temp dir with a fresh viper.
func setup(t *testing.T, envFile string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	if envFile != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envFile), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}
	}
	t.Chdir(dir)
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

	want := Config{
		App: AppConfig{Port: 8080, Debug: true},
		DB:  DatabaseConfig{DatabaseURI: "mongodb://localhost:27017", DatabaseName: "todos", DBType: "mongo"},
		JWT: JWTConfig{PrivateKeyPath: "secrets/private.pem", PublicKeyPath: "secrets/public.pem"},
	}
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

	want := Config{
		App: AppConfig{Port: 9090, Debug: true},
		DB:  DatabaseConfig{DatabaseURI: "postgres://db/todos", DatabaseName: "todos", DBType: "postgres"},
		JWT: JWTConfig{PrivateKeyPath: "/run/secrets/private.pem", PublicKeyPath: "/run/secrets/public.pem"},
	}
	if *cfg != want {
		t.Fatalf("config = %+v, want %+v", *cfg, want)
	}
}

// Every field in Config must be listed in envKeys, or it can't be set from the environment.
func TestEnvKeysCoverConfig(t *testing.T) {
	setup(t, "")
	for _, key := range envKeys {
		t.Setenv(key, "1")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.Port != 1 || !cfg.App.Debug || cfg.DB.DatabaseURI == "" || cfg.DB.DatabaseName == "" ||
		cfg.DB.DBType == "" || cfg.JWT.PrivateKeyPath == "" || cfg.JWT.PublicKeyPath == "" {
		t.Fatalf("some Config fields are not bound to the environment: %+v", *cfg)
	}
}

func TestLoadConfig_InvalidTypeReturnsError(t *testing.T) {
	setup(t, "PORT=not-a-number\n")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected an error for a non-numeric PORT")
	}
}
