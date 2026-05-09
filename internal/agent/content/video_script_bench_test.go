package content

import (
	"context"
	"testing"
)

func BenchmarkVideoScript_GenerateLLM(b *testing.B) {
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator:  newGoodTikTokGenerator(),
		TenantID:   "tenant-1",
		MinQuality: 0.6,
	})
	if err != nil {
		b.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	defer func() { _ = gen.Close(context.Background()) }()
	req := videoRequest(VideoPlatformTikTok)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate(context.Background(), req)
	}
}
