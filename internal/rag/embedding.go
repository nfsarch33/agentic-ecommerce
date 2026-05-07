package rag

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

type HashEmbedder struct {
	dimensions int
}

func NewHashEmbedder(dimensions int) *HashEmbedder {
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}
	return &HashEmbedder{dimensions: dimensions}
}

func (e *HashEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = hashEmbedding(text, e.dimensions)
	}
	return out, nil
}

func hashEmbedding(text string, dimensions int) []float64 {
	vec := make([]float64, dimensions)
	for _, token := range chunkWordRe.FindAllString(strings.ToLower(text), -1) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		sum := h.Sum64()
		idx := int(sum % uint64(dimensions))
		sign := 1.0
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], sum)
		if buf[0]&1 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}
	normalize(vec)
	return vec
}

func normalize(vec []float64) {
	var norm float64
	for _, value := range vec {
		norm += value * value
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] = vec[i] / norm
	}
}

func validateDimensions(vec []float64, dimensions int) error {
	if dimensions <= 0 {
		return nil
	}
	if len(vec) != dimensions {
		return fmt.Errorf("%w: got %d want %d", ErrInvalidDimensions, len(vec), dimensions)
	}
	return nil
}
