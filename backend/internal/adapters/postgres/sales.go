package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// SalesRepository stores tickets, their matches with people and the simulated
// visit counts.
type SalesRepository struct {
	db *pgxpool.Pool
}

func NewSalesRepository(db *pgxpool.Pool) *SalesRepository { return &SalesRepository{db: db} }

var (
	farPast   = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	farFuture = time.Date(9000, 1, 1, 0, 0, 0, 0, time.UTC)
)

// bounds fills the open ends of a selection.
func bounds(f domain.SalesFilter) (time.Time, time.Time) {
	from, to := f.From, f.To
	if from.IsZero() {
		from = farPast
	}
	if to.IsZero() {
		to = farFuture
	}
	return from, to
}

// salesWhere is the condition shared by every ticket query ($1 site, $2 from, $3 to, $4 local, $5 origin).
const salesWhere = `t.site_id = $1 AND t.sold_at >= $2 AND t.sold_at < $3 AND ($4::int[] IS NULL OR t.local_id = ANY($4)) AND ($5 = '' OR t.origin = $5)`

func (r *SalesRepository) CreateTickets(ctx context.Context, site domain.Site, tickets []domain.Ticket) (domain.TicketsCreated, error) {
	var out domain.TicketsCreated
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	if err = checkLocals(ctx, tx, site, tickets); err != nil {
		return out, err
	}
	batch := &pgx.Batch{}
	for _, t := range tickets {
		batch.Queue(`INSERT INTO sales_tickets (ticket_id, site_id, local_id, sold_at, total, origin, external_ref)
			VALUES ($1::text::uuid, $2, $3, $4, $5, $6, NULLIF($7, '')) ON CONFLICT DO NOTHING`,
			t.ID, site.ID, t.LocalID, t.SoldAt, t.Total, t.Origin, t.ExternalRef)
	}
	results := tx.SendBatch(ctx, batch)
	created := make([]domain.Ticket, 0, len(tickets))
	for _, t := range tickets {
		tag, execErr := results.Exec()
		if execErr != nil {
			results.Close()
			return out, rejected(execErr)
		}
		if tag.RowsAffected() == 1 {
			created = append(created, t)
		}
	}
	if err = results.Close(); err != nil {
		return out, rejected(err)
	}
	if err = copyItems(ctx, tx, created); err != nil {
		return out, rejected(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return domain.TicketsCreated{Created: len(created), Duplicates: len(tickets) - len(created)}, nil
}

// checkLocals verifies that every ticket's local belongs to the site.
func checkLocals(ctx context.Context, tx pgx.Tx, site domain.Site, tickets []domain.Ticket) error {
	rows, err := tx.Query(ctx, "SELECT id FROM locales WHERE site_id = $1", site.ID)
	if err != nil {
		return err
	}
	known := map[int]bool{}
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for i, t := range tickets {
		if !known[t.LocalID] {
			return domain.Invalid("ticket " + strconv.Itoa(i+1) + ": el local indicado no pertenece al sitio")
		}
	}
	return nil
}

func copyItems(ctx context.Context, tx pgx.Tx, tickets []domain.Ticket) error {
	rows := make([][]any, 0, len(tickets)*2)
	for _, t := range tickets {
		id, err := domain.ParseUUID(t.ID)
		if err != nil {
			return err
		}
		for _, it := range t.Items {
			rows = append(rows, []any{id, it.Product, it.Category, it.Quantity, it.UnitPrice})
		}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"sales_items"}, []string{"ticket_id", "product", "category", "quantity", "unit_price"}, pgx.CopyFromRows(rows))
	return err
}

// copyTickets stores tickets that cannot collide with stored ones (the simulated ones, after the old ones were removed).
func copyTickets(ctx context.Context, tx pgx.Tx, site domain.Site, tickets []domain.Ticket) error {
	rows := make([][]any, 0, len(tickets))
	for _, t := range tickets {
		id, err := domain.ParseUUID(t.ID)
		if err != nil {
			return err
		}
		rows = append(rows, []any{id, site.ID, t.LocalID, t.SoldAt, t.Total, t.Origin})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"sales_tickets"}, []string{"ticket_id", "site_id", "local_id", "sold_at", "total", "origin"}, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return copyItems(ctx, tx, tickets)
}

func (r *SalesRepository) ReplaceSimulated(ctx context.Context, site domain.Site, tickets []domain.Ticket, visits []domain.VisitCount) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	if err = checkLocals(ctx, tx, site, tickets); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sales_tickets WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM visit_counts WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID); err != nil {
		return err
	}
	if err = copyTickets(ctx, tx, site, tickets); err != nil {
		return rejected(err)
	}
	rows := make([][]any, 0, len(visits))
	for _, v := range visits {
		rows = append(rows, []any{site.ID, v.LocalID, v.Hour, v.Visitors, v.Origin})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"visit_counts"}, []string{"site_id", "local_id", "hour", "visitors", "origin"}, pgx.CopyFromRows(rows)); err != nil {
		return rejected(err)
	}
	return tx.Commit(ctx)
}

func (r *SalesRepository) DeleteSimulated(ctx context.Context, site domain.Site) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "DELETE FROM sales_tickets WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM visit_counts WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), tx.Commit(ctx)
}

func (r *SalesRepository) DeleteTicket(ctx context.Context, site domain.Site, id string) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM sales_tickets WHERE site_id = $1 AND ticket_id = $2::text::uuid", site.ID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("Ticket no encontrado")
	}
	return nil
}

func (r *SalesRepository) Tickets(ctx context.Context, site domain.Site, f domain.TicketFilter) (domain.TicketPage, error) {
	page := domain.TicketPage{Tickets: []domain.Ticket{}}
	from, to := bounds(f.SalesFilter)
	args := []any{site.ID, from, to, f.LocalArray(), f.Origin()}
	if err := r.db.QueryRow(ctx, "SELECT count(*) FROM sales_tickets t WHERE "+salesWhere, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	index := map[string]*domain.Ticket{}
	var ids []string
	err := collect(ctx, r.db, `SELECT t.ticket_id::text, t.local_id, l.name, t.sold_at, t.total::float8, t.origin, COALESCE(t.external_ref, ''),
			m.session_id::text, i.public_number, i.gender_estimate, m.stay_start, m.stay_end, m.offset_s, m.confidence
		FROM sales_tickets t
		JOIN locales l ON l.id = t.local_id
		LEFT JOIN sales_matches m ON m.ticket_id = t.ticket_id
		LEFT JOIN identities i ON i.global_id = m.global_id
		WHERE `+salesWhere+` ORDER BY t.sold_at DESC, t.ticket_id LIMIT $6 OFFSET $7`, append(args, f.Limit, f.Offset), func(row pgx.Rows) error {
		var t domain.Ticket
		var session, gender *string
		var person *int32
		var start, end *time.Time
		var offset, confidence *float32
		if err := row.Scan(&t.ID, &t.LocalID, &t.Local, &t.SoldAt, &t.Total, &t.Origin, &t.ExternalRef, &session, &person, &gender, &start, &end, &offset, &confidence); err != nil {
			return err
		}
		t.Items = []domain.TicketItem{}
		if session != nil && person != nil && gender != nil && start != nil && end != nil && offset != nil && confidence != nil {
			t.Match = &domain.TicketMatch{SessionID: *session, Person: int(*person), Gender: *gender, StayStart: *start, StayEnd: *end,
				OffsetS: domain.Round2(float64(*offset)), Confidence: domain.Round2(float64(*confidence))}
		}
		page.Tickets = append(page.Tickets, t)
		ids = append(ids, t.ID)
		return nil
	})
	if err != nil {
		return page, err
	}
	for i := range page.Tickets {
		index[page.Tickets[i].ID] = &page.Tickets[i]
	}
	if len(ids) == 0 {
		return page, nil
	}
	err = collect(ctx, r.db, `SELECT ticket_id::text, product, category, quantity, unit_price::float8, line_total::float8
		FROM sales_items WHERE ticket_id = ANY($1::text[]::uuid[]) ORDER BY item_id`, []any{ids}, func(row pgx.Rows) error {
		var id string
		var it domain.TicketItem
		if err := row.Scan(&id, &it.Product, &it.Category, &it.Quantity, &it.UnitPrice, &it.LineTotal); err != nil {
			return err
		}
		if t := index[id]; t != nil {
			t.Items = append(t.Items, it)
		}
		return nil
	})
	return page, err
}

func (r *SalesRepository) HourlySales(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.HourSales, error) {
	from, to := bounds(f)
	var out []domain.HourSales
	err := collect(ctx, r.db, `SELECT t.local_id, date_trunc('hour', t.sold_at), count(*)::int, COALESCE(sum(t.total), 0)::float8
		FROM sales_tickets t WHERE `+salesWhere+` GROUP BY 1, 2 ORDER BY 2, 1`,
		[]any{site.ID, from, to, f.LocalArray(), f.Origin()}, func(row pgx.Rows) error {
			var h domain.HourSales
			err := row.Scan(&h.LocalID, &h.Hour, &h.Tickets, &h.Revenue)
			out = append(out, h)
			return err
		})
	return out, err
}

func (r *SalesRepository) HourlyVisits(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.HourVisits, error) {
	from, to := bounds(f)
	origin := f.Origin()
	type key struct {
		local int
		hour  int64
	}
	sum := map[key]int{}
	add := func(row pgx.Rows) error {
		var v domain.HourVisits
		if err := row.Scan(&v.LocalID, &v.Hour, &v.Visitors); err != nil {
			return err
		}
		sum[key{v.LocalID, v.Hour.Unix()}] += v.Visitors
		return nil
	}
	if origin != domain.OriginSimulated {
		// People the model saw come into a local: each person counts once per local and hour.
		err := collect(ctx, r.db, `SELECT z.local_id, date_trunc('hour', e.start_time), count(DISTINCT e.global_id)::int
			FROM spatial_events e
			JOIN sessions s ON s.session_id = e.session_id
			JOIN zones z ON z.id = e.zone_id
			WHERE s.site_id = $1 AND e.event_type = 'ENTER' AND z.zone_type = 'INTERIOR' AND z.local_id IS NOT NULL
			  AND e.start_time >= $2 AND e.start_time < $3 AND ($4::int[] IS NULL OR z.local_id = ANY($4))
			GROUP BY 1, 2`, []any{site.ID, from, to, f.LocalArray()}, add)
		if err != nil {
			return nil, err
		}
	}
	err := collect(ctx, r.db, `SELECT local_id, hour, visitors FROM visit_counts
		WHERE site_id = $1 AND hour >= $2 AND hour < $3 AND ($4::int[] IS NULL OR local_id = ANY($4)) AND ($5 = '' OR origin = $5)`,
		[]any{site.ID, from, to, f.LocalArray(), origin}, add)
	if err != nil {
		return nil, err
	}
	out := make([]domain.HourVisits, 0, len(sum))
	for k, v := range sum {
		out = append(out, domain.HourVisits{LocalID: k.local, Hour: time.Unix(k.hour, 0).UTC(), Visitors: v})
	}
	return out, nil
}

func (r *SalesRepository) Products(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.ProductSales, error) {
	from, to := bounds(f)
	out := []domain.ProductSales{}
	err := collect(ctx, r.db, `SELECT i.product, COALESCE(max(i.category), ''), sum(i.quantity)::int, sum(i.line_total)::float8
		FROM sales_items i JOIN sales_tickets t ON t.ticket_id = i.ticket_id
		WHERE `+salesWhere+` GROUP BY i.product ORDER BY 4 DESC, 1 LIMIT 50`,
		[]any{site.ID, from, to, f.LocalArray(), f.Origin()}, func(row pgx.Rows) error {
			var p domain.ProductSales
			err := row.Scan(&p.Product, &p.Category, &p.Units, &p.Revenue)
			out = append(out, p)
			return err
		})
	return out, err
}

func (r *SalesRepository) Counts(ctx context.Context, site domain.Site) (domain.SalesCounts, error) {
	var c domain.SalesCounts
	err := r.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE origin = 'REAL')::int, count(*) FILTER (WHERE origin = 'SIMULADO')::int,
		(SELECT count(*)::int FROM sales_matches m JOIN sales_tickets x ON x.ticket_id = m.ticket_id WHERE x.site_id = $1)
		FROM sales_tickets WHERE site_id = $1`, site.ID).Scan(&c.Real, &c.Simulated, &c.Matched)
	return c, err
}

func (r *SalesRepository) MatchedByLocal(ctx context.Context, site domain.Site, f domain.SalesFilter) (map[int]int, error) {
	from, to := bounds(f)
	out := map[int]int{}
	err := collect(ctx, r.db, `SELECT t.local_id, count(*)::int FROM sales_matches m JOIN sales_tickets t ON t.ticket_id = m.ticket_id
		WHERE `+salesWhere+` GROUP BY 1`, []any{site.ID, from, to, f.LocalArray(), f.Origin()}, func(row pgx.Rows) error {
		var local, n int
		err := row.Scan(&local, &n)
		out[local] = n
		return err
	})
	return out, err
}

func (r *SalesRepository) StayEvents(ctx context.Context, session domain.Session) ([]domain.StayEvent, map[string]domain.PersonSpan, error) {
	var events []domain.StayEvent
	err := collect(ctx, r.db, `SELECT e.global_id::text, z.local_id, e.event_type, e.start_time, COALESCE(e.confidence, 1)::float8
		FROM spatial_events e JOIN zones z ON z.id = e.zone_id
		WHERE e.session_id = $1::text::uuid AND e.event_type IN ('ENTER', 'EXIT') AND z.zone_type = 'INTERIOR' AND z.local_id IS NOT NULL
		ORDER BY e.global_id, z.local_id, e.start_time, e.event_type`, []any{session.ID}, func(row pgx.Rows) error {
		var e domain.StayEvent
		err := row.Scan(&e.GlobalID, &e.LocalID, &e.Type, &e.At, &e.Confidence)
		events = append(events, e)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	people := map[string]domain.PersonSpan{}
	err = collect(ctx, r.db, `SELECT global_id::text, public_number, gender_estimate, first_seen, last_seen FROM identities WHERE session_id = $1::text::uuid`,
		[]any{session.ID}, func(row pgx.Rows) error {
			var id string
			var p domain.PersonSpan
			if err := row.Scan(&id, &p.Number, &p.Gender, &p.FirstSeen, &p.LastSeen); err != nil {
				return err
			}
			people[id] = p
			return nil
		})
	return events, people, err
}

func (r *SalesRepository) TicketsInWindow(ctx context.Context, site domain.Site, from, to time.Time) ([]domain.MatchTicket, error) {
	var out []domain.MatchTicket
	err := collect(ctx, r.db, `SELECT ticket_id::text, local_id, sold_at FROM sales_tickets
		WHERE site_id = $1 AND sold_at >= $2 AND sold_at <= $3 ORDER BY sold_at, ticket_id`, []any{site.ID, from, to}, func(row pgx.Rows) error {
		var t domain.MatchTicket
		err := row.Scan(&t.ID, &t.LocalID, &t.SoldAt)
		out = append(out, t)
		return err
	})
	return out, err
}

func (r *SalesRepository) ReplaceMatches(ctx context.Context, session domain.Session, matches []domain.Match) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "DELETE FROM sales_matches WHERE session_id = $1::text::uuid", session.ID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, m := range matches {
		// A ticket matched in another session of the same hours moves to this one.
		batch.Queue(`INSERT INTO sales_matches (ticket_id, session_id, global_id, stay_start, stay_end, offset_s, confidence)
			VALUES ($1::text::uuid, $2::text::uuid, $3::text::uuid, $4, $5, $6, $7)
			ON CONFLICT (ticket_id) DO UPDATE SET session_id = excluded.session_id, global_id = excluded.global_id, stay_start = excluded.stay_start,
			    stay_end = excluded.stay_end, offset_s = excluded.offset_s, confidence = excluded.confidence, matched_at = now()`,
			m.TicketID, session.ID, m.Stay.GlobalID, m.Stay.Start, m.Stay.End, float32(m.OffsetS), float32(m.Confidence))
	}
	results := tx.SendBatch(ctx, batch)
	for range matches {
		if _, err = results.Exec(); err != nil {
			results.Close()
			return rejected(err)
		}
	}
	if err = results.Close(); err != nil {
		return rejected(err)
	}
	params, _ := json.Marshal(domain.DefaultMatchParams)
	if _, err = tx.Exec(ctx, `INSERT INTO sales_match_runs (session_id, params) VALUES ($1::text::uuid, $2::text::jsonb)
		ON CONFLICT (session_id) DO UPDATE SET ran_at = now(), params = excluded.params`, session.ID, string(params)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SalesRepository) MatchedAt(ctx context.Context, session domain.Session) (*time.Time, error) {
	var at *time.Time
	err := r.db.QueryRow(ctx, "SELECT max(ran_at) FROM sales_match_runs WHERE session_id = $1::text::uuid", session.ID).Scan(&at)
	return at, err
}
