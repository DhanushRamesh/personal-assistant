// Package mysql : The knowledge store, in MySQL.
package mysql

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"

	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/domain/knowledge"
)

// MaxScanned : The most chunks compared against a question. Enough for
// years of what is known; past it, the newest are compared.
const MaxScanned = 20000

type row struct {
	UserID     string    `gorm:"column:user_id;primaryKey"`
	Source     string    `gorm:"column:source;primaryKey"`
	Ref        string    `gorm:"column:ref;primaryKey"`
	Chunk      int       `gorm:"column:chunk;primaryKey"`
	Text       string    `gorm:"column:text"`
	Embedding  []byte    `gorm:"column:embedding"`
	EmbedModel string    `gorm:"column:embed_model;primaryKey"`
	Hash       string    `gorm:"column:hash"`
	At         time.Time `gorm:"column:at"`
}

func (row) TableName() string { return "knowledge" }

// Store : What is known, in MySQL.
type Store struct{ db *gorm.DB }

// New : The store over the database.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Hash : See knowledge.Store.
func (s *Store) Hash(ctx context.Context, userID string, source knowledge.Source, ref, model string) (string, error) {
	var r row
	err := s.db.WithContext(ctx).Where("user_id = ? AND source = ? AND ref = ? AND embed_model = ?", userID, string(source), ref, model).
		Limit(1).Find(&r).Error
	if err != nil {
		return "", fmt.Errorf("knowledge: reading %s %s: %w", source, ref, err)
	}
	return r.Hash, nil
}

// Put : See knowledge.Store.
func (s *Store) Put(ctx context.Context, userID string, source knowledge.Source, ref, model, hash string, at time.Time, chunks []string, vectors []embed.Vector) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("knowledge: %d chunks and %d vectors", len(chunks), len(vectors))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND source = ? AND ref = ?", userID, string(source), ref).Delete(&row{}).Error; err != nil {
			return fmt.Errorf("knowledge: replacing %s %s: %w", source, ref, err)
		}
		rows := make([]row, len(chunks))
		for i := range chunks {
			rows[i] = row{UserID: userID, Source: string(source), Ref: ref, Chunk: i, Text: chunks[i],
				Embedding: blob(vectors[i]), EmbedModel: model, Hash: hash, At: at}
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return fmt.Errorf("knowledge: keeping %s %s: %w", source, ref, err)
		}
		return nil
	})
}

// Remove : See knowledge.Store.
func (s *Store) Remove(ctx context.Context, userID string, source knowledge.Source, ref string) error {
	if err := s.db.WithContext(ctx).Where("user_id = ? AND source = ? AND ref = ?", userID, string(source), ref).Delete(&row{}).Error; err != nil {
		return fmt.Errorf("knowledge: forgetting %s %s: %w", source, ref, err)
	}
	return nil
}

// Refs : See knowledge.Store.
func (s *Store) Refs(ctx context.Context, userID string, source knowledge.Source) ([]string, error) {
	var refs []string
	if err := s.db.WithContext(ctx).Model(&row{}).Where("user_id = ? AND source = ?", userID, string(source)).
		Distinct("ref").Pluck("ref", &refs).Error; err != nil {
		return nil, fmt.Errorf("knowledge: listing %s: %w", source, err)
	}
	return refs, nil
}

// NearestTo : See knowledge.Store.
func (s *Store) NearestTo(ctx context.Context, userID string, q embed.Vector, model string, limit int) ([]knowledge.Found, error) {
	if limit <= 0 || len(q) == 0 {
		return nil, nil
	}
	var rows []row
	if err := s.db.WithContext(ctx).Where("user_id = ? AND embed_model = ?", userID, model).
		Order("at DESC").Limit(MaxScanned).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("knowledge: reading: %w", err)
	}
	out := make([]knowledge.Found, 0, len(rows))
	for i := range rows {
		out = append(out, knowledge.Found{
			Piece: knowledge.Piece{Source: knowledge.Source(rows[i].Source), Ref: rows[i].Ref, Chunk: rows[i].Chunk, Text: rows[i].Text, At: rows[i].At},
			Score: embed.Similarity(q, vector(rows[i].Embedding)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func blob(v embed.Vector) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func vector(b []byte) embed.Vector {
	if len(b)%4 != 0 {
		return nil
	}
	out := make(embed.Vector, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

var _ knowledge.Store = (*Store)(nil)
