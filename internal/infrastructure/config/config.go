package config

import (
	"os"
	"strings"
)

// Supabase is only the identity provider (JWT issuer); all data lives in MongoDB.
type Config struct {
	Port                string
	MongoDBURI          string
	MongoDBName         string
	AllowedOrigins      []string
	DefaultAssetClasses []string
	LiquidAssetClasses  []string
	AuthAudience        string
	JWKSetURI           string
	AuthIssuer          string
	DevUserID           string // non-empty disables JWT validation; never honored on Vercel
}

func LoadConfig() Config {
	supabaseURL := strings.TrimRight(getEnv("SUPABASE_URL", "https://rhzumppfruperaivykhe.supabase.co"), "/")

	return Config{
		Port:        getEnv("PORT", "8080"),
		MongoDBURI:  getEnv("MONGODB_URI", "mongodb://localhost:27017"), // local default matches compose.yaml
		MongoDBName: getEnv("MONGODB_DATABASE", "base_wealth"),
		// Preview deployments of the frontend live under *.vercel.app; set FRONTEND_ORIGIN to lock it down.
		AllowedOrigins:      splitList(getEnv("FRONTEND_ORIGIN", "http://localhost:3000,http://127.0.0.1:3000,https://*.vercel.app")),
		DefaultAssetClasses: splitList(getEnv("WEALTH_DEFAULT_ASSET_CLASSES", "Cash,Fixed Income,Index Fund,Equity,Crypto")),
		LiquidAssetClasses:  splitList(getEnv("WEALTH_LIQUID_ASSET_CLASSES", "Cash,Equity,Crypto,Index Fund")),
		AuthAudience:        getEnv("SUPABASE_AUDIENCE", "authenticated"),
		JWKSetURI:           getEnv("SUPABASE_JWKS_URI", supabaseURL+"/auth/v1/.well-known/jwks.json"),
		AuthIssuer:          getEnv("SUPABASE_ISSUER", supabaseURL+"/auth/v1"),
		DevUserID:           devUserID(),
	}
}

// devUserID is ignored when VERCEL is set (Vercel injects it into every deployment), so a stray
// AUTH_DEV_USER_ID in the project settings can't switch off auth in production.
func devUserID() string {
	if os.Getenv("VERCEL") != "" {
		return ""
	}
	return os.Getenv("AUTH_DEV_USER_ID")
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
