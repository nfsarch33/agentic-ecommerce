package rag

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var chunkWordRe = regexp.MustCompile(`[A-Za-z0-9]+(?:['-][A-Za-z0-9]+)?`)

type ChunkOptions struct {
	MaxWords     int
	OverlapWords int
}

func ChunkDocument(doc Document, opts ChunkOptions) ([]Chunk, error) {
	text := strings.TrimSpace(doc.Content)
	if text == "" {
		return nil, ErrEmptyDocument
	}
	if opts.MaxWords <= 0 {
		opts.MaxWords = 180
	}
	if opts.OverlapWords < 0 {
		opts.OverlapWords = 0
	}
	if opts.OverlapWords >= opts.MaxWords {
		opts.OverlapWords = opts.MaxWords / 4
	}

	words := chunkWordRe.FindAllString(text, -1)
	if len(words) == 0 {
		return nil, ErrEmptyDocument
	}
	step := opts.MaxWords - opts.OverlapWords
	if step <= 0 {
		step = opts.MaxWords
	}
	createdAt := doc.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	chunks := make([]Chunk, 0, (len(words)/step)+1)
	for start, index := 0, 0; start < len(words); start, index = start+step, index+1 {
		end := start + opts.MaxWords
		if end > len(words) {
			end = len(words)
		}
		chunkID := fmt.Sprintf("%s:%04d", doc.ID, index)
		if strings.TrimSpace(doc.ID) == "" {
			chunkID = fmt.Sprintf("chunk:%04d", index)
		}
		chunks = append(chunks, Chunk{
			ID:         chunkID,
			DocumentID: doc.ID,
			TenantID:   doc.TenantID,
			Index:      index,
			Title:      doc.Title,
			Source:     doc.Source,
			Text:       strings.Join(words[start:end], " "),
			Metadata:   copyMetadata(doc.Metadata),
			CreatedAt:  createdAt,
		})
		if end == len(words) {
			break
		}
	}
	return chunks, nil
}

func copyMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
