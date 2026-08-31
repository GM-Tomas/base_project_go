package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                string
	SupabaseURL         string
	DatabaseURL         string
	DatabaseUser        string
	DatabasePassword    string
	AllowedOrigins      []string
	DefaultFxUsdArs     float64
	DefaultAssetClasses []string
	LiquidAssetClasses  []string
	AuthAudience        string
	JWKSetURI           string
	AuthIssuer          string
}

func LoadConfig() Config {
	port := getEnv("PORT", "8080")
	supabaseURL := getEnv("SUPABASE_URL", "https://vffsdgqqyqcbmkehnpxx.supabase.co")

	jwkSetURI := getEnv("SUPABASE_JWKS_URI", supabaseURL+"/auth/v1/.well-known/jwks.json")
	authIssuer := getEnv("SUPABASE_ISSUER", supabaseURL+"/auth/v1")
	authAudience := getEnv("SUPABASE_AUDIENCE", "authenticated")

	// Origins
	corsEnv := getEnv("FRONTEND_ORIGIN", "")
	var allowedOrigins []string
	if corsEnv != "" {
		for _, o := range strings.Split(corsEnv, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	} else {
		allowedOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"https://*.vercel.app",
		}
	}

	fxRateStr := getEnv("WEALTH_DEFAULT_FX_USD_ARS", "1050.0")
	fxRate, err := strconv.ParseFloat(fxRateStr, 64)
	if err != nil || fxRate <= 0 {
		fxRate = 1050.0
	}

	defaultClassesStr := getEnv("WEALTH_DEFAULT_ASSET_CLASSES", "Cash,Fixed Income,Index Fund,Equity,Crypto")
	var defaultClasses []string
	for _, c := range strings.Split(defaultClassesStr, ",") {
		trimmed := strings.TrimSpace(c)
		if trimmed != "" {
			defaultClasses = append(defaultClasses, trimmed)
		}
	}

	liquidClassesStr := getEnv("WEALTH_LIQUID_ASSET_CLASSES", "Cash,Equity,Crypto,Index Fund")
	var liquidClasses []string
	for _, c := range strings.Split(liquidClassesStr, ",") {
		trimmed := strings.TrimSpace(c)
		if trimmed != "" {
			liquidClasses = append(liquidClasses, trimmed)
		}
	}

	dbURL := getEnv("SUPABASE_DB_URL", getEnv("DATABASE_URL", "postgres://base_wealth:base_wealth@localhost:5432/base_wealth?sslmode=disable"))
	dbUser := getEnv("SUPABASE_DB_USER", "")
	dbPassword := getEnv("SUPABASE_DB_PASSWORD", "")

	return Config{
		Port:                port,
		SupabaseURL:         supabaseURL,
		DatabaseURL:         dbURL,
		DatabaseUser:        dbUser,
		DatabasePassword:    dbPassword,
		AllowedOrigins:      allowedOrigins,
		DefaultFxUsdArs:     fxRate,
		DefaultAssetClasses: defaultClasses,
		LiquidAssetClasses:  liquidClasses,
		AuthAudience:        authAudience,
		JWKSetURI:           jwkSetURI,
		AuthIssuer:          authIssuer,
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
