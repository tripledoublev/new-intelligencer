package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const consolidationStateFilename = "consolidation-state.json"

type ConsolidationState struct {
	LearnedBatchSizeByModel map[string]int `json:"learned_batch_size_by_model,omitempty"`
}

func suggestedConsolidationBatchSize(model string) int {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "qwen3.5:2b":
		return 8
	default:
		return 0
	}
}

func loadConsolidationState(dir string) (ConsolidationState, error) {
	path := filepath.Join(dir, consolidationStateFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ConsolidationState{}, nil
		}
		return ConsolidationState{}, fmt.Errorf("reading consolidation state: %w", err)
	}

	var state ConsolidationState
	if err := json.Unmarshal(data, &state); err != nil {
		return ConsolidationState{}, fmt.Errorf("parsing consolidation state JSON: %w", err)
	}
	if state.LearnedBatchSizeByModel == nil {
		state.LearnedBatchSizeByModel = make(map[string]int)
	}
	return state, nil
}

func saveConsolidationState(dir string, state ConsolidationState) error {
	if state.LearnedBatchSizeByModel == nil {
		state.LearnedBatchSizeByModel = make(map[string]int)
	}
	return saveJSONFile(filepath.Join(dir, consolidationStateFilename), state)
}

func loadLearnedConsolidationBatchSize(dir, model string) (int, error) {
	state, err := loadConsolidationState(dir)
	if err != nil {
		return 0, err
	}
	return state.LearnedBatchSizeByModel[model], nil
}

func saveLearnedConsolidationBatchSize(dir, model string, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("learned consolidation batch size must be positive")
	}

	return withLock(dir, "consolidation-state.lock", func() error {
		state, err := loadConsolidationState(dir)
		if err != nil {
			return err
		}
		if state.LearnedBatchSizeByModel == nil {
			state.LearnedBatchSizeByModel = make(map[string]int)
		}

		existing := state.LearnedBatchSizeByModel[model]
		if existing > 0 && existing <= batchSize {
			return nil
		}

		state.LearnedBatchSizeByModel[model] = batchSize
		return saveConsolidationState(dir, state)
	})
}
