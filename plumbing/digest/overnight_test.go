package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type splittingTestEngine struct {
	categorizeCalls      []int
	failAll              bool
	consolidateCalls     []int
	failConsolidateAbove int
}

func (e *splittingTestEngine) Categorize(ctx context.Context, traceLabel string, sections []NewspaperSection, posts []DisplayPost) (EditorialCategorization, error) {
	e.categorizeCalls = append(e.categorizeCalls, len(posts))
	if e.failAll || len(posts) > 2 {
		return EditorialCategorization{}, fmt.Errorf("decoding ollama JSON payload: invalid character 'I' looking for beginning of value")
	}

	assignments := make([]EditorialAssignment, 0, len(posts))
	for _, post := range posts {
		assignments = append(assignments, EditorialAssignment{
			Rkey:      post.Rkey,
			SectionID: "tech",
		})
	}
	return EditorialCategorization{Assignments: assignments}, nil
}

func (e *splittingTestEngine) Consolidate(ctx context.Context, traceLabel string, section NewspaperSection, posts []DisplayPost) (EditorialConsolidation, error) {
	e.consolidateCalls = append(e.consolidateCalls, len(posts))
	if e.failConsolidateAbove > 0 && len(posts) > e.failConsolidateAbove {
		return EditorialConsolidation{}, fmt.Errorf("decoding ollama JSON payload: invalid character 'B' looking for beginning of value")
	}

	groups := make([]EditorialStoryDraft, 0, len(posts))
	for _, post := range posts {
		groups = append(groups, EditorialStoryDraft{
			PrimaryRkey: post.Rkey,
			PostRkeys:   []string{post.Rkey},
		})
	}
	return EditorialConsolidation{StoryGroups: groups}, nil
}

func (e *splittingTestEngine) SelectFrontPage(ctx context.Context, traceLabel string, maxStories int, candidates []FrontPageCandidate) (EditorialFrontPageSelection, error) {
	return EditorialFrontPageSelection{}, nil
}

func (e *splittingTestEngine) WriteHeadlines(ctx context.Context, traceLabel string, section NewspaperSection, stories []HeadlineCandidate) (EditorialHeadlinePlan, error) {
	return EditorialHeadlinePlan{}, nil
}

func TestCategorizationBatchComplete_CoveredBySubBatches(t *testing.T) {
	bp := &BatchProgress{
		Categorization: []CatBatch{
			{Offset: 40, Limit: 20},
			{Offset: 60, Limit: 20},
		},
	}

	assert.True(t, categorizationBatchComplete(bp, 40, 40))
	assert.False(t, categorizationBatchComplete(bp, 40, 60))
}

func TestRunCategorizationStage_SplitsFailedBatch(t *testing.T) {
	dir := t.TempDir()

	posts := []Post{
		{Rkey: "r1", Text: "one", Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: "two", Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: "three", Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: "four", Author: Author{Handle: "a"}},
	}
	index := PostsIndex{
		"r1": 0,
		"r2": 1,
		"r3": 2,
		"r4": 3,
	}
	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      posts,
		Index:      index,
		Categories: Categories{},
		Config: Config{
			Pipeline: PipelineSettings{Provider: "ollama"},
		},
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), Categories{}))

	engine := &splittingTestEngine{}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		BatchSize: 4,
		Model:     "test-model",
	}

	err := runner.runCategorizationStage(context.Background(), wd)
	require.NoError(t, err)

	assert.Equal(t, []int{4, 2, 2}, engine.categorizeCalls)

	bp, err := loadBatchProgress(dir)
	require.NoError(t, err)
	assert.True(t, categorizationBatchComplete(bp, 0, 4))

	cats, err := LoadCategories(filepath.Join(dir, "categories.json"))
	require.NoError(t, err)
	require.Contains(t, cats, "tech")
	assert.ElementsMatch(t, []string{"r1", "r2", "r3", "r4"}, cats["tech"].Visible)
}

func TestRunCategorizationStage_QuarantinesSingletonFailure(t *testing.T) {
	dir := t.TempDir()

	post := Post{Rkey: "r1", Text: "plain text", Author: Author{Handle: "a"}}
	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      []Post{post},
		Index:      PostsIndex{"r1": 0},
		Categories: Categories{},
		Config: Config{
			Pipeline: PipelineSettings{Provider: "ollama"},
		},
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), Categories{}))

	engine := &splittingTestEngine{failAll: true}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		BatchSize: 1,
		Model:     "test-model",
	}

	err := runner.runCategorizationStage(context.Background(), wd)
	require.NoError(t, err)

	quarantined, err := loadQuarantinedRoots(dir)
	require.NoError(t, err)
	require.Contains(t, quarantined, "r1")
	assert.Contains(t, quarantined["r1"].Reason, "invalid character")
	assert.Equal(t, "tech", quarantined["r1"].FallbackSectionID)

	bp, err := loadBatchProgress(dir)
	require.NoError(t, err)
	assert.True(t, categorizationBatchComplete(bp, 0, 1))
}

func TestRunCategorizationStage_SplitsLargePromptBeforeCall(t *testing.T) {
	dir := t.TempDir()

	longText := strings.Repeat("x", 9000)
	posts := []Post{
		{Rkey: "r1", Text: longText, Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: longText, Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: longText, Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: longText, Author: Author{Handle: "a"}},
	}
	index := PostsIndex{"r1": 0, "r2": 1, "r3": 2, "r4": 3}
	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      posts,
		Index:      index,
		Categories: Categories{},
		Config: Config{
			Pipeline: PipelineSettings{Provider: "ollama"},
		},
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), Categories{}))

	engine := &splittingTestEngine{}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		BatchSize: 4,
		Model:     "test-model",
	}

	err := runner.runCategorizationStage(context.Background(), wd)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 2}, engine.categorizeCalls)
}

func TestCategorizeBatchRange_SkipsCompletedCoveredSubrange(t *testing.T) {
	dir := t.TempDir()

	posts := []Post{
		{Rkey: "r1", Text: "one", Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: "two", Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: "three", Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: "four", Author: Author{Handle: "a"}},
	}
	index := PostsIndex{"r1": 0, "r2": 1, "r3": 2, "r4": 3}
	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      posts,
		Index:      index,
		Categories: Categories{},
		Config: Config{
			Pipeline: PipelineSettings{Provider: "ollama"},
		},
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), Categories{}))
	require.NoError(t, saveBatchProgress(dir, &BatchProgress{
		Categorization: []CatBatch{
			{Offset: 0, Limit: 2},
			{Offset: 2, Limit: 2},
		},
	}))

	engine := &splittingTestEngine{}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		BatchSize: 4,
		Model:     "test-model",
	}

	err := runner.categorizeBatchRange(context.Background(), wd, runner.NewspaperConfig.Sections, posts, 0, 4)
	require.NoError(t, err)
	assert.Empty(t, engine.categorizeCalls)
}

func TestRunCategorizationStage_UsesLearnedBatchCap(t *testing.T) {
	dir := t.TempDir()

	posts := []Post{
		{Rkey: "r1", Text: "one", Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: "two", Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: "three", Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: "four", Author: Author{Handle: "a"}},
	}
	index := PostsIndex{"r1": 0, "r2": 1, "r3": 2, "r4": 3}
	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      posts,
		Index:      index,
		Categories: Categories{},
		Config: Config{
			Pipeline: PipelineSettings{Provider: "ollama"},
		},
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), Categories{}))
	require.NoError(t, saveLearnedCategorizationBatchSize(dir, "qwen3.5:2b", 2))

	engine := &splittingTestEngine{}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		BatchSize: 40,
		Model:     "qwen3.5:2b",
	}

	err := runner.runCategorizationStage(context.Background(), wd)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 2}, engine.categorizeCalls)
}

func TestConsolidateSectionDrafts_SplitsFailedSection(t *testing.T) {
	engine := &splittingTestEngine{failConsolidateAbove: 2}
	runner := OvernightRunner{
		Engine: engine,
		Model:  "test-model",
	}

	section := NewspaperSection{ID: "tech-atproto", Name: "ATProto", Type: "news"}
	posts := []Post{
		{Rkey: "r1", Text: "one", Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: "two", Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: "three", Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: "four", Author: Author{Handle: "a"}},
	}

	drafts, err := runner.consolidateSectionDrafts(context.Background(), section, posts, "consolidate-tech-atproto")
	require.NoError(t, err)
	assert.Equal(t, []int{4, 2, 2}, engine.consolidateCalls)
	assert.Len(t, drafts, 4)
}

func TestInitializeConsolidationBatchSize_SuggestsQwenDefault(t *testing.T) {
	runner := OvernightRunner{Model: "qwen3.5:2b"}

	err := runner.initializeConsolidationBatchSize()
	require.NoError(t, err)
	assert.Equal(t, 8, runner.ConsolidationBatchSize)
}

func TestRunConsolidationStage_UsesLearnedBatchCap(t *testing.T) {
	dir := t.TempDir()

	posts := []Post{
		{Rkey: "r1", Text: "one", Author: Author{Handle: "a"}},
		{Rkey: "r2", Text: "two", Author: Author{Handle: "a"}},
		{Rkey: "r3", Text: "three", Author: Author{Handle: "a"}},
		{Rkey: "r4", Text: "four", Author: Author{Handle: "a"}},
		{Rkey: "r5", Text: "five", Author: Author{Handle: "a"}},
	}
	index := PostsIndex{"r1": 0, "r2": 1, "r3": 2, "r4": 3, "r5": 4}
	cats := Categories{
		"tech": {Visible: []string{"r1", "r2", "r3", "r4", "r5"}},
	}

	wd := &WorkspaceData{
		Dir:        dir,
		Posts:      posts,
		Index:      index,
		Categories: cats,
	}
	require.NoError(t, SaveCategories(filepath.Join(dir, "categories.json"), cats))
	require.NoError(t, saveLearnedConsolidationBatchSize(dir, "qwen3.5:2b", 2))

	engine := &splittingTestEngine{}
	runner := OvernightRunner{
		Dir:    dir,
		Engine: engine,
		NewspaperConfig: NewspaperConfig{
			Sections: []NewspaperSection{
				{ID: "tech", Name: "Technology", Type: "news"},
			},
		},
		Model: "qwen3.5:2b",
	}

	err := runner.runConsolidationStage(context.Background(), wd)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 2, 1}, engine.consolidateCalls)
	assert.Equal(t, 2, runner.ConsolidationBatchSize)
}
