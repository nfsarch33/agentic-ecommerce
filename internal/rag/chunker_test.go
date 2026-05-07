package rag

import "testing"

func TestChunkDocumentSplitsWithOverlapAndMetadata(t *testing.T) {
	t.Parallel()

	doc := Document{
		ID:       "doc-1",
		TenantID: "tenant-a",
		Title:    "Resistance Bands",
		Source:   "supplier-spec",
		Content:  "Resistance bands use natural latex for progressive tension. The set includes five resistance levels. Each band is labelled for quick setup.",
		Metadata: map[string]string{"sku": "RB-SET"},
	}

	chunks, err := ChunkDocument(doc, ChunkOptions{MaxWords: 8, OverlapWords: 2})
	if err != nil {
		t.Fatalf("ChunkDocument: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d, want at least 3", len(chunks))
	}
	if chunks[0].DocumentID != doc.ID || chunks[0].TenantID != doc.TenantID || chunks[0].Source != doc.Source {
		t.Fatalf("chunk metadata = %+v", chunks[0])
	}
	if chunks[0].Index != 0 || chunks[1].Index != 1 {
		t.Fatalf("chunk indexes = %d/%d, want 0/1", chunks[0].Index, chunks[1].Index)
	}
	if chunks[0].Text == "" || chunks[1].Text == "" {
		t.Fatalf("chunk text should not be empty: %+v", chunks)
	}
	if chunks[0].Metadata["sku"] != "RB-SET" {
		t.Fatalf("chunk metadata map = %+v", chunks[0].Metadata)
	}
	if chunks[0].Metadata["sku"] = "mutated"; doc.Metadata["sku"] != "RB-SET" {
		t.Fatalf("chunk metadata should be copied, doc metadata = %+v", doc.Metadata)
	}
}

func TestChunkDocumentRejectsEmptyContent(t *testing.T) {
	t.Parallel()

	_, err := ChunkDocument(Document{ID: "doc-empty", Content: "   "}, ChunkOptions{})
	if err == nil {
		t.Fatal("expected empty content error")
	}
}
