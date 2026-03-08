package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const categorizationStateFilename = "categorization-state.json"

type CategorizationState struct {
	LearnedBatchSizeByModel map[string]int `json:"learned_batch_size_by_model,omitempty"`
}

func suggestedCategorizationBatchSize(model string) int {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "qwen3.5:2b":
		return 10
	default:
		return 40
	}
}

func loadCategorizationState(dir string) (CategorizationState, error) {
	path := filepath.Join(dir, categorizationStateFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CategorizationState{}, nil
		}
		return CategorizationState{}, fmt.Errorf("reading categorization state: %w", err)
	}

	var state CategorizationState
	if err := json.Unmarshal(data, &state); err != nil {
		return CategorizationState{}, fmt.Errorf("parsing categorization state JSON: %w", err)
	}
	if state.LearnedBatchSizeByModel == nil {
		state.LearnedBatchSizeByModel = make(map[string]int)
	}
	return state, nil
}

func saveCategorizationState(dir string, state CategorizationState) error {
	if state.LearnedBatchSizeByModel == nil {
		state.LearnedBatchSizeByModel = make(map[string]int)
	}
	return saveJSONFile(filepath.Join(dir, categorizationStateFilename), state)
}

func loadLearnedCategorizationBatchSize(dir, model string) (int, error) {
	state, err := loadCategorizationState(dir)
	if err != nil {
		return 0, err
	}
	return state.LearnedBatchSizeByModel[model], nil
}

func saveLearnedCategorizationBatchSize(dir, model string, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("learned categorization batch size must be positive")
	}

	return withLock(dir, "categorization-state.lock", func() error {
		state, err := loadCategorizationState(dir)
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
		return saveCategorizationState(dir, state)
	})
}
