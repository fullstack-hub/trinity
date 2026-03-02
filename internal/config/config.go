package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	URL  string   `yaml:"url"`
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`
	Dir  string   `yaml:"dir"`
}

type Config struct {
	Servers      map[string]ServerConfig `yaml:"servers"`
	DefaultAgent string                  `yaml:"default_agent"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
