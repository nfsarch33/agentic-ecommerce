// Command variantgen turns source items into platform variants
// (v18870-3-variant-generator): one source, six shapes (facebook, instagram
// carousel, x, tiktok script, youtube short, linkedin), drafted by the
// tier-0 alias through the router and grounded by CODE before the ledger
// row is written. The tone judge is a separate flag-driven leg so the eval
// table (grounding + tone) reads from the same ledger.
//
// Input:  NDJSON sources {"id","title","body","facts":[...]}.
// Output: NDJSON ledger rows, one per source x platform:
//
//	{"source_id","platform","text","slides","violations","format",
//	 "status","tokens_in","tokens_out","secs","ts"}
//
// Status: drafted (no violations) | grounding_fail | format_fail | error.
//
// The router client is the enrich-batch shape (OpenAI-compatible chat on
// 127.0.0.1:8787, X-Helixon-Agent header, bearer from env).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/contentgen"
)

type routerClient struct {
	base  string
	key   string
	agent string
	http  *http.Client
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *routerClient) chat(ctx context.Context, model, system, user string) (string, int64, int64, error) {
	body, _ := json.Marshal(chatRequest{Model: model, Messages: []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	req.Header.Set("X-Helixon-Agent", c.agent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", 0, 0, fmt.Errorf("router %s: %.200s", resp.Status, raw)
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", 0, 0, fmt.Errorf("router decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("router returned no choices")
	}
	return cr.Choices[0].Message.Content, cr.Usage.PromptTokens, cr.Usage.CompletionTokens, nil
}

type ledgerRow struct {
	SourceID   string   `json:"source_id"`
	Platform   string   `json:"platform"`
	Text       string   `json:"text"`
	Slides     []string `json:"slides,omitempty"`
	Violations []string `json:"violations,omitempty"`
	Format     []string `json:"format,omitempty"`
	Status     string   `json:"status"`
	TokensIn   int64    `json:"tokens_in"`
	TokensOut  int64    `json:"tokens_out"`
	Secs       float64  `json:"secs"`
	Ts         string   `json:"ts"`
	ToneScore  int      `json:"tone_score,omitempty"`
	ToneNote   string   `json:"tone_note,omitempty"`
}

const systemPrompt = `You write social variants for an Australian online shop.
Hard rules: use ONLY facts that appear in the SOURCE block. Never invent a
number, price, size, material or product property. Quote figures exactly as
the source states them. No emojis unless the platform guidance says so.
AU spelling. Plain active voice.`

func userPrompt(p contentgen.Platform, s contentgen.Source) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PLATFORM: %s\nGUIDANCE: %s\n\nSOURCE:\nTitle: %s\n", p.Name, p.Guidance, s.Title)
	if s.Body != "" {
		fmt.Fprintf(&b, "%s\n", s.Body)
	}
	for _, f := range s.Facts {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	if p.MaxSlides > 0 {
		fmt.Fprintf(&b, "\nOUTPUT: exactly one line per slide, numbered '1| text' up to %d slides.\n", p.MaxSlides)
	} else {
		b.WriteString("\nOUTPUT: the post text only, no preamble.\n")
	}
	return b.String()
}

func main() {
	base := flag.String("router", "http://127.0.0.1:8787", "llm-router base URL")
	model := flag.String("model", "qwen3.8-27b-local", "draft model (tier-0 alias)")
	agent := flag.String("agent", "variantgen", "X-Helixon-Agent value")
	limit := flag.Int("limit", 0, "stop after N sources (0 = all)")
	timeoutPer := flag.Duration("timeout", 120*time.Second, "per-request timeout")
	judgeModel := flag.String("judge-model", "", "tone judge model (empty = skip judging; e.g. MiniMax-M3)")
	flag.Parse()
	inPath := flag.Arg(0)
	outPath := flag.Arg(1)
	if inPath == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: variantgen [-flags] <sources.ndjson> <ledger.ndjson>")
		os.Exit(2)
	}
	in, err := os.Open(inPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "variantgen:", err)
		os.Exit(2)
	}
	defer in.Close()
	out, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "variantgen:", err)
		os.Exit(2)
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()

	rc := &routerClient{base: *base, key: os.Getenv("LLM_ROUTER_TOKEN"), agent: *agent, http: &http.Client{}}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if *limit > 0 && n >= *limit {
			break
		}
		var s contentgen.Source
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			fmt.Fprintf(os.Stderr, "variantgen: bad source line: %v\n", err)
			continue
		}
		n++
		for _, p := range contentgen.Platforms {
			ctx, cancel := context.WithTimeout(context.Background(), *timeoutPer)
			start := time.Now()
			raw, tin, tout, err := rc.chat(ctx, *model, systemPrompt, userPrompt(p, s))
			cancel()
			row := ledgerRow{SourceID: s.ID, Platform: p.Name, TokensIn: tin, TokensOut: tout, Ts: time.Now().UTC().Format(time.RFC3339)}
			row.Secs = time.Since(start).Seconds()
			if err != nil {
				row.Status = "error"
				row.Violations = []string{err.Error()}
			} else {
				v := contentgen.Variant{SourceID: s.ID, Platform: p.Name, Text: strings.TrimSpace(raw)}
				if p.MaxSlides > 0 {
					v.Slides = contentgen.ParseSlides(raw)
					v.Text = ""
				}
				row.Text, row.Slides = v.Text, v.Slides
				row.Violations = contentgen.CheckGrounding(v, s)
				row.Format = contentgen.CheckFormat(v, p)
				switch {
				case len(row.Violations) > 0:
					row.Status = "grounding_fail"
				case len(row.Format) > 0:
					row.Status = "format_fail"
				default:
					row.Status = "drafted"
				}
				if *judgeModel != "" && row.Status == "drafted" {
					jctx, jcancel := context.WithTimeout(context.Background(), *timeoutPer)
					score, _, _, jerr := rc.chat(jctx, *judgeModel, judgeSystem, judgeUser(p, v))
					jcancel()
					if jerr != nil {
						row.ToneNote = "judge error: " + jerr.Error()
					} else if n, ok := parseScore(score); ok {
						row.ToneScore = n
						row.ToneNote = firstLine(scoreLine(score))
					} else {
						row.ToneNote = "judge unparseable: " + firstLine(scoreLine(score))
					}
				}
			}
			b, _ := json.Marshal(row)
			fmt.Fprintln(w, string(b))
			if err := w.Flush(); err != nil {
				fmt.Fprintln(os.Stderr, "variantgen: flush:", err)
				os.Exit(2)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "variantgen: %d source(s) x %d platforms -> %s\n", n, len(contentgen.Platforms), outPath)
}

const judgeSystem = `You are a strict social-media tone judge for an Australian shop.
Score the POST for its platform: 5 excellent fit and tone, 4 good, 3 passable,
2 off-tone, 1 wrong for the platform. Judge only tone, clarity and platform
fit — factual grounding is checked by code elsewhere. Reply with one line:
SCORE: <1-5> then one short reason.`

func judgeUser(p contentgen.Platform, v contentgen.Variant) string {
	body := v.Text
	if len(v.Slides) > 0 {
		body = strings.Join(v.Slides, " / ")
	}
	return "PLATFORM: " + p.Name + "\nPOST:\n" + body
}

// parseScore reads the judge reply. The MiniMax judge reasons inside a
// <think> block first; the verdict is the SCORE: line after it — the note
// must carry that line's reason, never the reasoning preamble.
func parseScore(s string) (int, bool) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "SCORE:") {
			for _, f := range strings.Fields(line) {
				if len(f) == 1 && f[0] >= '1' && f[0] <= '5' {
					return int(f[0] - '0'), true
				}
			}
		}
	}
	return 0, false
}

func scoreLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "SCORE:") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
