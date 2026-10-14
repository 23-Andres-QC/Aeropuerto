package services

import (
	"context"
	"log/slog"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// PrivacyService applies the retention decided for the platform: raw positions 90 days, finished logins and the audit log
// as long as they are useful. Aggregates and gender counts are kept. (Appearance vectors, 24 h, live in backend-vivo.)
type PrivacyService struct {
	repo     ports.PrivacyRepository
	accounts *AccountService
	now      func() time.Time
}

func NewPrivacyService(repo ports.PrivacyRepository, accounts *AccountService) *PrivacyService {
	return &PrivacyService{repo: repo, accounts: accounts, now: time.Now}
}

// SetClock replaces the clock (tests).
func (s *PrivacyService) SetClock(now func() time.Time) { s.now = now }

// PurgeResult is what one pass cleared.
type PurgeResult struct {
	Sessions        int       `json:"sesiones"`
	Points          int64     `json:"puntos"`
	LoginsEnded     int       `json:"sesiones_de_login"`
	AuditLines      int       `json:"lineas_de_bitacora"`
	PointsOlderThan time.Time `json:"puntos_anteriores_a"`
}

// Purge clears what passed its retention.
func (s *PrivacyService) Purge(ctx context.Context, actor domain.Principal, ip string) (PurgeResult, error) {
	now := s.now()
	res := PurgeResult{PointsOlderThan: now.AddDate(0, 0, -domain.RetentionPointsDays)}
	var err error
	if res.Sessions, res.Points, err = s.repo.PurgePoints(ctx, res.PointsOlderThan, now); err != nil {
		return res, err
	}
	if res.LoginsEnded, err = s.accounts.repo.PurgeSessions(ctx, now.Add(-24*time.Hour)); err != nil {
		return res, err
	}
	if res.AuditLines, err = s.accounts.repo.PurgeAudit(ctx, now.AddDate(0, 0, -domain.RetentionAuditDays)); err != nil {
		return res, err
	}
	if res.Sessions > 0 || res.Points > 0 {
		s.accounts.Audit(ctx, actor, ip, "retencion", "puntos crudos", "sesiones y puntos borrados por pasar de 90 días")
		slog.Info("retención aplicada", "sesiones", res.Sessions, "puntos", res.Points, "logins", res.LoginsEnded, "bitacora", res.AuditLines)
	}
	return res, nil
}

// Policy is the retention in force, with the state of the raw positions, for the privacy notice.
type Policy struct {
	VectorsHours int                `json:"vectores_horas"`
	PointsDays   int                `json:"puntos_dias"`
	AuditDays    int                `json:"bitacora_dias"`
	Aggregates   string             `json:"agregados"`
	State        domain.PointsState `json:"estado"`
}

func (s *PrivacyService) Policy(ctx context.Context) (Policy, error) {
	st, err := s.repo.PointsState(ctx)
	return Policy{VectorsHours: domain.RetentionVectorsHours, PointsDays: domain.RetentionPointsDays, AuditDays: domain.RetentionAuditDays,
		Aggregates: "sin límite", State: st}, err
}

// Run purges once a day (the first pass a minute after starting) until ctx ends.
func (s *PrivacyService) Run(ctx context.Context) {
	system := domain.Principal{User: "sistema"}
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Minute)
			if _, err := s.Purge(c, system, ""); err != nil {
				slog.Error("retención: no se pudo aplicar", "error", err)
			}
			cancel()
			timer.Reset(24 * time.Hour)
		}
	}
}
