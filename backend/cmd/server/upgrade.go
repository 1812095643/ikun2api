package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/repository"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/MACOS-DO/sub4api/internal/setup"
	_ "github.com/lib/pq"
	"io"
	"os"
	"strings"
	"time"
)

func runUpgradePreflight() error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load existing configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("open configured database failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect configured database failed")
	}
	return repository.ValidateUpgradeSchema(ctx, db)
}

func runUpgradeBackup(dir, restore string) error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load existing configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("open configured database failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dumper := repository.NewPgDumper(cfg, db)
	if restore != "" {
		f, err := os.Open(restore)
		if err != nil {
			return err
		}
		defer f.Close()
		expected, err := os.ReadFile(restore + ".sha256")
		if err != nil {
			return fmt.Errorf("snapshot integrity record missing")
		}
		actual, err := service.UpgradeFileHash(restore)
		if err != nil || actual != strings.TrimSpace(string(expected)) {
			return fmt.Errorf("snapshot integrity check failed")
		}
		header := make([]byte, 1024*1024)
		n, _ := f.Read(header)
		if _, err = f.Seek(0, 0); err != nil {
			return err
		}
		prefix := "DROP SCHEMA IF EXISTS public CASCADE;\n"
		if !strings.Contains(string(header[:n]), "CREATE SCHEMA public;") && !strings.Contains(string(header[:n]), "CREATE SCHEMA \"public\";") {
			prefix += "CREATE SCHEMA public;\n"
		}
		return dumper.Restore(ctx, io.MultiReader(strings.NewReader(prefix), f))
	}
	if err := repository.ValidateUpgradeSchema(ctx, db); err != nil {
		return err
	}
	return service.WriteUpgradeBackup(ctx, dumper, setup.GetDataDir(), dir)
}

func runUpgradeInfo() error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load existing configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	counts := map[string]int64{}
	digests := map[string]string{}
	for _, table := range []string{"users", "accounts", "api_keys", "groups", "usage_logs", "user_subscriptions"} {
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return fmt.Errorf("inspect required table %s failed", table)
		}
		counts[table] = n
	}
	queries := map[string]string{
		"users":    "SELECT md5(coalesce(string_agg(json_build_array(id,email,password_hash,balance,status,role)::text,',' ORDER BY id),'')) FROM users",
		"accounts": "SELECT md5(coalesce(string_agg(json_build_array(id,platform,type,credentials,proxy_id,deleted_at)::text,',' ORDER BY id),'')) FROM accounts",
		"api_keys": "SELECT md5(coalesce(string_agg(json_build_array(id,user_id,key,status)::text,',' ORDER BY id),'')) FROM api_keys",
	}
	for key, q := range queries {
		var value string
		if err := db.QueryRowContext(ctx, q).Scan(&value); err != nil {
			return fmt.Errorf("inspect %s failed", key)
		}
		digests[key] = value
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"version": Version, "data_dir": setup.GetDataDir(), "server_host": cfg.Server.Host, "server_port": cfg.Server.Port, "counts": counts, "digests": digests})
}
