package config

import "github.com/spf13/viper"

type Config struct {
	Port     int    `mapstructure:"PORT"`
	Debug    bool   `mapstructure:"DEBUG"`
	MongoURI string `mapstructure:"MONGO_URI"`
	MongoDBN string `mapstructure:"MONGO_DBN"`
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
