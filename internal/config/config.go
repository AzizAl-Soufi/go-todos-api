package config

import "github.com/spf13/viper"

type AppConfig struct {
	Port  int  `mapstructure:"PORT"`
	Debug bool `mapstructure:"DEBUG"`
}

type DatabaseConfig struct {
	DatabaseURI  string `mapstructure:"DATABASE_URI"`
	DatabaseName string `mapstructure:"DATABASE_NAME"`
	DBType       string `mapstructure:"DB_TYPE"`
}

type JWTConfig struct {
	PrivateKeyPath string `mapstructure:"JWT_PRIVATE_KEY_PATH"`
	PublicKeyPath  string `mapstructure:"JWT_PUBLIC_KEY_PATH"`
}

type Config struct {
	App AppConfig      `mapstructure:",squash"`
	DB  DatabaseConfig `mapstructure:",squash"`
	JWT JWTConfig      `mapstructure:",squash"`
}

var envKeys = []string{
	"PORT", "DEBUG",
	"DATABASE_URI", "DATABASE_NAME", "DB_TYPE",
	"JWT_PRIVATE_KEY_PATH", "JWT_PUBLIC_KEY_PATH",
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	_ = viper.ReadInConfig()

	viper.AutomaticEnv()

	// AutomaticEnv only applies to keys viper already knows about, so without
	// a .env file (e.g. in a container) Unmarshal would ignore the environment.
	// Binding every key makes env-only configuration work.
	for _, key := range envKeys {
		if err := viper.BindEnv(key); err != nil {
			return nil, err
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
