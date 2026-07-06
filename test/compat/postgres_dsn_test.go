package compat_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAppDSNConformance(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://postgres:changeme@postgres:5432/app?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.ConnConfig.Host != "postgres" || cfg.ConnConfig.Port != 5432 || cfg.ConnConfig.Database != "app" {
		t.Fatalf("parsed DSN = host %q port %d database %q", cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database)
	}
}
