package memoria

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"vivo/migrations"
)

// Postgres implementa Repositorio sobre la base propia de las cámaras en vivo.
// Los vectores viajan como texto de pgvector ([1,2,3]) con cast explícito.
type Postgres struct {
	db *pgxpool.Pool
}

func NuevoPostgres(db *pgxpool.Pool) *Postgres { return &Postgres{db: db} }

// Migrar aplica, en una transacción y en orden, las migraciones pendientes.
func Migrar(ctx context.Context, db *pgxpool.Pool) error {
	archivos, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(archivos)
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(310002)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	for _, nombre := range archivos {
		var aplicada bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", nombre).Scan(&aplicada); err != nil {
			return err
		}
		if aplicada {
			continue
		}
		sql, err := fs.ReadFile(migrations.FS, nombre)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", nombre, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", nombre); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Postgres) Personas(ctx context.Context) ([]Persona, Numeracion, error) {
	var n Numeracion
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, n, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT siguiente, epoca FROM numeracion").Scan(&n.Siguiente, &n.Epoca); err != nil {
		return nil, n, err
	}
	filas, err := tx.Query(ctx, `SELECT id, suma::text, muestras, genero, confianza_genero, camaras, apariciones,
	                                    primera_vez, ultima_vez
	                             FROM personas ORDER BY id`)
	if err != nil {
		return nil, n, err
	}
	personas := []Persona{}
	indice := map[int64]int{}
	for filas.Next() {
		var p Persona
		var suma string
		if err = filas.Scan(&p.ID, &suma, &p.Muestras, &p.Genero, &p.ConfianzaGenero, &p.Camaras, &p.Apariciones,
			&p.PrimeraVez, &p.UltimaVez); err != nil {
			filas.Close()
			return nil, n, err
		}
		if p.Suma, err = ParsearVector(suma); err != nil {
			filas.Close()
			return nil, n, err
		}
		p.Vistas = []Vista{}
		indice[p.ID] = len(personas)
		personas = append(personas, p)
	}
	if err = filas.Err(); err != nil {
		return nil, n, err
	}
	filas, err = tx.Query(ctx, "SELECT persona_id, tramo, camara, prototipo::text, muestras FROM vistas ORDER BY persona_id, tramo")
	if err != nil {
		return nil, n, err
	}
	defer filas.Close()
	for filas.Next() {
		var id int64
		var v Vista
		var prototipo string
		if err = filas.Scan(&id, &v.Tramo, &v.Camara, &prototipo, &v.Muestras); err != nil {
			return nil, n, err
		}
		if v.Prototipo, err = ParsearVector(prototipo); err != nil {
			return nil, n, err
		}
		if i, ok := indice[id]; ok {
			personas[i].Vistas = append(personas[i].Vistas, v)
		}
	}
	return personas, n, filas.Err()
}

// comprobarEpoca bloquea la numeración hasta el final de la transacción: un
// «olvidar a todos» no puede colarse entre la comprobación y el cambio.
func comprobarEpoca(ctx context.Context, tx pgx.Tx, epoca int64) error {
	var actual int64
	if err := tx.QueryRow(ctx, "SELECT epoca FROM numeracion FOR UPDATE").Scan(&actual); err != nil {
		return err
	}
	if actual != epoca {
		return ErrEpoca
	}
	return nil
}

func (r *Postgres) Guardar(ctx context.Context, p Persona, epoca int64) error {
	camaras := p.Camaras
	if camaras == nil {
		camaras = []string{}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = comprobarEpoca(ctx, tx, epoca); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO personas (id, suma, muestras, genero, confianza_genero, camaras, apariciones, primera_vez, ultima_vez)
	                       VALUES ($1, $2::text::vector, $3, $4, $5, $6, $7, $8, $9)
	                       ON CONFLICT (id) DO UPDATE SET suma = EXCLUDED.suma, muestras = EXCLUDED.muestras,
	                           genero = EXCLUDED.genero, confianza_genero = EXCLUDED.confianza_genero,
	                           camaras = EXCLUDED.camaras, apariciones = EXCLUDED.apariciones,
	                           primera_vez = EXCLUDED.primera_vez, ultima_vez = EXCLUDED.ultima_vez`,
		p.ID, p.Suma.Texto(), p.Muestras, p.Genero, p.ConfianzaGenero, camaras, p.Apariciones, p.PrimeraVez, p.UltimaVez)
	if err != nil {
		return err
	}
	if p.Vistas != nil {
		if _, err = tx.Exec(ctx, "DELETE FROM vistas WHERE persona_id = $1", p.ID); err != nil {
			return err
		}
		if len(p.Vistas) > 0 {
			lote := &pgx.Batch{}
			for _, v := range p.Vistas {
				lote.Queue("INSERT INTO vistas (persona_id, tramo, camara, prototipo, muestras) VALUES ($1, $2, $3, $4::text::vector, $5)",
					p.ID, v.Tramo, v.Camara, v.Prototipo.Texto(), v.Muestras)
			}
			if err = tx.SendBatch(ctx, lote).Close(); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE numeracion SET siguiente = GREATEST(siguiente, $1 + 1)", p.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Postgres) Borrar(ctx context.Context, id, epoca int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = comprobarEpoca(ctx, tx, epoca); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM personas WHERE id = $1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Postgres) BorrarTodas(ctx context.Context) (int64, int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	var epoca int64
	if err = tx.QueryRow(ctx, "UPDATE numeracion SET siguiente = 1, epoca = epoca + 1 RETURNING epoca").Scan(&epoca); err != nil {
		return 0, 0, err
	}
	resultado, err := tx.Exec(ctx, "DELETE FROM personas")
	if err != nil {
		return 0, 0, err
	}
	return resultado.RowsAffected(), epoca, tx.Commit(ctx)
}

func (r *Postgres) Purgar(ctx context.Context, antes time.Time) (int64, error) {
	resultado, err := r.db.Exec(ctx, "DELETE FROM personas WHERE ultima_vez < $1", antes)
	if err != nil {
		return 0, err
	}
	return resultado.RowsAffected(), nil
}

func (r *Postgres) Resumen(ctx context.Context) (Resumen, error) {
	var s Resumen
	err := r.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM personas), (SELECT count(*) FROM vistas), siguiente, epoca
	                           FROM numeracion`).Scan(&s.Personas, &s.Vistas, &s.Siguiente, &s.Epoca)
	return s, err
}
