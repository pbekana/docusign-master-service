// Package storage provides local filesystem and MinIO storage adapters.
package storage

import (
	"bytes"
	"fmt"

	"github.com/parquet-go/parquet-go"
)

// WriteParquet serialises records into a Parquet-encoded byte slice.
// T must be a concrete struct type with parquet struct tags. The function must
// be called with a concrete type at the call site — Go generics do not allow
// calling with an interface type.
//
// Example:
//
//	data, err := WriteParquet(normalizedEnvelopes)
func WriteParquet[T any](records []T) ([]byte, error) {
	if len(records) == 0 {
		// Return a valid empty Parquet file so consumers do not have to special-case nil.
		var buf bytes.Buffer
		if err := parquet.Write[T](&buf, nil); err != nil {
			return nil, fmt.Errorf("storage: writing empty parquet: %w", err)
		}
		return buf.Bytes(), nil
	}

	var buf bytes.Buffer
	if err := parquet.Write[T](&buf, records); err != nil {
		return nil, fmt.Errorf("storage: writing parquet: %w", err)
	}
	return buf.Bytes(), nil
}
