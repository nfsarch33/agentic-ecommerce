package csvexport_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/csvexport"
)

// sliceSource returns a DataSource that emits a fixed set of rows.
func sliceSource(rows []csvexport.Row) csvexport.DataSource {
	return func(_ context.Context, ch chan<- csvexport.Row) error {
		defer close(ch)
		for _, r := range rows {
			ch <- r
		}
		return nil
	}
}

// --- StreamCSV ---

func TestStreamCSV_WithHeaders(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rows := []csvexport.Row{{"Alice", "alice@example.com"}, {"Bob", "bob@example.com"}}
	cfg := csvexport.WriterConfig{Headers: []string{"name", "email"}, ChunkSize: 10}

	n, err := csvexport.StreamCSV(context.Background(), &buf, cfg, sliceSource(rows))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 rows, got %d", n)
	}
	r := csv.NewReader(strings.NewReader(buf.String()))
	records, _ := r.ReadAll()
	if len(records) != 3 { // header + 2 data rows
		t.Fatalf("want 3 records (header+2), got %d", len(records))
	}
	if records[0][0] != "name" {
		t.Fatalf("header row should start with 'name', got %q", records[0][0])
	}
}

func TestStreamCSV_NoHeaders(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rows := []csvexport.Row{{"1", "order_created"}, {"2", "order_shipped"}}
	cfg := csvexport.WriterConfig{}

	n, err := csvexport.StreamCSV(context.Background(), &buf, cfg, sliceSource(rows))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 rows, got %d", n)
	}
}

func TestStreamCSV_EmptySource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n, err := csvexport.StreamCSV(context.Background(), &buf, csvexport.WriterConfig{Headers: []string{"id"}}, sliceSource(nil))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0, got %d", n)
	}
}

func TestStreamCSV_SourceError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	errSrc := func(_ context.Context, ch chan<- csvexport.Row) error {
		close(ch)
		return errors.New("db connection lost")
	}
	_, err := csvexport.StreamCSV(context.Background(), &buf, csvexport.WriterConfig{}, errSrc)
	if err == nil {
		t.Fatal("expected error from source")
	}
}

// --- JobRegistry ---

func TestJobRegistry_Submit_Done(t *testing.T) {
	t.Parallel()
	reg := csvexport.NewJobRegistry()
	rows := make([]csvexport.Row, 50)
	for i := range rows {
		rows[i] = csvexport.Row{"id", "value"}
	}
	var buf bytes.Buffer
	id := reg.Submit(context.Background(), csvexport.WriterConfig{}, sliceSource(rows), &buf)
	if id == "" {
		t.Fatal("expected job ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	job, err := reg.WaitFor(ctx, id, 1*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != csvexport.StatusDone {
		t.Fatalf("want done, got %q", job.Status)
	}
	if job.RowCount != 50 {
		t.Fatalf("want 50, got %d", job.RowCount)
	}
}

func TestJobRegistry_Status_NotFound(t *testing.T) {
	t.Parallel()
	reg := csvexport.NewJobRegistry()
	_, err := reg.Status("nonexistent")
	if !errors.Is(err, csvexport.ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

func TestJobRegistry_WaitFor_ContextCancel(t *testing.T) {
	t.Parallel()
	reg := csvexport.NewJobRegistry()
	// Source that never finishes.
	blocker := func(ctx context.Context, ch chan<- csvexport.Row) error {
		<-ctx.Done()
		close(ch)
		return ctx.Err()
	}
	var buf bytes.Buffer
	id := reg.Submit(context.Background(), csvexport.WriterConfig{}, blocker, &buf)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := reg.WaitFor(ctx, id, 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected context timeout error")
	}
}
