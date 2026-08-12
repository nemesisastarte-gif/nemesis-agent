package store

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/teteekoue/NemesisCode/backend/config"
)

// TestNewRedisCliInMemory vérifie que le fallback Redis en mémoire
// (miniredis) fonctionne pour les usages du backend : clés simples et
// streams (tasker / notify utilisent XADD + XREADGROUP).
func TestNewRedisCliInMemory(t *testing.T) {
	cfg := &config.Config{} // Redis.Host vide → miniredis intégré
	rdb := NewRedisCli(cfg)
	if rdb == nil {
		t.Fatal("nil client")
	}
	defer rdb.Close()

	ctx := context.Background()

	if err := rdb.Set(ctx, "k", "v", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, err := rdb.Get(ctx, "k").Result(); err != nil || v != "v" {
		t.Fatalf("get: err=%v v=%q", err, v)
	}

	// Streams : création de groupe + écriture + lecture de groupe.
	if err := rdb.XGroupCreateMkStream(ctx, "s", "g", "0").Err(); err != nil {
		t.Fatalf("xgroup create: %v", err)
	}
	if err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: "s", Values: map[string]any{"a": "b"}}).Err(); err != nil {
		t.Fatalf("xadd: %v", err)
	}
	res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: "g", Consumer: "c", Streams: []string{"s", ">"},
	}).Result()
	if err != nil {
		t.Fatalf("xreadgroup: %v", err)
	}
	if len(res) != 1 || len(res[0].Messages) != 1 {
		t.Fatalf("unexpected xreadgroup result: %+v", res)
	}

	// Eval (scripts Lua utilisés par le backend).
	if _, err := rdb.Eval(ctx, "return 1", nil).Result(); err != nil {
		t.Fatalf("eval: %v", err)
	}
}

// TestNewRedisCliExplicit vérifie que le mode « vrai Redis » construit bien
// un client sur l'adresse configurée (sans connexion).
func TestNewRedisCliExplicit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 6379
	rdb := NewRedisCli(cfg)
	if rdb == nil {
		t.Fatal("nil client")
	}
	rdb.Close()
}
