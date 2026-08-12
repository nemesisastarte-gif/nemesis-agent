package store

import (
	"fmt"
	"net"
	"sync"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/teteekoue/NemesisCode/backend/config"
)

// miniRedis est un serveur Redis en mémoire (miniredis) utilisé en secours
// quand aucune adresse Redis n'est configurée (mode local sans Docker).
var (
	miniOnce sync.Once
	miniAddr string
)

// NewRedisCli crée le client Redis. Si cfg.Redis.Host est vide, un serveur
// Redis en mémoire (miniredis) est démarré dans le processus — aucune
// dépendance externe, mais les données ne survivent pas au redémarrage
// (acceptable en usage local mono-instance).
func NewRedisCli(cfg *config.Config) *redis.Client {
	if cfg.Redis.Host == "" {
		miniOnce.Do(func() {
			m := miniredis.NewMiniRedis()
			if err := m.Start(); err != nil {
				panic(fmt.Errorf("start in-memory redis: %w", err))
			}
			miniAddr = m.Addr()
		})
		return redis.NewClient(&redis.Options{
			Addr: miniAddr,
			DB:   cfg.Redis.DB,
		})
	}

	addr := net.JoinHostPort(cfg.Redis.Host, fmt.Sprintf("%d", cfg.Redis.Port))
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Redis.Pass,
		DB:           cfg.Redis.DB,
		MaxIdleConns: 3,
	})
	return rdb
}
