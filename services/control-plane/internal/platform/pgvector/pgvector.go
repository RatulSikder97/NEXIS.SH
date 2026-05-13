// Package pgvector is a thin wrapper over github.com/pgvector/pgvector-go that
// keeps the adapter layer free of direct vector-encoding noise. We use the
// Vector type for pgx parameter binding and column scanning; everything else
// stays pgx.
package pgvector

import (
	pgv "github.com/pgvector/pgvector-go"
)

// Vector is re-exported so adapter callers don't need to import pgvector-go
// directly outside this package.
type Vector = pgv.Vector

// FromSlice converts a []float32 into a pgvector.Vector for INSERT/UPDATE.
func FromSlice(v []float32) pgv.Vector { return pgv.NewVector(v) }

// ToSlice converts a pgvector.Vector back to []float32 after SELECT.
func ToSlice(v pgv.Vector) []float32 { return v.Slice() }
