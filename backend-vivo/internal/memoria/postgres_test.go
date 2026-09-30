package memoria

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Prueba contra PostgreSQL + pgvector reales. Solo corre con
// VIVO_TEST_DATABASE_URL apuntando a una base desechable (se vacía entera).
func TestPostgresGuardaYRecuerda(t *testing.T) {
	url := os.Getenv("VIVO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("sin VIVO_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrar(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Migrar(ctx, db); err != nil {
		t.Fatalf("migrar dos veces no debe fallar: %v", err)
	}
	repo := NuevoPostgres(db)
	if _, _, err = repo.BorrarTodas(ctx); err != nil {
		t.Fatal(err)
	}
	_, numeracion, err := repo.Personas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	epoca := numeracion.Epoca

	p := personaDePrueba()
	if err = repo.Guardar(ctx, p, epoca); err != nil {
		t.Fatal(err)
	}
	// Guardar otra vez el estado completo reemplaza, no duplica.
	p.Muestras, p.Apariciones = 20, 3
	p.Vistas = append(p.Vistas, Vista{Tramo: "s1/tel-x/L9/T1", Camara: "tel-x", Prototipo: vectorDePrueba(2), Muestras: 4})
	if err = repo.Guardar(ctx, p, epoca); err != nil {
		t.Fatal(err)
	}
	// Sin vistas (nil) se conservan las que ya tenía.
	p.Vistas, p.Muestras = nil, 21
	if err = repo.Guardar(ctx, p, epoca); err != nil {
		t.Fatal(err)
	}
	otra := personaDePrueba()
	otra.ID, otra.Genero, otra.ConfianzaGenero, otra.Vistas = 3, nil, nil, []Vista{}
	if err = repo.Guardar(ctx, otra, epoca); err != nil {
		t.Fatal(err)
	}

	personas, numeracion, err := repo.Personas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if numeracion.Siguiente != 8 || len(personas) != 2 || personas[0].ID != 3 || personas[1].ID != 7 {
		t.Fatalf("numeracion=%+v personas=%+v", numeracion, personas)
	}
	leida := personas[1]
	if leida.Muestras != 21 || leida.Apariciones != 3 || len(leida.Vistas) != 2 || *leida.Genero != "Mujer" || personas[0].Genero != nil {
		t.Fatalf("persona leída = %+v", leida)
	}
	for i := range p.Suma {
		if leida.Suma[i] != p.Suma[i] {
			t.Fatalf("la suma no volvió igual en %d", i)
		}
	}
	if !leida.UltimaVez.Equal(p.UltimaVez) || leida.Camaras[0] != "tel-a1b2c3d4" {
		t.Fatalf("fechas o cámaras = %v %v", leida.UltimaVez, leida.Camaras)
	}

	// Las fichas van sin vectores y con la vista más reciente primero.
	fichas, err := repo.Fichas(ctx, 10)
	if err != nil || len(fichas) != 2 || fichas[0].Apariciones == 0 || fichas[0].UltimaVez.Before(fichas[1].UltimaVez) {
		t.Fatalf("fichas = %+v, %v", fichas, err)
	}
	if fichas, err = repo.Fichas(ctx, 1); err != nil || len(fichas) != 1 {
		t.Fatalf("fichas con límite = %+v, %v", fichas, err)
	}

	if n, err := repo.Purgar(ctx, p.UltimaVez.Add(time.Second)); err != nil || n != 2 {
		t.Fatalf("purgar = %d, %v", n, err)
	}
	// La numeración no retrocede con la retención: un ID no se reutiliza.
	if s, err := repo.Resumen(ctx); err != nil || s.Personas != 0 || s.Siguiente != 8 {
		t.Fatalf("resumen = %+v, %v", s, err)
	}
	if err = repo.Guardar(ctx, otra, epoca); err != nil {
		t.Fatal(err)
	}
	if err = repo.Borrar(ctx, 3, epoca); err != nil {
		t.Fatal(err)
	}
	if err = repo.Borrar(ctx, 3, epoca); err != nil {
		t.Fatalf("borrar una que ya no está no es error: %v", err)
	}
	if err = repo.Guardar(ctx, otra, epoca); err != nil {
		t.Fatal(err)
	}
	borradas, nueva, err := repo.BorrarTodas(ctx)
	if err != nil || borradas != 1 || nueva != epoca+1 {
		t.Fatalf("olvidar a todos = %d, %d, %v", borradas, nueva, err)
	}
	if s, _ := repo.Resumen(ctx); s.Siguiente != 1 || s.Epoca != nueva || s.Personas != 0 {
		t.Fatalf("olvidar a todos debe reiniciar la numeración: %+v", s)
	}
	// Un guardado que venía de antes del olvido no resucita a nadie.
	if err = repo.Guardar(ctx, otra, epoca); !errors.Is(err, ErrEpoca) {
		t.Fatalf("guardado de época vieja = %v", err)
	}
	if err = repo.Borrar(ctx, 3, epoca); !errors.Is(err, ErrEpoca) {
		t.Fatalf("borrado de época vieja = %v", err)
	}
}
