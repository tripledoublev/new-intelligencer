package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const quarantinedRootsFilename = "quarantined-roots.json"

type QuarantinedRoot struct {
	Rkey              string    `json:"rkey"`
	Reason            string    `json:"reason"`
	TraceLabel        string    `json:"trace_label,omitempty"`
	PromptChars       int       `json:"prompt_chars,omitempty"`
	FallbackSectionID string    `json:"fallback_section_id,omitempty"`
	QuarantinedAt     time.Time `json:"quarantined_at"`
}

type QuarantinedRoots map[string]QuarantinedRoot

func loadQuarantinedRoots(dir string) (QuarantinedRoots, error) {
	path := filepath.Join(dir, quarantinedRootsFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return QuarantinedRoots{}, nil
		}
		return nil, fmt.Errorf("reading quarantined roots: %w", err)
	}

	var roots QuarantinedRoots
	if err := json.Unmarshal(data, &roots); err != nil {
		return nil, fmt.Errorf("parsing quarantined roots JSON: %w", err)
	}
	if roots == nil {
		roots = QuarantinedRoots{}
	}
	return roots, nil
}

func saveQuarantinedRoots(dir string, roots QuarantinedRoots) error {
	return saveJSONFile(filepath.Join(dir, quarantinedRootsFilename), roots)
}

func quarantineRootInDir(dir string, root Post, reason, traceLabel string, promptChars int, fallbackSectionID string) error {
	return withLock(dir, "quarantined-roots.lock", func() error {
		roots, err := loadQuarantinedRoots(dir)
		if err != nil {
			return err
		}

		roots[root.Rkey] = QuarantinedRoot{
			Rkey:              root.Rkey,
			Reason:            reason,
			TraceLabel:        traceLabel,
			PromptChars:       promptChars,
			FallbackSectionID: fallbackSectionID,
			QuarantinedAt:     time.Now(),
		}

		return saveQuarantinedRoots(dir, roots)
	})
}
