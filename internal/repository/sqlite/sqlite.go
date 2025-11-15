package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/repository"
)

type Repo struct {
	db     *sql.DB
	Rules  repository.RuleRepository
	Alarms repository.AlarmRepository
}

func New(db *sql.DB) *Repo {
	r := &Repo{db: db}
	r.Rules = &ruleRepo{db: db}
	r.Alarms = &alarmRepo{db: db}
	return r
}

// ----- Rules -----
type ruleRepo struct{ db *sql.DB }

func (r *ruleRepo) Create(ctx context.Context, rule *domain.Rule) error {
	now := time.Now().UTC()

	res, err := r.db.ExecContext(
		ctx,
		`INSERT INTO rules(name, signal, type, severity, latch, enabled, config, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		rule.Name,
		rule.Signal,
		rule.Type,
		rule.Severity,
		boolToInt(rule.Latch),
		boolToInt(rule.Enabled),
		string(rule.Config),
		now,
		now,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}

		return err
	}

	id, _ := res.LastInsertId()
	rule.ID = id
	rule.Version = 1
	rule.CreatedAt = now
	rule.UpdatedAt = now

	return nil
}

func (r *ruleRepo) GetByID(ctx context.Context, id int64) (*domain.Rule, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, name, signal, type, severity, latch, enabled, config, version, created_at, updated_at FROM rules WHERE id = ?`,
		id,
	)

	return scanRule(row)
}

func (r *ruleRepo) GetByName(ctx context.Context, name string) (*domain.Rule, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, name, signal, type, severity, latch, enabled, config, version, created_at, updated_at FROM rules WHERE name = ?`,
		name,
	)

	return scanRule(row)
}

func (r *ruleRepo) List(ctx context.Context) ([]domain.Rule, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, name, signal, type, severity, latch, enabled, config, version, created_at, updated_at FROM rules ORDER BY id ASC`,
	)

	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	var out []domain.Rule

	for rows.Next() {
		var rule domain.Rule
		var cfg string
		var created, updated string
		var latch, enabled int

		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Signal, &rule.Type, &rule.Severity, &latch, &enabled, &cfg, &rule.Version, &created, &updated); err != nil {
			return nil, err
		}

		rule.Latch = latch == 1
		rule.Enabled = enabled == 1
		rule.Config = []byte(cfg)
		rule.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		rule.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, rule)
	}

	return out, nil
}

func scanRule(row *sql.Row) (*domain.Rule, error) {
	var rule domain.Rule
	var cfg string
	var created, updated string
	var latch, enabled int

	if err := row.Scan(&rule.ID, &rule.Name, &rule.Signal, &rule.Type, &rule.Severity, &latch, &enabled, &cfg, &rule.Version, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}

		return nil, err
	}

	rule.Latch = latch == 1
	rule.Enabled = enabled == 1
	rule.Config = []byte(cfg)
	rule.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rule.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)

	return &rule, nil
}

// ----- Alarms -----
type alarmRepo struct{ db *sql.DB }

func (a *alarmRepo) Create(ctx context.Context, al *domain.Alarm) error {
	now := time.Now().UTC()

	res, err := a.db.ExecContext(
		ctx,
		`INSERT INTO alarms(rule_id, entity, state, first_seen, last_seen, acked_by, silence_until, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		al.RuleID,
		al.Entity,
		al.State,
		al.FirstSeen,
		al.LastSeen,
		al.AckedBy,
		al.SilenceUntil,
		now,
		now,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}

		return err
	}

	id, _ := res.LastInsertId()
	al.ID = id
	al.CreatedAt = now
	al.UpdatedAt = now

	return nil
}

func (a *alarmRepo) GetByID(ctx context.Context, id int64) (*domain.Alarm, error) {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT id, rule_id, entity, state, first_seen, last_seen, acked_by, silence_until, created_at, updated_at FROM alarms WHERE id = ?`,
		id,
	)

	return scanAlarmRow(row)
}

func (a *alarmRepo) GetByRuleEntity(ctx context.Context, ruleID int64, entity string) (*domain.Alarm, error) {
	row := a.db.QueryRowContext(
		ctx,
		`SELECT id, rule_id, entity, state, first_seen, last_seen, acked_by, silence_until, created_at, updated_at FROM alarms WHERE rule_id = ? AND entity = ?`,
		ruleID,
		entity,
	)

	return scanAlarmRow(row)
}

func scanAlarmRow(row *sql.Row) (*domain.Alarm, error) {
	var al domain.Alarm
	var firstSeen, lastSeen, silenceUntil sql.NullString
	var ackedBy sql.NullString

	if err := row.Scan(&al.ID, &al.RuleID, &al.Entity, &al.State, &firstSeen, &lastSeen, &ackedBy, &silenceUntil, &al.CreatedAt, &al.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}

		return nil, err
	}

	if firstSeen.Valid {
		t, _ := time.Parse(time.RFC3339Nano, firstSeen.String)
		al.FirstSeen = &t
	}

	if lastSeen.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastSeen.String)
		al.LastSeen = &t
	}

	if silenceUntil.Valid {
		t, _ := time.Parse(time.RFC3339Nano, silenceUntil.String)
		al.SilenceUntil = &t
	}

	if ackedBy.Valid {
		v := ackedBy.String
		al.AckedBy = &v
	}

	return &al, nil
}

func (a *alarmRepo) List(ctx context.Context, limit int) ([]domain.Alarm, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, rule_id, entity, state, first_seen, last_seen, acked_by, silence_until, created_at, updated_at FROM alarms ORDER BY updated_at DESC LIMIT ?`,
		limit,
	)

	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	var out []domain.Alarm

	for rows.Next() {
		var al domain.Alarm
		var firstSeen, lastSeen, silenceUntil sql.NullString
		var ackedBy sql.NullString

		if err := rows.Scan(&al.ID, &al.RuleID, &al.Entity, &al.State, &firstSeen, &lastSeen, &ackedBy, &silenceUntil, &al.CreatedAt, &al.UpdatedAt); err != nil {
			return nil, err
		}

		if firstSeen.Valid {
			t, _ := time.Parse(time.RFC3339Nano, firstSeen.String)
			al.FirstSeen = &t
		}

		if lastSeen.Valid {
			t, _ := time.Parse(time.RFC3339Nano, lastSeen.String)
			al.LastSeen = &t
		}

		if silenceUntil.Valid {
			t, _ := time.Parse(time.RFC3339Nano, silenceUntil.String)
			al.SilenceUntil = &t
		}

		if ackedBy.Valid {
			v := ackedBy.String
			al.AckedBy = &v
		}

		out = append(out, al)
	}

	return out, nil
}

func (a *alarmRepo) Update(ctx context.Context, al *domain.Alarm) error {
	_, err := a.db.ExecContext(
		ctx,
		`UPDATE alarms SET state = ?, first_seen = ?, last_seen = ?, acked_by = ?, silence_until = ?, updated_at = ? WHERE id = ?`,
		al.State, al.FirstSeen, al.LastSeen, al.AckedBy, al.SilenceUntil, al.UpdatedAt, al.ID,
	)

	return err
}

func (a *alarmRepo) CreateEvent(ctx context.Context, ev *domain.AlarmEvent) error {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}

	res, err := a.db.ExecContext(
		ctx,
		`INSERT INTO alarm_events(alarm_id, type, actor, at, note, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.AlarmID, ev.Type, ev.Actor, ev.At, ev.Note, string(ev.Payload),
	)

	if err != nil {
		return err
	}

	id, _ := res.LastInsertId()
	ev.ID = id

	return nil
}

func (a *alarmRepo) ListEvents(ctx context.Context, alarmID int64) ([]domain.AlarmEvent, error) {
	rows, err := a.db.QueryContext(
		ctx,
		`SELECT id, alarm_id, type, actor, at, note, payload FROM alarm_events WHERE alarm_id = ? ORDER BY at ASC`,
		alarmID,
	)

	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	var out []domain.AlarmEvent

	for rows.Next() {
		var ev domain.AlarmEvent
		var payload sql.NullString

		if err := rows.Scan(&ev.ID, &ev.AlarmID, &ev.Type, &ev.Actor, &ev.At, &ev.Note, &payload); err != nil {
			return nil, err
		}

		if payload.Valid {
			ev.Payload = []byte(payload.String)
		}

		out = append(out, ev)
	}

	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

func closeRows(rows *sql.Rows) {
	_ = rows.Close()
}

// Basic detection for SQLite UNIQUE errors
func isUniqueViolation(err error) bool {
	// modernc.org/sqlite returns errors with strings like "UNIQUE constraint failed"
	return err != nil && !errors.Is(err, sql.ErrNoRows) && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
