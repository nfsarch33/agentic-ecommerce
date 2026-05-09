package media

import (
	"context"
	"testing"
)

func BenchmarkStubVideoAssembler_Assemble(b *testing.B) {
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: NewStubVideoAssembler(),
		TenantID:  "tenant-1",
	})
	if err != nil {
		b.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	defer func() { _ = pipeline.Close(context.Background()) }()
	req := VideoAssemblyRequest{
		TenantID:        "tenant-1",
		ProductID:       "p",
		VoiceoverScript: "hello world",
		SubtitleLines:   []string{"00:00 hook", "00:05 demo"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.Assemble(context.Background(), req)
	}
}
