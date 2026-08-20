// Package config carrega a configuração a partir do ambiente e falha ruidosamente
// quando algo obrigatório está faltando — configuração incompleta é erro de boot,
// não surpresa em produção.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	JWTSecret   string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
	CORSOrigins []string
	LogLevel    string
	Timezone    string
}

// Load lê o ambiente. Em desenvolvimento aceita padrões; em produção exige os
// segredos explicitamente.
func Load() (Config, error) {
	c := Config{
		Env:         env("APP_ENV", "development"),
		Port:        env("API_PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", ""),
		JWTSecret:   env("JWT_SECRET", ""),
		LogLevel:    env("LOG_LEVEL", "info"),
		Timezone:    env("TZ", "America/Fortaleza"),
		CORSOrigins: split(env("CORS_ORIGINS", "http://localhost:3000")),
	}

	var err error
	if c.AccessTTL, err = time.ParseDuration(env("JWT_ACCESS_TTL", "15m")); err != nil {
		return c, fmt.Errorf("JWT_ACCESS_TTL inválido: %w", err)
	}
	if c.RefreshTTL, err = time.ParseDuration(env("JWT_REFRESH_TTL", "720h")); err != nil {
		return c, fmt.Errorf("JWT_REFRESH_TTL inválido: %w", err)
	}

	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL é obrigatório")
	}
	if c.Env == "production" && len(c.JWTSecret) < 32 {
		return c, fmt.Errorf("JWT_SECRET precisa de ao menos 32 bytes em produção")
	}
	return c, nil
}

func (c Config) IsProduction() bool { return c.Env == "production" }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func split(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
