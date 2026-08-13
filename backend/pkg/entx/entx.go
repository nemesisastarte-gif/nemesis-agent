package entx

import (
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

type Page struct {
	entc.DefaultExtension
}

func (*Page) Templates() []*gen.Template {
	return []*gen.Template{
		gen.MustParse(gen.NewTemplate("page").
			ParseFiles("../templates/page.tmpl")),
	}
}

type Cursor struct {
	entc.DefaultExtension
}

func (*Cursor) Templates() []*gen.Template {
	return []*gen.Template{
		gen.MustParse(gen.NewTemplate("cursor").
			Funcs(gen.Funcs).
			ParseFiles("../templates/cursor.tmpl")),
	}
}

// IsSQLite indique si la base courante est SQLite (mode local).
func IsSQLite() bool { return sqliteMode }

// WithForUpdate applique ForUpdate() sauf en SQLite : les verrous pessimistes
// PostgreSQL (SELECT ... FOR UPDATE) n'existent pas en SQLite — l'écriture y
// est déjà sérialisée par le verrou d'écriture global de la connexion unique.
func WithForUpdate[T interface{ ForUpdate(...sql.LockOption) T }](q T) T {
	if sqliteMode {
		return q
	}
	return q.ForUpdate()
}
