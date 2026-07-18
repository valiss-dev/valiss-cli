package store

import (
	"context"
	"fmt"
	"time"

	"liteorm.org"
	"liteorm.org/orm"
)

// TemplateRecord is one generation of a claim template. It carries claim
// material only (extension grants, TTL, the bearer flag, a description), never
// identity claims. HTTP, GRPC, and Ext are JSON-encoded string lists of the raw
// grant-flag values; the chain layer marshals and unmarshals them.
type TemplateRecord struct {
	Name        string
	Generation  uint64
	HTTP        string
	GRPC        string
	Ext         string
	TTLSeconds  int64
	Bearer      bool
	Description string
	Salt        string
	ContentHash string
	CreatedAt   time.Time
	Retired     bool
	RetiredAt   time.Time
}

// PutTemplate appends a template generation row (append-only, like entities).
func (l *Local) PutTemplate(r TemplateRecord) error {
	row := templateRow{
		Name:        r.Name,
		Generation:  r.Generation,
		HTTP:        r.HTTP,
		GRPC:        r.GRPC,
		Ext:         r.Ext,
		TTLSeconds:  r.TTLSeconds,
		Bearer:      r.Bearer,
		Description: r.Description,
		Salt:        r.Salt,
		ContentHash: r.ContentHash,
		CreatedAt:   nonZeroTime(r.CreatedAt),
		Retired:     r.Retired,
		RetiredAt:   r.RetiredAt,
	}
	if err := orm.NewRepo[templateRow](l.db).Create(context.Background(), &row); err != nil {
		return fmt.Errorf("valiss: writing template %q: %w", r.Name, err)
	}
	return nil
}

// LatestTemplate returns the highest-generation row for a template name, or
// ErrNoTemplate when the name has none.
func (l *Local) LatestTemplate(name string) (TemplateRecord, error) {
	row, err := orm.NewRepo[templateRow](l.db).
		Where("name = ?", name).
		OrderBy("generation DESC").
		First(context.Background())
	if liteorm.IsNotFound(err) {
		return TemplateRecord{}, fmt.Errorf("%w: %s", ErrNoTemplate, name)
	}
	if err != nil {
		return TemplateRecord{}, fmt.Errorf("valiss: reading template %q: %w", name, err)
	}
	return templateRecordOf(row), nil
}

// TemplateAt returns a specific generation of a template, or ErrNoTemplate.
func (l *Local) TemplateAt(name string, generation uint64) (TemplateRecord, error) {
	row, err := orm.NewRepo[templateRow](l.db).
		Where("name = ? AND generation = ?", name, generation).
		First(context.Background())
	if liteorm.IsNotFound(err) {
		return TemplateRecord{}, fmt.Errorf("%w: %s@%d", ErrNoTemplate, name, generation)
	}
	if err != nil {
		return TemplateRecord{}, fmt.Errorf("valiss: reading template %q@%d: %w", name, generation, err)
	}
	return templateRecordOf(row), nil
}

// ListTemplates returns the latest generation of every template name, ordered
// by name.
func (l *Local) ListTemplates() ([]TemplateRecord, error) {
	rows, err := orm.NewRepo[templateRow](l.db).
		OrderBy("name ASC, generation DESC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: listing templates: %w", err)
	}
	var out []TemplateRecord
	seen := make(map[string]bool)
	for _, row := range rows {
		if seen[row.Name] {
			continue
		}
		seen[row.Name] = true
		out = append(out, templateRecordOf(row))
	}
	return out, nil
}

// TemplateGenerations returns every generation of a template name, newest
// first, for the template audit.
func (l *Local) TemplateGenerations(name string) ([]TemplateRecord, error) {
	rows, err := orm.NewRepo[templateRow](l.db).
		Where("name = ?", name).
		OrderBy("generation DESC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: reading template generations for %q: %w", name, err)
	}
	out := make([]TemplateRecord, len(rows))
	for i, row := range rows {
		out[i] = templateRecordOf(row)
	}
	return out, nil
}

// RetireTemplate marks every generation of a template name retired, so the
// name accepts no new mints. Generations are retained (reference-retained GC
// is not yet implemented; see the template verb family).
func (l *Local) RetireTemplate(name string) error {
	if _, err := l.LatestTemplate(name); err != nil {
		return err
	}
	if _, err := l.db.ExecContext(context.Background(),
		"UPDATE templates SET retired = 1, retired_at = ? WHERE name = ? AND retired = 0",
		time.Now().UTC(), name); err != nil {
		return fmt.Errorf("valiss: retiring template %q: %w", name, err)
	}
	return nil
}

// templateRecordOf converts a persisted row to a TemplateRecord.
func templateRecordOf(row templateRow) TemplateRecord {
	return TemplateRecord{
		Name:        row.Name,
		Generation:  row.Generation,
		HTTP:        row.HTTP,
		GRPC:        row.GRPC,
		Ext:         row.Ext,
		TTLSeconds:  row.TTLSeconds,
		Bearer:      row.Bearer,
		Description: row.Description,
		Salt:        row.Salt,
		ContentHash: row.ContentHash,
		CreatedAt:   row.CreatedAt,
		Retired:     row.Retired,
		RetiredAt:   row.RetiredAt,
	}
}
