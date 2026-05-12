package runtimeobs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrUnsafeResourceProbe = errors.New("runtimeobs: unsafe resource probe")

type resourceProbeSample struct {
	SentruxDesktopProcesses int     `json:"sentrux_desktop_processes"`
	SentruxMCPProcesses     int     `json:"sentrux_mcp_processes"`
	MemoryFreePercent       float64 `json:"memory_free_percent"`
	FreePct                 float64 `json:"free_pct"`
}

func LoadProcessSnapshotFromResourceProbe(r io.Reader) (ProcessSnapshot, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var latest ProcessSnapshot
	found := false
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		sample, err := parseResourceProbeLine(line)
		if err != nil {
			return ProcessSnapshot{}, fmt.Errorf("resource probe line %d: %w", lineNo, err)
		}
		latest = ProcessSnapshot{
			SentruxDesktopProcesses: sample.SentruxDesktopProcesses,
			SentruxMCPProcesses:     sample.SentruxMCPProcesses,
			MemoryFreePercent:       sample.memoryFreePercent(),
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return ProcessSnapshot{}, fmt.Errorf("resource probe scan: %w", err)
	}
	if !found {
		return ProcessSnapshot{}, io.EOF
	}
	return latest, nil
}

func (s resourceProbeSample) memoryFreePercent() float64 {
	if s.MemoryFreePercent != 0 {
		return s.MemoryFreePercent
	}
	return s.FreePct
}

func parseResourceProbeLine(line string) (resourceProbeSample, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return resourceProbeSample{}, fmt.Errorf("decode: %w", err)
	}
	for key := range raw {
		if unsafeResourceProbeKey(key) {
			return resourceProbeSample{}, ErrUnsafeResourceProbe
		}
	}
	var sample resourceProbeSample
	if err := json.Unmarshal([]byte(line), &sample); err != nil {
		return resourceProbeSample{}, fmt.Errorf("decode sample: %w", err)
	}
	return sample, nil
}

func unsafeResourceProbeKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "cmd") ||
		strings.Contains(key, "argv") ||
		strings.Contains(key, "command") ||
		strings.Contains(key, "process_line")
}
