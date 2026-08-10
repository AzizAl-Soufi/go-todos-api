package config

import "github.com/spf13/viper"

type AppConfig struct {
	Port  int  `mapstructure:"PORT"`
	Debug bool `mapstructure:"DEBUG"`
}

type DatabaseConfig struct {
	MongoURI string `mapstructure:"MONGO_URI"`
	MongoDBN string `mapstructure:"MONGO_DBN"`
	DBType   string `mapstructure:"DB_TYPE"`
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

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	_ = viper.ReadInConfig()

	viper.AutomaticEnv()

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}