package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type MapObject struct {
	ID       string          `json:"id"`
	Floor    int             `json:"floor_id"`
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	Category string          `json:"category"`
	Color    string          `json:"color"`
	Source   string          `json:"source_ref"`
	Bearing  float64         `json:"bearing"`
	Geometry json.RawMessage `json:"geometry"`
	Coverage json.RawMessage `json:"coverage"`
	Revision int             `json:"revision"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

const mapColumns = "id,floor_id,kind,name,category,color,source_ref,bearing,ST_AsGeoJSON(geom)::json,ST_AsGeoJSON(coverage)::json,revision"

func scanMap(row pgx.Row, o *MapObject) error {
	return row.Scan(&o.ID, &o.Floor, &o.Kind, &o.Name, &o.Category, &o.Color, &o.Source, &o.Bearing, &o.Geometry, &o.Coverage, &o.Revision)
}
func validGeometry(raw json.RawMessage, kind string) bool {
	var g struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return false
	}
	pointOK := func(p []float64) bool {
		return len(p) == 2 && !math.IsNaN(p[0]) && !math.IsNaN(p[1]) && p[0] >= -77.2086 && p[0] <= -77.0331 && p[1] >= -12.0711 && p[1] <= -11.9813
	}
	if kind == "camera" {
		var p []float64
		return g.Type == "Point" && json.Unmarshal(g.Coordinates, &p) == nil && pointOK(p)
	}
	var rings [][][]float64
	if g.Type != "Polygon" || json.Unmarshal(g.Coordinates, &rings) != nil || len(rings) != 1 || len(rings[0]) < 4 || len(rings[0]) > 101 {
		return false
	}
	r := rings[0]
	for _, p := range r {
		if !pointOK(p) {
			return false
		}
	}
	return r[0][0] == r[len(r)-1][0] && r[0][1] == r[len(r)-1][1]
}
func validateMap(o *MapObject) bool {
	o.Name = strings.TrimSpace(o.Name)
	o.Source = strings.TrimSpace(o.Source)
	if o.Floor != 3 || len(o.Name) == 0 || len(o.Name) > 100 || !colorPattern.MatchString(o.Color) || math.IsNaN(o.Bearing) || o.Bearing < 0 || o.Bearing >= 360 || len(o.Source) > 500 {
		return false
	}
	if o.Kind != "camera" && o.Kind != "zone" {
		return false
	}
	if o.Kind == "zone" {
		if !map[string]bool{"shop": true, "corridor": true, "entry": true, "queue": true, "front": true, "other": true}[o.Category] || o.Source != "" {
			return false
		}
	} else {
		if o.Category != "camera" {
			return false
		}
		if strings.Contains(o.Source, "://") {
			u, e := url.Parse(o.Source)
			if e != nil || u.User != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
				return false
			}
		}
	}
	if !validGeometry(o.Geometry, o.Kind) {
		return false
	}
	if len(o.Coverage) > 0 && string(o.Coverage) != "null" {
		if o.Kind != "camera" || !validGeometry(o.Coverage, "zone") {
			return false
		}
	} else {
		o.Coverage = nil
	}
	return true
}
func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			fail(w, 403, "Origen no permitido")
			return false
		}
	}
	return true
}
func (a *App) mapRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/map-objects", a.listMap)
	m.HandleFunc("POST /api/v1/map-objects", a.saveMap)
	m.HandleFunc("PUT /api/v1/map-objects/{id}", a.saveMap)
	m.HandleFunc("DELETE /api/v1/map-objects/{id}", a.deleteMap)
}
func (a *App) listMap(w http.ResponseWriter, r *http.Request) {
	if f := r.URL.Query().Get("floor_id"); f != "" && f != "3" {
		fail(w, 400, "Solo el nivel 3 está habilitado")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx, "SELECT "+mapColumns+" FROM map_objects WHERE floor_id=3 ORDER BY name,id")
	if err != nil {
		fail(w, 503, "No se pudo cargar la configuración")
		return
	}
	defer rows.Close()
	result := []MapObject{}
	for rows.Next() {
		var o MapObject
		if scanMap(rows, &o) != nil {
			fail(w, 503, "No se pudo leer la configuración")
			return
		}
		result = append(result, o)
	}
	if rows.Err() != nil {
		fail(w, 503, "Lectura interrumpida")
		return
	}
	jsonResponse(w, result)
}
func (a *App) saveMap(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(w, r) {
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "Se requiere application/json")
		return
	}
	var o MapObject
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	d.DisallowUnknownFields()
	if d.Decode(&o) != nil || d.Decode(new(any)) != io.EOF || !validateMap(&o) {
		fail(w, 400, "Datos inválidos: revisa nombre, geometría, color y fuente sin credenciales; máximo 100 vértices, nivel 3")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// Reject crossing polygons in PostGIS before attempting to write.
	var valid bool
	err := a.db.QueryRow(ctx, `SELECT ST_IsValid(ST_GeomFromGeoJSON($1)) AND CASE WHEN $2::text IS NULL THEN true ELSE ST_IsValid(ST_GeomFromGeoJSON($2)) END`, string(o.Geometry), nullableJSON(o.Coverage)).Scan(&valid)
	if err != nil || !valid {
		fail(w, 400, "El polígono se cruza o tiene geometría inválida")
		return
	}
	var saved MapObject
	if r.Method == "POST" {
		o.ID = newID()
		err = scanMap(a.db.QueryRow(ctx, `INSERT INTO map_objects(id,floor_id,kind,name,category,color,source_ref,bearing,geom,coverage) VALUES($1,$2,$3,$4,$5,$6,$7,$8,ST_SetSRID(ST_GeomFromGeoJSON($9),4326),ST_SetSRID(ST_GeomFromGeoJSON($10),4326)) RETURNING `+mapColumns, o.ID, o.Floor, o.Kind, o.Name, o.Category, o.Color, o.Source, o.Bearing, string(o.Geometry), nullableJSON(o.Coverage)), &saved)
	} else {
		if o.Revision < 1 {
			fail(w, 400, "Falta la revisión del elemento")
			return
		}
		err = scanMap(a.db.QueryRow(ctx, `UPDATE map_objects SET name=$2,category=$3,color=$4,source_ref=$5,bearing=$6,geom=ST_SetSRID(ST_GeomFromGeoJSON($7),4326),coverage=ST_SetSRID(ST_GeomFromGeoJSON($8),4326),revision=revision+1,updated_at=now() WHERE id=$1 AND revision=$9 AND kind=$10 AND floor_id=$11 RETURNING `+mapColumns, r.PathValue("id"), o.Name, o.Category, o.Color, o.Source, o.Bearing, string(o.Geometry), nullableJSON(o.Coverage), o.Revision, o.Kind, o.Floor), &saved)
	}
	if err == pgx.ErrNoRows {
		fail(w, 409, "El elemento cambió o fue eliminado. Recarga antes de editar")
		return
	}
	if err != nil {
		fail(w, 503, "No se pudo guardar el elemento")
		return
	}
	if r.Method == "POST" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
	}
	jsonResponse(w, saved)
}
func nullableJSON(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return string(r)
}
func (a *App) deleteMap(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(w, r) {
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
	if err != nil || revision < 1 {
		fail(w, 400, "Falta revisión válida")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.db.Exec(ctx, "DELETE FROM map_objects WHERE id=$1 AND revision=$2", r.PathValue("id"), revision)
	if err != nil {
		fail(w, 503, "No se pudo eliminar")
		return
	}
	if result.RowsAffected() != 1 {
		fail(w, 409, "El elemento cambió o ya fue eliminado; recarga")
		return
	}
	w.WriteHeader(204)
}
