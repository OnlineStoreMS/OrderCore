package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	Auth         AuthConfig
	Integrations IntegrationsConfig
	Storage      StorageConfig
	CORS         CORSConfig
}

type ServerConfig struct {
	Port int
	Mode string
}

type DatabaseConfig struct {
	Driver      string
	SQLitePath  string
	PostgresDSN string `mapstructure:"postgres_dsn"`
}

type AuthConfig struct {
	Enabled   bool
	JWTSecret string `mapstructure:"jwt_secret"`
}

type IntegrationsConfig struct {
	ProductCoreAPIURL     string `mapstructure:"productcore_api_url"`
	SupplyCoreAPIURL      string `mapstructure:"supplycore_api_url"`
	StoreSyncAgentAPIURL  string `mapstructure:"storesyncagent_api_url"`
	StoreCoreAPIURL       string `mapstructure:"storecore_api_url"`
}

type StorageConfig struct {
	Driver        string      `mapstructure:"driver"`
	LocalPath     string      `mapstructure:"local_path"`
	PublicBaseURL string      `mapstructure:"public_base_url"`
	Prefix        string      `mapstructure:"prefix"`
	MinIO         MinIOConfig `mapstructure:"minio"`
}

type MinIOConfig struct {
	Endpoint   string
	AccessKey  string `mapstructure:"access_key"`
	SecretKey  string `mapstructure:"secret_key"`
	Bucket     string
	UseSSL     bool   `mapstructure:"use_ssl"`
	Prefix     string
	PublicRead bool   `mapstructure:"public_read"`
}

type CORSConfig struct {
	AllowOrigins []string `mapstructure:"allow_origins"`
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8098
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "postgres"
	}
	if cfg.Database.SQLitePath == "" {
		cfg.Database.SQLitePath = "./data/ordercore.db"
	}
	if cfg.Database.PostgresDSN == "" {
		cfg.Database.PostgresDSN = "host=127.0.0.1 user=ordercore password=ordercore dbname=ordercore port=5432 sslmode=disable TimeZone=Asia/Shanghai"
	}
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = "change-me-in-production-use-long-random-string"
	}
	if cfg.Integrations.ProductCoreAPIURL == "" {
		cfg.Integrations.ProductCoreAPIURL = "http://127.0.0.1:8090"
	}
	if cfg.Integrations.SupplyCoreAPIURL == "" {
		cfg.Integrations.SupplyCoreAPIURL = "http://127.0.0.1:8092"
	}
	if cfg.Integrations.StoreSyncAgentAPIURL == "" {
		cfg.Integrations.StoreSyncAgentAPIURL = "http://127.0.0.1:8097"
	}
	if cfg.Integrations.StoreCoreAPIURL == "" {
		cfg.Integrations.StoreCoreAPIURL = "http://127.0.0.1:8094"
	}
	if cfg.Storage.LocalPath == "" {
		cfg.Storage.LocalPath = "./data/uploads"
	}
	if cfg.Storage.Driver == "" {
		cfg.Storage.Driver = "local"
	}
	if cfg.Storage.PublicBaseURL == "" {
		if cfg.Storage.Driver == "minio" {
			cfg.Storage.PublicBaseURL = "http://127.0.0.1:9100/ordercore"
		} else {
			cfg.Storage.PublicBaseURL = "http://127.0.0.1:8098/uploads"
		}
	}
	if cfg.Storage.Prefix == "" {
		cfg.Storage.Prefix = "attachments"
	}
	if cfg.Storage.MinIO.Prefix == "" {
		cfg.Storage.MinIO.Prefix = cfg.Storage.Prefix
	}
	if !v.IsSet("storage.minio.public_read") {
		cfg.Storage.MinIO.PublicRead = true
	}
	if len(cfg.CORS.AllowOrigins) == 0 {
		cfg.CORS.AllowOrigins = []string{
			"http://localhost:5182",
			"http://127.0.0.1:5182",
			"http://localhost:5174",
			"http://127.0.0.1:5174",
		}
	}
	return &cfg, nil
}
