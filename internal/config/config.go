package config

import (
	"encoding/json"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Port              string `mapstructure:"PORT"`
	MongoDBURI        string `mapstructure:"MONGODB_URI"`
	DatabaseName      string `mapstructure:"DATABASE_NAME"`
	FirebaseProjectID string `mapstructure:"FIREBASE_PROJECT_ID"`
	JWTSecret         string `mapstructure:"JWT_SECRET"`
	JWTExpire         string `mapstructure:"JWT_EXPIRE"`

	// Firebase Service Account fields (from environment variables)
	FirebaseType                    string `mapstructure:"FIREBASE_TYPE"`
	FirebasePrivateKeyID            string `mapstructure:"FIREBASE_PRIVATE_KEY_ID"`
	FirebasePrivateKey              string `mapstructure:"FIREBASE_PRIVATE_KEY"`
	FirebaseClientEmail             string `mapstructure:"FIREBASE_CLIENT_EMAIL"`
	FirebaseClientID                string `mapstructure:"FIREBASE_CLIENT_ID"`
	FirebaseAuthURI                 string `mapstructure:"FIREBASE_AUTH_URI"`
	FirebaseTokenURI                string `mapstructure:"FIREBASE_TOKEN_URI"`
	FirebaseAuthProviderX509CertURL string `mapstructure:"FIREBASE_AUTH_PROVIDER_X509_CERT_URL"`
	FirebaseClientX509CertURL       string `mapstructure:"FIREBASE_CLIENT_X509_CERT_URL"`
	FirebaseUniverseDomain          string `mapstructure:"FIREBASE_UNIVERSE_DOMAIN"`
}

// GetFirebaseCredentialsJSON returns the Firebase service account credentials as JSON bytes.
// Returns nil if required fields are not set.
func (c *Config) GetFirebaseCredentialsJSON() []byte {
	if c.FirebasePrivateKey == "" || c.FirebaseClientEmail == "" {
		return nil
	}

	creds := map[string]string{
		"type":                        c.FirebaseType,
		"project_id":                  c.FirebaseProjectID,
		"private_key_id":              c.FirebasePrivateKeyID,
		"private_key":                 c.FirebasePrivateKey,
		"client_email":                c.FirebaseClientEmail,
		"client_id":                   c.FirebaseClientID,
		"auth_uri":                    c.FirebaseAuthURI,
		"token_uri":                   c.FirebaseTokenURI,
		"auth_provider_x509_cert_url": c.FirebaseAuthProviderX509CertURL,
		"client_x509_cert_url":        c.FirebaseClientX509CertURL,
		"universe_domain":             c.FirebaseUniverseDomain,
	}

	jsonBytes, err := json.Marshal(creds)
	if err != nil {
		log.Printf("Failed to marshal Firebase credentials: %v", err)
		return nil
	}

	return jsonBytes
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("PORT", "8080")
	viper.SetDefault("DATABASE_NAME", "chat_db")
	viper.SetDefault("FIREBASE_TYPE", "service_account")
	viper.SetDefault("FIREBASE_AUTH_URI", "https://accounts.google.com/o/oauth2/auth")
	viper.SetDefault("FIREBASE_TOKEN_URI", "https://oauth2.googleapis.com/token")
	viper.SetDefault("FIREBASE_AUTH_PROVIDER_X509_CERT_URL", "https://www.googleapis.com/oauth2/v1/certs")
	viper.SetDefault("FIREBASE_UNIVERSE_DOMAIN", "googleapis.com")

	if err := viper.ReadInConfig(); err != nil {
		// Ignore all config file errors - rely on environment variables in production
		log.Println("No .env file found, relying on environment variables")
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func Load() *Config {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	return cfg
}
