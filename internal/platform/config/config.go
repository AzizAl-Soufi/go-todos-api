// Package config loads configuration from a .env file and the environment.
//
// Every setting is declared once, as a struct field with a `mapstructure` tag
// naming its environment variable and an optional `default` tag; the list of
// keys to bind is derived from the struct, so it can't drift.
package config

import (
	"reflect"
	"strings"

	"github.com/spf13/viper"
)

type AppConfig struct {
	Port    int    `mapstructure:"PORT"`
	Debug   bool   `mapstructure:"DEBUG"`
	BaseURL string `mapstructure:"APP_BASE_URL" default:"http://localhost:8000"`
	// MigrateOnStart applies pending database migrations when the API boots.
	MigrateOnStart bool `mapstructure:"MIGRATE_ON_START"`
}

type DatabaseConfig struct {
	DatabaseURI  string `mapstructure:"DATABASE_URI"`
	DatabaseName string `mapstructure:"DATABASE_NAME"`
	DBType       string `mapstructure:"DB_TYPE" default:"memory"`
	// MongoMigrationsConfig points the MongoDB migrator at its YAML config.
	MongoMigrationsConfig string `mapstructure:"MONGO_MIGRATIONS_CONFIG" default:"./cmd/migrate/mongo.yaml"`
}

// FirebaseConfig points at the Firebase project whose ID tokens the API
// accepts. CredentialsFile (a service account JSON key) is optional; it
// enables passkey sign-in (custom tokens) and deleting the Firebase user with
// the account.
type FirebaseConfig struct {
	ProjectID       string `mapstructure:"FIREBASE_PROJECT_ID"`
	CredentialsFile string `mapstructure:"FIREBASE_CREDENTIALS_FILE"`
}

type AuthConfig struct {
	// RequireVerifiedEmail blocks the todos API until the email is verified.
	RequireVerifiedEmail bool `mapstructure:"AUTH_REQUIRE_VERIFIED_EMAIL"`
	// Per-client-IP limit on the unauthenticated /auth endpoints.
	RateLimitPerMinute int `mapstructure:"AUTH_RATE_LIMIT_PER_MINUTE" default:"20"`
	RateLimitBurst     int `mapstructure:"AUTH_RATE_LIMIT_BURST" default:"10"`
}

// LimitsConfig bounds how fast one signed-in user may write.
type LimitsConfig struct {
	// Per-user limit on POST /todos. A team task by an owner or admin pushes
	// a notification to the whole team, so this also bounds notifications.
	TodoCreatesPerMinute int `mapstructure:"TODO_CREATE_RATE_LIMIT_PER_MINUTE" default:"120"`
	TodoCreateBurst      int `mapstructure:"TODO_CREATE_RATE_LIMIT_BURST" default:"30"`
}

// PasskeyConfig names the WebAuthn relying party. Passkeys are bound to RPID
// and only work on pages served from Origins; both default to the host and
// origin of APP_BASE_URL.
type PasskeyConfig struct {
	RPID   string `mapstructure:"PASSKEY_RP_ID"`
	RPName string `mapstructure:"PASSKEY_RP_NAME" default:"Todos"`
	// Origins is a comma-separated list of scheme://host[:port] values.
	Origins string `mapstructure:"PASSKEY_ORIGINS"`
}

// SMTPConfig enables the SMTP mailer when Host is set; otherwise emails are logged.
type SMTPConfig struct {
	Host     string `mapstructure:"SMTP_HOST"`
	Port     int    `mapstructure:"SMTP_PORT" default:"587"`
	Username string `mapstructure:"SMTP_USERNAME"`
	Password string `mapstructure:"SMTP_PASSWORD"`
	From     string `mapstructure:"SMTP_FROM"`
}

type Config struct {
	App      AppConfig      `mapstructure:",squash"`
	DB       DatabaseConfig `mapstructure:",squash"`
	Firebase FirebaseConfig `mapstructure:",squash"`
	Auth     AuthConfig     `mapstructure:",squash"`
	Limits   LimitsConfig   `mapstructure:",squash"`
	Passkey  PasskeyConfig  `mapstructure:",squash"`
	SMTP     SMTPConfig     `mapstructure:",squash"`
}

// setting is one environment variable declared on Config.
type setting struct {
	key, defaultValue string
	hasDefault        bool
}

// settings walks Config (following squashed structs) and returns every key.
func settings() []setting {
	var out []setting
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("mapstructure"), ",")
			if opts == "squash" && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			if name == "" {
				continue
			}
			def, ok := f.Tag.Lookup("default")
			out = append(out, setting{key: name, defaultValue: def, hasDefault: ok})
		}
	}
	walk(reflect.TypeFor[Config]())
	return out
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	_ = viper.ReadInConfig()

	viper.AutomaticEnv()

	// AutomaticEnv only applies to keys viper already knows about, so without
	// a .env file (e.g. in a container) Unmarshal would ignore the environment.
	// Binding every declared key makes env-only configuration work.
	for _, s := range settings() {
		if err := viper.BindEnv(s.key); err != nil {
			return nil, err
		}
		if s.hasDefault {
			viper.SetDefault(s.key, s.defaultValue)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
