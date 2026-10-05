// enrich-batch drives the content-enrichment fixture loop: read products
// from the fixture store (wc/v3), draft four fields on the cheap tier-0
// model through the router, run the deterministic grounding check, judge
// with MiniMax, and write one ledger row per draft. Publishing is a
// separate, gated step: --publish refuses to run without an approved
// decision AND the audited store proxy — this command never writes to the
// store on its own.
package main

import (
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/enrichment"
	"github.com/nfsarch33/agentic-ecommerce/internal/costledger"
)

const (
	draftSystem = `You write product copy for a storefront. You may cite ONLY the product attributes given to you — never invent a colour, material, size, measure, property or price the product does not have. Reply with ONE JSON object, no prose, no code fences: {"description": string (40-80 words), "seo_title": string (<=60 chars), "meta_description": string (<=155 chars), "alt_text": string (10-125 chars, describes the photo using only given attributes)}.`
	judgeSystem = `You judge a product draft against the product's attributes. Reply with ONE JSON object: {"acceptable": true|false, "reason": string}. false if the draft is unreadable, off-brand, or names any attribute the product does not have.`
)

type draftPayload struct {
	Description     string `json:"description"`
	SEOTitle        string `json:"seo_title"`
	MetaDescription string `json:"meta_description"`
	AltText         string `json:"alt_text"`
}

type judgePayload struct {
	Acceptable bool   `json:"acceptable"`
	Reason     string `json:"reason"`
}

type reportRow struct {
	Time       string       `json:"ts"`
	SKU        string       `json:"sku"`
	Status     string       `json:"status"` // drafted | grounding_fail | judge_reject | error
	Violations []string     `json:"violations,omitempty"`
	Judge      string       `json:"judge,omitempty"`
	TokensIn   int64        `json:"tokens_in"`
	TokensOut  int64        `json:"tokens_out"`
	Secs       float64      `json:"secs"`
	Draft      draftPayload `json:"draft,omitempty"`
}

// routerClient is the minimal OpenAI-compatible chat client the batch
// needs: one endpoint, the fleet bearer, the agent header.
type routerClient struct {
	base   string
	bearer string
	agent  string
	hc     *http.Client
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
	body, _ := json.Marshal(map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"temperature": 0.4,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("X-Helixon-Agent", c.agent)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", 0, 0, fmt.Errorf("router %s: %s", resp.Status, truncate(raw, 200))
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

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// parseJSONObject strips code fences and picks the first {...} block.
func parseJSONObject(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexAny(s, "\n"); j >= 0 {
			s = s[j+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("no JSON object in reply: %s", truncate([]byte(s), 120))
	}
	return []byte(s[i : j+1]), nil
}

func factsFrom(p woocommerce.Product) enrichment.Facts {
	f := enrichment.Facts{
		SKU:         p.SKU,
		Name:        p.Name,
		Price:       p.Price,
		AttrOptions: map[string][]string{},
	}
	for _, c := range p.Categories {
		if c.Name != "" {
			f.Categories = append(f.Categories, c.Name)
		}
	}
	for _, t := range p.Tags {
		if t.Name != "" {
			f.Tags = append(f.Tags, t.Name)
		}
	}
	for _, a := range p.Attributes {
		f.AttrOptions[a.Name] = a.Options
	}
	return f
}

// publishGate refuses the publish mode unless the audited proxy is named:
// the store is written only through the counting proxy, only with an
// approved decision from the gate. The batch's default mode drafts and
// never writes.
func publishGate(publish bool, proxy string) error {
	if !publish {
		return nil
	}
	if proxy == "" {
		return fmt.Errorf("--publish requires --store-proxy (or ENRICH_STORE_PROXY): the store is written only through the audited proxy, only with an approved decision — this batch drafts")
	}
	return nil
}

func main() {
	var (
		router     = flag.String("router", "http://127.0.0.1:8787/v1", "router base URL")
		draftModel = flag.String("draft-model", "qwen3.8-27b-local", "tier-0 draft model")
		judgeModel = flag.String("judge-model", "MiniMax-M3", "judge model")
		wooBase    = flag.String("woo-base", os.Getenv("WOO_BASE_URL"), "fixture store base URL")
		count      = flag.Int("count", 50, "minimum drafts to produce")
		out        = flag.String("out", "", "report ndjson path (default enrich-batch-<ts>.ndjson in cwd)")
		publish    = flag.Bool("publish", false, "publish approved drafts (requires --store-proxy AND approved decisions; refuses otherwise)")
		proxy      = flag.String("store-proxy", os.Getenv("ENRICH_STORE_PROXY"), "audited store proxy URL for --publish")
		agentFlag  = flag.String("agent", "enrich-batch", "agent name for the router header (or ENRICH_AGENT)")
	)
	flag.Parse()

	bearer := os.Getenv("LLM_ROUTER_TOKEN")
	if bearer == "" {
		fatal("LLM_ROUTER_TOKEN not set")
	}
	if err := publishGate(*publish, *proxy); err != nil {
		fatal("%v", err)
	}
	if *out == "" {
		*out = fmt.Sprintf("enrich-batch-%s.ndjson", time.Now().UTC().Format("20060102T150405Z"))
	}

	// The router's per-agent header routes fair-share queues; the caller
	// names itself (ENRICH_AGENT env or --agent flag), defaulting to this
	// tool's own name — never a fleet agent id.
	agent := os.Getenv("ENRICH_AGENT")
	if agent == "" {
		agent = *agentFlag
	}
	rc := &routerClient{base: *router, bearer: bearer, agent: agent, hc: &http.Client{Timeout: 120 * time.Second}}
	wc := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        *wooBase,
		ConsumerKey:    os.Getenv("WOO_CONSUMER_KEY"),
		ConsumerSecret: os.Getenv("WOO_CONSUMER_SECRET"),
	}, rc.hc)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	dsn := os.Getenv("ECOMMERCE_DB_URL")
	if dsn == "" {
		fatal("ECOMMERCE_DB_URL not set: every draft needs its ledger row (fail closed)")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatal("ledger pool: %v", err)
	}
	defer pool.Close()
	ledger := costledger.NewPGRecorder(pool)

	// Page until a short page: one 100-slot request silently worked only
	// the newest half of the catalogue (the r25 run's older half sat on
	// page 2), and a partial batch reads as a full pass on the report.
	var products []woocommerce.Product
	for page := 1; page <= 20; page++ {
		batch, err := wc.ListProducts(ctx, woocommerce.ListOptions{PerPage: 100, Page: page})
		if err != nil {
			fatal("list products page %d: %v", page, err)
		}
		products = append(products, batch...)
		if len(batch) < 100 {
			break
		}
	}
	var eligible []woocommerce.Product
	for _, p := range products {
		if strings.HasPrefix(p.SKU, "ENR-") {
			eligible = append(eligible, p)
		}
	}
	fmt.Fprintf(os.Stderr, "products: %d total, %d ENR- eligible\n", len(products), len(eligible))

	rep, err := os.Create(*out)
	if err != nil {
		fatal("report: %v", err)
	}
	defer rep.Close()

	var drafted int
	for _, p := range eligible {
		if drafted >= *count {
			break
		}
		row := runOne(ctx, rc, p, *draftModel, *judgeModel)
		b, _ := json.Marshal(row)
		fmt.Fprintln(rep, string(b))
		if row.Status == "drafted" || row.Status == "judge_reject" {
			drafted++
		}
		fmt.Fprintf(os.Stderr, "%s %s violations=%d judge=%s secs=%.1f\n", row.SKU, row.Status, len(row.Violations), row.Judge, row.Secs)
		recordLedger(ctx, ledger, *row, *draftModel)
	}
	fmt.Fprintf(os.Stderr, "done: %d drafts reported to %s\n", drafted, *out)
	if drafted < *count {
		fatal("only %d drafts (want %d)", drafted, *count)
	}
}

// recordLedger writes the row every draft owes the ledger — success or
// failure, tokens in and out, the violations in the error text. A draft
// that cannot be counted is a batch-stopping bug, not a skip.
func recordLedger(ctx context.Context, rec costledger.Recorder, row reportRow, model string) {
	status := "ok"
	if row.Status != "drafted" {
		status = "error"
	}
	if err := rec.Record(ctx, costledger.Row{
		JobID:     "enrich-" + row.SKU,
		TenantID:  "fixture",
		Action:    "content_draft",
		Model:     model,
		TokensIn:  row.TokensIn,
		TokensOut: row.TokensOut,
		CostCents: 0,
		Status:    status,
		ErrorText: strings.Join(row.Violations, "; "),
	}); err != nil {
		fatal("ledger row for %s: %v", row.SKU, err)
	}
}

func runOne(ctx context.Context, rc *routerClient, p woocommerce.Product, draftModel, judgeModel string) *reportRow {
	start := time.Now()
	facts := factsFrom(p)

	user, _ := json.Marshal(map[string]any{
		"name": p.Name, "sku": p.SKU, "price": p.Price,
		"categories": facts.Categories, "tags": facts.Tags, "attributes": facts.AttrOptions,
	})
	content, tin, tout, err := rc.chat(ctx, draftModel, draftSystem, string(user))
	row := &reportRow{Time: start.UTC().Format(time.RFC3339), SKU: p.SKU, TokensIn: tin, TokensOut: tout}
	if err != nil {
		row.Status, row.Secs = "error", time.Since(start).Seconds()
		return row
	}
	obj, err := parseJSONObject(content)
	if err != nil {
		row.Status, row.Secs = "error", time.Since(start).Seconds()
		return row
	}
	var d draftPayload
	if err := json.Unmarshal(obj, &d); err != nil {
		row.Status, row.Secs = "error", time.Since(start).Seconds()
		return row
	}
	dr := enrichment.Draft{Description: d.Description, SEOTitle: d.SEOTitle, MetaDescription: d.MetaDescription, AltText: d.AltText}
	vs := enrichment.Check(dr, facts)
	for _, v := range vs {
		row.Violations = append(row.Violations, v.String())
	}
	if len(vs) > 0 {
		// one re-draft with the violations named; a second failure is terminal
		feedback, _ := json.Marshal(map[string]any{"rewrite_avoiding_these_ungrounded_claims": row.Violations})
		if content2, tin2, tout2, err2 := rc.chat(ctx, draftModel, draftSystem, string(user)+"\n"+string(feedback)); err2 == nil {
			if obj2, err3 := parseJSONObject(content2); err3 == nil {
				var d2 draftPayload
				if json.Unmarshal(obj2, &d2) == nil {
					dr = enrichment.Draft{Description: d2.Description, SEOTitle: d2.SEOTitle, MetaDescription: d2.MetaDescription, AltText: d2.AltText}
					row.TokensIn += tin2
					row.TokensOut += tout2
					vs = enrichment.Check(dr, facts)
					row.Violations = nil
					for _, v := range vs {
						row.Violations = append(row.Violations, v.String())
					}
					d = d2
				}
			}
		}
		if len(vs) > 0 {
			row.Status, row.Secs = "grounding_fail", time.Since(start).Seconds()
			return row
		}
	}
	row.Draft = d

	juser, _ := json.Marshal(map[string]any{"product": string(user), "draft": d})
	jcontent, jtin, jtout, err := rc.chat(ctx, judgeModel, judgeSystem, string(juser))
	row.TokensIn += jtin
	row.TokensOut += jtout
	if err != nil {
		row.Status, row.Secs = "drafted", time.Since(start).Seconds() // judged unavailable: drafted, unjudged — never published
		row.Judge = "unavailable: " + err.Error()
		return row
	}
	if jobj, err := parseJSONObject(jcontent); err == nil {
		var j judgePayload
		if json.Unmarshal(jobj, &j) == nil {
			if j.Acceptable {
				row.Judge = "acceptable"
			} else {
				row.Judge = "reject: " + j.Reason
			}
		}
	}
	row.Status = "drafted"
	if strings.HasPrefix(row.Judge, "reject") {
		row.Status = "judge_reject"
	}
	row.Secs = time.Since(start).Seconds()
	return row
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "enrich-batch: "+f+"\n", a...)
	os.Exit(1)
}
