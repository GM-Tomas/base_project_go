package config

import (
	"os"
	"strings"
)

type Config struct {
	Port                string
	DBType              string // "postgres" or "mongodb"
	DatabaseURL         string
	MongoDBURI          string
	MongoDBName         string
	AllowedOrigins      []string
	DefaultAssetClasses []string
	LiquidAssetClasses  []string
	AuthAudience        string
	JWKSetURI           string
	AuthIssuer          string
}

func LoadConfig() Config {
	supabaseURL := strings.TrimRight(getEnv("SUPABASE_URL", "https://vffsdgqqyqcbmkehnpxx.supabase.co"), "/")

	mongoURI := getEnv("MONGODB_URI", "")
	dbType := strings.ToLower(getEnv("DB_TYPE", ""))
	if dbType == "" {
		if mongoURI != "" {
			dbType = "mongodb"
		} else {
			dbType = "postgres"
		}
	}

	return Config{
		Port:        getEnv("PORT", "8080"),
		DBType:      dbType,
		DatabaseURL: getEnv("SUPABASE_DB_URL", getEnv("DATABASE_URL", "postgres://base_wealth:base_wealth@localhost:5432/base_wealth?sslmode=disable")),
		MongoDBURI:  mongoURI,
		MongoDBName: getEnv("MONGODB_DATABASE", "base_wealth"),
		// Preview deployments of the frontend live under *.vercel.app; set FRONTEND_ORIGIN to lock it down.
		AllowedOrigins:      splitList(getEnv("FRONTEND_ORIGIN", "http://localhost:3000,http://127.0.0.1:3000,https://*.vercel.app")),
		DefaultAssetClasses: splitList(getEnv("WEALTH_DEFAULT_ASSET_CLASSES", "Cash,Fixed Income,Index Fund,Equity,Crypto")),
		LiquidAssetClasses:  splitList(getEnv("WEALTH_LIQUID_ASSET_CLASSES", "Cash,Equity,Crypto,Index Fund")),
		AuthAudience:        getEnv("SUPABASE_AUDIENCE", "authenticated"),
		JWKSetURI:           getEnv("SUPABASE_JWKS_URI", supabaseURL+"/auth/v1/.well-known/jwks.json"),
		AuthIssuer:          getEnv("SUPABASE_ISSUER", supabaseURL+"/auth/v1"),
	}
}

func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
