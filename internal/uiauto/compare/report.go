package compare

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteReport writes diff.json and summary.md to outputDir. The directory
// is created if missing. Returns the two written paths so the caller can
// log evidence lines without re-deriving them.
func WriteReport(rep Report, outputDir string) (jsonPath, mdPath string, err error) {
	if err = os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir output dir %s: %w", outputDir, err)
	}
	jsonPath = filepath.Join(outputDir, "diff.json")
	jsonBytes, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal report json: %w", err)
	}
	if err = os.WriteFile(jsonPath, append(jsonBytes, '\n'), 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", jsonPath, err)
	}
	mdPath = filepath.Join(outputDir, "summary.md")
	mdFile, err := os.Create(mdPath)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", mdPath, err)
	}
	defer mdFile.Close()
	if err = renderMarkdown(mdFile, rep); err != nil {
		return "", "", fmt.Errorf("render markdown: %w", err)
	}
	return jsonPath, mdPath, nil
}

func renderMarkdown(w io.Writer, rep Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# uiauto vs Playwright comparison -- %s\n\n", rep.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Mode: `%s`\n\n", rep.Mode)
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "| Metric | Count |")
	fmt.Fprintln(&b, "|---|---:|")
	fmt.Fprintf(&b, "| Total scenarios | %d |\n", rep.Summary.Total)
	fmt.Fprintf(&b, "| Agreement | %d |\n", rep.Summary.Agreed)
	fmt.Fprintf(&b, "| Disagreement | %d |\n", rep.Summary.Disagreed)
	fmt.Fprintf(&b, "| Both pass | %d |\n", rep.Summary.BothPass)
	fmt.Fprintf(&b, "| Both fail | %d |\n", rep.Summary.BothFail)
	fmt.Fprintf(&b, "| Playwright only pass | %d |\n", rep.Summary.PlaywrightOnlyPass)
	fmt.Fprintf(&b, "| uiauto only pass | %d |\n", rep.Summary.UIAutoOnlyPass)
	fmt.Fprintf(&b, "| Self-heal events total | %d |\n\n", rep.Summary.SelfHealEvents)
	fmt.Fprintln(&b, "## Per-scenario detail")
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "| Spec | Playwright | uiauto | Tier | Self-heal | Agreement |")
	fmt.Fprintln(&b, "|---|---|---|---|---:|:---:|")
	for _, it := range rep.Items {
		agree := "no"
		if it.Agreement {
			agree = "yes"
		}
		fmt.Fprintf(&b, "| `%s` | %s (%dms) | %s (%dms) | %s | %d | %s |\n",
			it.Spec,
			it.Playwright.Result, it.Playwright.DurationMs,
			it.UIAuto.Result, it.UIAuto.DurationMs,
			it.UIAuto.TierUsed,
			len(it.UIAuto.SelfHealEvents),
			agree,
		)
	}
	for _, it := range rep.Items {
		if it.Notes == "" && len(it.UIAuto.SelfHealEvents) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### `%s`\n\n", it.Spec)
		if it.Notes != "" {
			fmt.Fprintf(&b, "%s\n\n", it.Notes)
		}
		if len(it.Playwright.Selectors) > 0 {
			fmt.Fprintln(&b, "Playwright selectors:")
			for _, s := range it.Playwright.Selectors {
				fmt.Fprintf(&b, "- `%s`\n", s)
			}
			fmt.Fprintln(&b, "")
		}
		if len(it.UIAuto.Selectors) > 0 {
			fmt.Fprintln(&b, "uiauto selectors:")
			for _, s := range it.UIAuto.Selectors {
				fmt.Fprintf(&b, "- `%s`\n", s)
			}
			fmt.Fprintln(&b, "")
		}
		if len(it.UIAuto.SelfHealEvents) > 0 {
			fmt.Fprintln(&b, "Self-heal events:")
			for _, ev := range it.UIAuto.SelfHealEvents {
				fmt.Fprintf(&b, "- step %d (tier=%s): `%s` -> `%s` -- %s\n", ev.Step, ev.Tier, ev.HealedFrom, ev.HealedTo, ev.Reason)
			}
			fmt.Fprintln(&b, "")
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return nil
}
