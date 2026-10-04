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
	DevUserID           string // non-empty: requests WITHOUT a token act as this user (tokens are still verified); never honored on Vercel
	DocsPassword        string // non-empty puts Swagger/OpenAPI behind basic auth (user "docs")
	HideDocs            bool   // on Vercel without DOCS_PASSWORD the docs aren't served at all
	// Preview is a Vercel preview deployment. The project's MONGODB_URI reaches previews too, so a preview
	// never connects to the database (see app.BuildApp); app previews run on demo data in the browser.
	Preview bool
}

func LoadConfig() Config {
	supabaseURL := strings.TrimRight(getEnv("SUPABASE_URL", "https://rhzumppfruperaivykhe.supabase.co"), "/")

	return Config{
		Port:        getEnv("PORT", "8080"),
		MongoDBURI:  getEnv("MONGODB_URI", "mongodb://localhost:27017"), // local default matches compose.yaml
		MongoDBName: getEnv("MONGODB_DATABASE", "base_wealth"),
		// Never "*.vercel.app": anyone can deploy there. FRONTEND_ORIGIN overrides (e.g. a custom domain).
		AllowedOrigins:      splitList(getEnv("FRONTEND_ORIGIN", "http://localhost:3000,http://127.0.0.1:3000,https://base-project-fe.vercel.app")),
		DefaultAssetClasses: splitList(getEnv("WEALTH_DEFAULT_ASSET_CLASSES", "Cash,Fixed Income,Index Fund,Equity,Crypto")),
		LiquidAssetClasses:  splitList(getEnv("WEALTH_LIQUID_ASSET_CLASSES", "Cash,Equity,Crypto,Index Fund")),
		AuthAudience:        getEnv("SUPABASE_AUDIENCE", "authenticated"),
		JWKSetURI:           getEnv("SUPABASE_JWKS_URI", supabaseURL+"/auth/v1/.well-known/jwks.json"),
		AuthIssuer:          getEnv("SUPABASE_ISSUER", supabaseURL+"/auth/v1"),
		DevUserID:           devUserID(),
		DocsPassword:        os.Getenv("DOCS_PASSWORD"),
		HideDocs:            os.Getenv("VERCEL") != "" && os.Getenv("DOCS_PASSWORD") == "",
		Preview:             vercelTarget() == "preview",
	}
}

// vercelTarget is the Vercel environment a deployment belongs to: production, preview, or the name of a
// custom environment (which VERCEL_ENV reports as "preview", so it's only the fallback).
func vercelTarget() string {
	if target := os.Getenv("VERCEL_TARGET_ENV"); target != "" {
		return target
	}
	return os.Getenv("VERCEL_ENV")
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
