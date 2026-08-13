package handler

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/GoYoko/web"
	"github.com/labstack/echo/v4"
	"github.com/samber/do"

	"github.com/teteekoue/NemesisCode/backend/config"
)

type StaticHandler struct {
}

func NewStaticHandler(i *do.Injector) (*StaticHandler, error) {
	w := do.MustInvoke[*web.Web](i)
	cfg := do.MustInvoke[*config.Config](i)

	s := &StaticHandler{}

	prefix := strings.TrimSpace(cfg.StaticFiles.RoutePrefix)
	dir := cfg.StaticFiles.Dir

	if prefix == "" || prefix == "/" {
		// Mode « une seule origine » (paquet .deb local) : le frontend est
		// servi à la racine par le backend, avec fallback SPA vers
		// index.html pour les routes du navigateur (/console, /manager…).
		w.Echo().GET("/*", rootStaticHandler(dir))
	} else {
		w.Echo().Static(prefix, dir)
	}
	return s, nil
}

// rootStaticHandler sert les fichiers du répertoire statique à la racine et
// retombe sur index.html (SPA) pour les routes frontend. Les chemins API
// internes non routés restent en 404 (et ne reçoivent jamais le fallback).
func rootStaticHandler(dir string) echo.HandlerFunc {
	indexPath := filepath.Join(dir, "index.html")
	indexInfo, indexErr := os.Stat(indexPath)

	return func(c echo.Context) error {
		p := c.Request().URL.Path
		// Ne jamais intercepter les routes internes de l'API.
		if p == "/health" ||
			strings.HasPrefix(p, "/api/") ||
			strings.HasPrefix(p, "/internal/") ||
			strings.HasPrefix(p, "/v1/") ||
			p == "/mcp" || strings.HasPrefix(p, "/mcp/") {
			return echo.ErrNotFound
		}

		// Chemin relatif sûr (anti traversal) : "./" + path nettoyé.
		rel := strings.TrimPrefix(filepath.Clean("/"+p), "/")
		candidate := filepath.Join(dir, rel)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return c.File(candidate)
		}

		// Fallback SPA : uniquement pour les méthodes GET/HEAD du frontend.
		if indexErr == nil && indexInfo.Mode().IsRegular() {
			return c.File(indexPath)
		}
		return echo.ErrNotFound
	}
}
