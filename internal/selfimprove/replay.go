package selfimprove

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ReplayResult struct {
	Accepted []Evidence
	Rejected []ReplayReject
}

type ReplayReject struct {
	Line   int
	Reason string
}

func DecodeEvidenceReplay(r io.Reader) (ReplayResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var result ReplayResult
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Evidence
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			result.Rejected = append(result.Rejected, ReplayReject{Line: lineNo, Reason: "invalid json"})
			continue
		}
		if err := ValidateEvidence(ev); err != nil {
			result.Rejected = append(result.Rejected, ReplayReject{Line: lineNo, Reason: err.Error()})
			continue
		}
		result.Accepted = append(result.Accepted, ev)
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("selfimprove: replay scan: %w", err)
	}
	return result, nil
}

func EncodeRewardArtifactsNDJSON(w io.Writer, artifacts []RewardArtifact) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, artifact := range artifacts {
		if err := enc.Encode(artifact); err != nil {
			return fmt.Errorf("selfimprove: encode reward artifact: %w", err)
		}
	}
	return nil
}
