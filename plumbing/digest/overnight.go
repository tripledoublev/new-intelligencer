package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/cobra"
)

const maxCategorizationPromptChars = 30000
const maxConsolidationPromptChars = 24000
const maxFrontPagePromptChars = 20000
const maxHeadlinesPromptChars = 22000

var overnightCmd = &cobra.Command{
	Use:   "overnight",
	Short: "Run the local Ollama pipeline and emit a markdown newspaper",
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		outputFormat, _ := cmd.Flags().GetString("output")
		sinceStr, _ := cmd.Flags().GetString("since")
		limit, _ := cmd.Flags().GetInt("limit")
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		modelOverride, _ := cmd.Flags().GetString("model")
		categorizationModelOverride, _ := cmd.Flags().GetString("categorization-model")
		consolidationModelOverride, _ := cmd.Flags().GetString("consolidation-model")
		frontPageModelOverride, _ := cmd.Flags().GetString("front-page-model")
		headlinesModelOverride, _ := cmd.Flags().GetString("headlines-model")
		ollamaUsernameOverride, _ := cmd.Flags().GetString("ollama-username")
		ollamaPasswordOverride, _ := cmd.Flags().GetString("ollama-password")
		allowFallbacks, _ := cmd.Flags().GetBool("allow-fallbacks")
		timeoutOverride, _ := cmd.Flags().GetInt("ollama-timeout-seconds")

		if provider != "ollama" {
			return fmt.Errorf("overnight currently supports only --provider ollama")
		}

		since := time.Now().Add(-24 * time.Hour)
		if sinceStr != "" {
			parsed, err := time.Parse(time.RFC3339, sinceStr)
			if err != nil {
				return fmt.Errorf("invalid --since timestamp: %w", err)
			}
			since = parsed
		}

		ollamaCfg, err := LoadOllamaConfig()
		if err != nil {
			return fmt.Errorf("loading ollama config: %w", err)
		}
		if modelOverride != "" {
			ollamaCfg.Model = modelOverride
		}
		if cmd.Flags().Changed("ollama-username") {
			ollamaCfg.Username = ollamaUsernameOverride
		}
		if cmd.Flags().Changed("ollama-password") {
			ollamaCfg.Password = ollamaPasswordOverride
		}
		if timeoutOverride > 0 {
			ollamaCfg.TimeoutSeconds = timeoutOverride
		}
		categorizationModel := resolveStageModel(ollamaCfg.Model, ollamaCfg.CategorizationModel, categorizationModelOverride)
		consolidationModel := resolveStageModel(ollamaCfg.Model, ollamaCfg.ConsolidationModel, consolidationModelOverride)
		frontPageModel := resolveStageModel(ollamaCfg.Model, ollamaCfg.FrontPageModel, frontPageModelOverride)
		headlinesModel := resolveStageModel(ollamaCfg.Model, ollamaCfg.HeadlinesModel, headlinesModelOverride)
		if !cmd.Flags().Changed("batch-size") {
			batchSize = suggestedCategorizationBatchSize(categorizationModel)
		}

		dir, err := ensureOvernightWorkspace(since, PipelineSettings{
			Provider:            provider,
			OutputFormat:        outputFormat,
			Model:               ollamaCfg.Model,
			CategorizationModel: categorizationModel,
			ConsolidationModel:  consolidationModel,
			FrontPageModel:      frontPageModel,
			HeadlinesModel:      headlinesModel,
		})
		if err != nil {
			return err
		}

		newspaperConfig, err := loadProjectNewspaperConfig(dir)
		if err != nil {
			return err
		}

		traceDir := filepath.Join(dir, "ollama-traces")
		defaultEngine, engineForModel := buildOllamaEngines(ollamaCfg, traceDir)

		runner := OvernightRunner{
			Dir:                  dir,
			Engine:               defaultEngine,
			CategorizationEngine: engineForModel(categorizationModel),
			ConsolidationEngine:  engineForModel(consolidationModel),
			FrontPageEngine:      engineForModel(frontPageModel),
			HeadlinesEngine:      engineForModel(headlinesModel),
			NewspaperConfig:      newspaperConfig,
			BatchSize:            batchSize,
			FetchLimit:           limit,
			OutputFormat:         outputFormat,
			AllowFallbacks:       allowFallbacks,
			Model:                ollamaCfg.Model,
			CategorizationModel:  categorizationModel,
			ConsolidationModel:   consolidationModel,
			FrontPageModel:       frontPageModel,
			HeadlinesModel:       headlinesModel,
		}

		outputPath, err := runner.Run(cmd.Context())
		if err != nil {
			return err
		}

		fmt.Printf("Overnight pipeline finished: %s\n", outputPath)
		return nil
	},
}

type OvernightRunner struct {
	Dir                     string
	Engine                  LocalEditorialEngine
	CategorizationEngine    LocalEditorialEngine
	ConsolidationEngine     LocalEditorialEngine
	FrontPageEngine         LocalEditorialEngine
	HeadlinesEngine         LocalEditorialEngine
	NewspaperConfig         NewspaperConfig
	BatchSize               int
	ConsolidationBatchSize  int
	FrontPageCandidateLimit int
	HeadlineBatchSize       int
	FetchLimit              int
	OutputFormat            string
	AllowFallbacks          bool
	Model                   string
	CategorizationModel     string
	ConsolidationModel      string
	FrontPageModel          string
	HeadlinesModel          string
}

func resolveStageModel(defaultModel, envOverride, flagOverride string) string {
	if strings.TrimSpace(flagOverride) != "" {
		return strings.TrimSpace(flagOverride)
	}
	if strings.TrimSpace(envOverride) != "" {
		return strings.TrimSpace(envOverride)
	}
	return strings.TrimSpace(defaultModel)
}

func buildOllamaEngines(cfg OllamaEnvConfig, traceDir string) (LocalEditorialEngine, func(string) LocalEditorialEngine) {
	defaultEngine := NewOllamaEditorialEngine(cfg, traceDir)
	cache := map[string]LocalEditorialEngine{
		strings.TrimSpace(cfg.Model): defaultEngine,
	}

	return defaultEngine, func(model string) LocalEditorialEngine {
		model = strings.TrimSpace(model)
		if model == "" {
			return defaultEngine
		}
		if engine, ok := cache[model]; ok {
			return engine
		}

		overrideCfg := cfg
		overrideCfg.Model = model
		engine := NewOllamaEditorialEngine(overrideCfg, filepath.Join(traceDir, sanitizeTraceLabel(model)))
		cache[model] = engine
		return engine
	}
}

func (r *OvernightRunner) categorizationModelName() string {
	if strings.TrimSpace(r.CategorizationModel) != "" {
		return strings.TrimSpace(r.CategorizationModel)
	}
	return strings.TrimSpace(r.Model)
}

func (r *OvernightRunner) consolidationModelName() string {
	if strings.TrimSpace(r.ConsolidationModel) != "" {
		return strings.TrimSpace(r.ConsolidationModel)
	}
	return strings.TrimSpace(r.Model)
}

func (r *OvernightRunner) frontPageModelName() string {
	if strings.TrimSpace(r.FrontPageModel) != "" {
		return strings.TrimSpace(r.FrontPageModel)
	}
	return strings.TrimSpace(r.Model)
}

func (r *OvernightRunner) headlinesModelName() string {
	if strings.TrimSpace(r.HeadlinesModel) != "" {
		return strings.TrimSpace(r.HeadlinesModel)
	}
	return strings.TrimSpace(r.Model)
}

func (r *OvernightRunner) categorizationEngine() LocalEditorialEngine {
	if r.CategorizationEngine != nil {
		return r.CategorizationEngine
	}
	return r.Engine
}

func (r *OvernightRunner) consolidationEngine() LocalEditorialEngine {
	if r.ConsolidationEngine != nil {
		return r.ConsolidationEngine
	}
	return r.Engine
}

func (r *OvernightRunner) frontPageEngine() LocalEditorialEngine {
	if r.FrontPageEngine != nil {
		return r.FrontPageEngine
	}
	return r.Engine
}

func (r *OvernightRunner) headlinesEngine() LocalEditorialEngine {
	if r.HeadlinesEngine != nil {
		return r.HeadlinesEngine
	}
	return r.Engine
}

func (r OvernightRunner) Run(ctx context.Context) (string, error) {
	wd, err := LoadWorkspace(r.Dir)
	if err != nil {
		return "", err
	}

	if err := r.ensurePostsFetched(wd); err != nil {
		return "", err
	}
	if err := r.runCategorizationStage(ctx, wd); err != nil {
		return "", err
	}
	if err := r.runConsolidationStage(ctx, wd); err != nil {
		return "", err
	}
	if err := r.runFrontPageStage(ctx, wd); err != nil {
		return "", err
	}

	created, err := autoGroupRemainingInDir(r.Dir)
	if err != nil {
		return "", err
	}
	if created > 0 {
		fmt.Printf("Auto-grouped %d remaining posts\n", created)
	}

	if err := r.runHeadlineStage(ctx, wd); err != nil {
		return "", err
	}

	wd, err = LoadWorkspace(r.Dir)
	if err != nil {
		return "", err
	}

	return compileWorkspaceOutput(wd, r.OutputFormat)
}

func ensureOvernightWorkspace(since time.Time, pipeline PipelineSettings) (string, error) {
	dir := workspaceDir
	if dir == "" {
		dir = GenerateWorkspaceDir(since)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating workspace directory: %w", err)
	}

	configPath := filepath.Join(dir, "config.json")
	config := Config{}
	if _, err := os.Stat(configPath); err == nil {
		loaded, err := LoadConfig(configPath)
		if err != nil {
			return "", err
		}
		config = loaded
	}

	if config.Version == "" {
		config.Version = "1"
	}
	if config.CreatedAt.IsZero() {
		config.CreatedAt = time.Now()
	}
	if config.TimeRange.Since.IsZero() {
		config.TimeRange.Since = since
	}
	config.Pipeline = pipeline

	if err := SaveConfig(configPath, config); err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(dir, "posts.json")); os.IsNotExist(err) {
		if err := SavePosts(filepath.Join(dir, "posts.json"), []Post{}); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "posts-index.json")); os.IsNotExist(err) {
		if err := SaveIndex(filepath.Join(dir, "posts-index.json"), PostsIndex{}); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "categories.json")); os.IsNotExist(err) {
		if err := SaveCategories(filepath.Join(dir, "categories.json"), Categories{}); err != nil {
			return "", err
		}
	}

	return dir, nil
}

func (r OvernightRunner) ensurePostsFetched(wd *WorkspaceData) error {
	if len(wd.Posts) > 0 {
		fmt.Printf("Using existing posts: %d\n", len(wd.Posts))
		return nil
	}

	var envCfg EnvConfig
	if err := envconfig.Process("", &envCfg); err != nil {
		return fmt.Errorf("loading Bluesky credentials from environment: %w", err)
	}

	fmt.Printf("Fetching posts from Bluesky since %s\n", wd.Config.TimeRange.Since.Format(time.RFC3339))
	client, err := Authenticate(envCfg.Handle, envCfg.Password, envCfg.PDSHost)
	if err != nil {
		return err
	}

	result, err := FetchPosts(client, wd.Config.TimeRange.Since, r.FetchLimit)
	if err != nil {
		return err
	}

	wd.Posts = result.Posts
	wd.Index = result.Index
	if err := SaveWorkspaceData(wd); err != nil {
		return err
	}

	fmt.Printf("Fetched %d posts\n", result.Total)
	return nil
}

func (r *OvernightRunner) runCategorizationStage(ctx context.Context, wd *WorkspaceData) error {
	if err := r.initializeCategorizationBatchSize(); err != nil {
		return err
	}

	candidateSections := make([]NewspaperSection, 0, len(r.NewspaperConfig.Sections))
	for _, section := range r.NewspaperConfig.Sections {
		if section.ID != "front-page" {
			candidateSections = append(candidateSections, section)
		}
	}

	roots := wd.GetThreadRoots()
	if len(roots) == 0 {
		fmt.Println("Categorization: no posts to categorize")
		return nil
	}

	bp, _ := loadBatchProgress(r.Dir)
	for offset := 0; offset < len(roots); {
		limit := min(r.BatchSize, len(roots)-offset)
		if categorizationBatchComplete(bp, offset, limit) {
			offset += limit
			continue
		}
		if err := r.categorizeBatchRange(ctx, wd, candidateSections, roots, offset, limit); err != nil {
			return err
		}
		offset += limit
		bp, _ = loadBatchProgress(r.Dir)
	}

	return nil
}

func (r *OvernightRunner) initializeCategorizationBatchSize() error {
	if r.BatchSize <= 0 {
		r.BatchSize = suggestedCategorizationBatchSize(r.categorizationModelName())
	}

	learned, err := loadLearnedCategorizationBatchSize(r.Dir, r.categorizationModelName())
	if err != nil {
		return err
	}
	if learned > 0 && learned < r.BatchSize {
		fmt.Printf("Using learned categorization batch cap %d for %s\n", learned, r.categorizationModelName())
		r.BatchSize = learned
	}

	return nil
}

func (r *OvernightRunner) lowerCategorizationBatchSize(newCap int) {
	if newCap <= 0 {
		newCap = 1
	}
	if r.BatchSize > 0 && newCap >= r.BatchSize {
		return
	}

	previous := r.BatchSize
	r.BatchSize = newCap
	if err := saveLearnedCategorizationBatchSize(r.Dir, r.categorizationModelName(), newCap); err != nil {
		fmt.Printf("Warning: failed to persist learned categorization batch cap %d for %s: %v\n", newCap, r.categorizationModelName(), err)
		return
	}

	fmt.Printf("Lowering categorization batch cap to %d for %s (was %d)\n", newCap, r.categorizationModelName(), previous)
}

func (r *OvernightRunner) categorizeBatchRange(ctx context.Context, wd *WorkspaceData, candidateSections []NewspaperSection, roots []Post, offset, limit int) error {
	bp, _ := loadBatchProgress(r.Dir)
	if categorizationBatchComplete(bp, offset, limit) {
		return nil
	}

	batch := roots[offset : offset+limit]
	display := FormatForDisplayWithThreads(batch, wd)
	traceLabel := fmt.Sprintf("categorize-batch-%04d-%04d", offset, offset+limit)
	promptChars, promptErr := categorizationPromptChars(candidateSections, display)
	if promptErr == nil && promptChars > maxCategorizationPromptChars {
		if limit > 1 {
			leftLimit := limit / 2
			rightLimit := limit - leftLimit
			r.lowerCategorizationBatchSize(leftLimit)
			fmt.Printf("Categorization batch %d-%d exceeds prompt budget (%d chars); retrying as %d-%d and %d-%d\n",
				offset, offset+limit,
				promptChars,
				offset, offset+leftLimit,
				offset+leftLimit, offset+limit,
			)
			if err := r.categorizeBatchRange(ctx, wd, candidateSections, roots, offset, leftLimit); err != nil {
				return err
			}
			return r.categorizeBatchRange(ctx, wd, candidateSections, roots, offset+leftLimit, rightLimit)
		}

		return r.quarantineCategorizationRoot(wd, batch[0], candidateSections, offset, limit, traceLabel,
			fmt.Sprintf("categorization prompt too large (%d chars > %d)", promptChars, maxCategorizationPromptChars),
			promptChars,
		)
	}

	fmt.Printf("Categorizing batch %d-%d with %s (%d roots)\n", offset, offset+limit, r.categorizationModelName(), len(batch))
	resp, err := r.categorizationEngine().Categorize(ctx, traceLabel, candidateSections, display)
	if err != nil {
		if limit > 1 && shouldSplitCategorizationBatch(err) {
			leftLimit := limit / 2
			rightLimit := limit - leftLimit
			r.lowerCategorizationBatchSize(leftLimit)
			fmt.Printf("Categorization batch %d-%d failed; retrying as %d-%d and %d-%d: %v\n",
				offset, offset+limit,
				offset, offset+leftLimit,
				offset+leftLimit, offset+limit,
				err,
			)
			if err := r.categorizeBatchRange(ctx, wd, candidateSections, roots, offset, leftLimit); err != nil {
				return err
			}
			return r.categorizeBatchRange(ctx, wd, candidateSections, roots, offset+leftLimit, rightLimit)
		}
		if limit == 1 && shouldSplitCategorizationBatch(err) {
			return r.quarantineCategorizationRoot(wd, batch[0], candidateSections, offset, limit, traceLabel, err.Error(), promptChars)
		}

		if !r.AllowFallbacks {
			return err
		}
		fmt.Printf("Categorization fallback for batch %d-%d: %v\n", offset, offset+limit, err)
		resp = fallbackCategorization(batch, candidateSections)
	}

	resp = normalizeCategorization(batch, candidateSections, resp)
	return r.applyCategorizationBatch(wd, resp, offset, limit)
}

func (r *OvernightRunner) quarantineCategorizationRoot(wd *WorkspaceData, root Post, candidateSections []NewspaperSection, offset, limit int, traceLabel, reason string, promptChars int) error {
	resp := normalizeCategorization([]Post{root}, candidateSections, fallbackCategorization([]Post{root}, candidateSections))

	fallbackSectionID := ""
	if len(resp.Assignments) > 0 {
		fallbackSectionID = resp.Assignments[0].SectionID
	}
	if err := quarantineRootInDir(r.Dir, root, reason, traceLabel, promptChars, fallbackSectionID); err != nil {
		return err
	}

	fmt.Printf("Quarantined root %s; using heuristic categorization into %s (%s)\n", root.Rkey, fallbackSectionID, reason)
	return r.applyCategorizationBatch(wd, resp, offset, limit)
}

func (r *OvernightRunner) applyCategorizationBatch(wd *WorkspaceData, resp EditorialCategorization, offset, limit int) error {
	sectionRkeys := make(map[string][]string)
	for _, assignment := range resp.Assignments {
		sectionRkeys[assignment.SectionID] = append(sectionRkeys[assignment.SectionID], assignment.Rkey)
		for _, replyRkey := range wd.GetReplies(assignment.Rkey) {
			sectionRkeys[assignment.SectionID] = append(sectionRkeys[assignment.SectionID], replyRkey)
		}
	}

	for sectionID, rkeys := range sectionRkeys {
		if err := CategorizePosts(wd.Categories, wd.Index, sectionID, uniqueStrings(rkeys)); err != nil {
			return err
		}
	}
	if err := SaveCategories(filepath.Join(wd.Dir, "categories.json"), wd.Categories); err != nil {
		return err
	}
	if err := markBatchDoneInDir(r.Dir, "categorization", offset, limit, ""); err != nil {
		return err
	}
	fmt.Printf("Categorization batch %d-%d complete\n", offset, offset+limit)
	return nil
}

func (r *OvernightRunner) runConsolidationStage(ctx context.Context, wd *WorkspaceData) error {
	if err := r.initializeConsolidationBatchSize(); err != nil {
		return err
	}

	bp, _ := loadBatchProgress(r.Dir)
	for _, section := range newsSections(r.NewspaperConfig) {
		catData, ok := wd.Categories[section.ID]
		if !ok || len(catData.Visible) == 0 {
			continue
		}
		if stringSliceContains(bp.Consolidation, section.ID) {
			continue
		}

		posts := postsForRkeys(wd, catData.Visible)
		fmt.Printf("Consolidating %s with %s (%d posts)\n", section.ID, r.consolidationModelName(), len(posts))
		drafts, err := r.consolidateSectionDrafts(ctx, section, posts, "consolidate-"+section.ID)
		if err != nil {
			return err
		}

		if err := replaceStoryGroupsForSection(r.Dir, section.ID, drafts); err != nil {
			return err
		}
		if err := markBatchDoneInDir(r.Dir, "consolidation", 0, 0, section.ID); err != nil {
			return err
		}
		fmt.Printf("Consolidation for %s complete\n", section.ID)
	}

	return nil
}

func (r *OvernightRunner) consolidateSectionDrafts(ctx context.Context, section NewspaperSection, posts []Post, traceLabel string) ([]EditorialStoryDraft, error) {
	if r.ConsolidationBatchSize > 0 && len(posts) > r.ConsolidationBatchSize {
		fmt.Printf("Consolidation %s exceeds learned batch cap (%d posts); chunking %d posts into <=%d-post batches\n",
			section.ID, r.ConsolidationBatchSize, len(posts), r.ConsolidationBatchSize,
		)

		var drafts []EditorialStoryDraft
		for start := 0; start < len(posts); {
			cap := r.ConsolidationBatchSize
			if cap <= 0 {
				cap = len(posts) - start
			}
			end := min(start+cap, len(posts))
			chunkDrafts, err := r.consolidateSectionDrafts(
				ctx,
				section,
				posts[start:end],
				fmt.Sprintf("%s-part-%04d-%04d", traceLabel, start, end),
			)
			if err != nil {
				return nil, err
			}
			drafts = append(drafts, chunkDrafts...)
			start = end
		}
		return drafts, nil
	}

	display := FormatForDisplay(posts)
	promptChars, promptErr := consolidationPromptChars(section, display)
	if promptErr == nil && promptChars > maxConsolidationPromptChars && len(posts) > 1 {
		mid := len(posts) / 2
		r.lowerConsolidationBatchSize(mid)
		fmt.Printf("Consolidation %s exceeds prompt budget (%d chars); retrying as %d and %d posts\n",
			section.ID, promptChars, mid, len(posts)-mid,
		)
		left, err := r.consolidateSectionDrafts(ctx, section, posts[:mid], traceLabel+"-part-1")
		if err != nil {
			return nil, err
		}
		right, err := r.consolidateSectionDrafts(ctx, section, posts[mid:], traceLabel+"-part-2")
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	}

	resp, err := r.consolidationEngine().Consolidate(ctx, traceLabel, section, display)
	if err != nil {
		if len(posts) > 1 && shouldSplitCategorizationBatch(err) {
			mid := len(posts) / 2
			r.lowerConsolidationBatchSize(mid)
			fmt.Printf("Consolidation %s failed; retrying as %d and %d posts: %v\n",
				section.ID, mid, len(posts)-mid, err,
			)
			left, err := r.consolidateSectionDrafts(ctx, section, posts[:mid], traceLabel+"-part-1")
			if err != nil {
				return nil, err
			}
			right, err := r.consolidateSectionDrafts(ctx, section, posts[mid:], traceLabel+"-part-2")
			if err != nil {
				return nil, err
			}
			return append(left, right...), nil
		}
		if !r.AllowFallbacks {
			return nil, err
		}
		fmt.Printf("Consolidation fallback for %s: %v\n", section.ID, err)
		resp = fallbackConsolidation(posts)
	}

	resp = normalizeConsolidation(posts, resp)
	return resp.StoryGroups, nil
}

func (r *OvernightRunner) initializeConsolidationBatchSize() error {
	if r.ConsolidationBatchSize <= 0 {
		r.ConsolidationBatchSize = suggestedConsolidationBatchSize(r.consolidationModelName())
	}

	learned, err := loadLearnedConsolidationBatchSize(r.Dir, r.consolidationModelName())
	if err != nil {
		return err
	}
	if learned > 0 && (r.ConsolidationBatchSize == 0 || learned < r.ConsolidationBatchSize) {
		fmt.Printf("Using learned consolidation batch cap %d for %s\n", learned, r.consolidationModelName())
		r.ConsolidationBatchSize = learned
	}

	return nil
}

func (r *OvernightRunner) lowerConsolidationBatchSize(newCap int) {
	if newCap <= 0 {
		newCap = 1
	}
	if r.ConsolidationBatchSize > 0 && newCap >= r.ConsolidationBatchSize {
		return
	}

	previous := r.ConsolidationBatchSize
	r.ConsolidationBatchSize = newCap
	if err := saveLearnedConsolidationBatchSize(r.Dir, r.consolidationModelName(), newCap); err != nil {
		fmt.Printf("Warning: failed to persist learned consolidation batch cap %d for %s: %v\n", newCap, r.consolidationModelName(), err)
		return
	}

	if previous > 0 {
		fmt.Printf("Lowering consolidation batch cap to %d for %s (was %d)\n", newCap, r.consolidationModelName(), previous)
		return
	}
	fmt.Printf("Setting consolidation batch cap to %d for %s\n", newCap, r.consolidationModelName())
}

func (r *OvernightRunner) runFrontPageStage(ctx context.Context, wd *WorkspaceData) error {
	bp, _ := loadBatchProgress(r.Dir)
	if bp.FrontPage {
		return nil
	}

	storyGroups, err := LoadStoryGroups(filepath.Join(r.Dir, "story-groups.json"))
	if err != nil {
		return err
	}

	candidates := buildFrontPageCandidates(storyGroups, wd, r.NewspaperConfig)
	if len(candidates) == 0 {
		return markBatchDoneInDir(r.Dir, "front-page", 0, 0, "")
	}

	maxStories := 5
	for _, section := range r.NewspaperConfig.Sections {
		if section.ID == "front-page" && section.MaxStories > 0 {
			maxStories = section.MaxStories
			break
		}
	}

	fmt.Printf("Selecting front page with %s (%d candidate stories)\n", r.frontPageModelName(), len(candidates))
	resp, err := r.selectFrontPageCandidates(ctx, maxStories, candidates)
	if err != nil {
		if !r.AllowFallbacks {
			return err
		}
		fmt.Printf("Front-page fallback: %v\n", err)
		resp = fallbackFrontPageSelection(candidates, maxStories)
	}
	resp = normalizeFrontPageSelection(candidates, maxStories, resp)

	if err := applyFrontPageSelection(r.Dir, resp.StoryIDs); err != nil {
		return err
	}
	fmt.Printf("Front page selection complete (%d stories)\n", len(resp.StoryIDs))
	return markBatchDoneInDir(r.Dir, "front-page", 0, 0, "")
}

func (r *OvernightRunner) runHeadlineStage(ctx context.Context, wd *WorkspaceData) error {
	bp, _ := loadBatchProgress(r.Dir)
	storyGroups, err := LoadStoryGroups(filepath.Join(r.Dir, "story-groups.json"))
	if err != nil {
		return err
	}

	for _, section := range sectionsWithStoriesOrdered(storyGroups, r.NewspaperConfig) {
		if stringSliceContains(bp.Headlines, section.ID) {
			continue
		}

		candidates := buildHeadlineCandidates(storyGroups, wd, section.ID)
		if len(candidates) == 0 {
			continue
		}

		fmt.Printf("Writing headlines for %s with %s (%d stories)\n", section.ID, r.headlinesModelName(), len(candidates))
		resp, err := r.writeHeadlinePlan(ctx, section, candidates, "headlines-"+section.ID)
		if err != nil {
			if !r.AllowFallbacks {
				return err
			}
			fmt.Printf("Headline fallback for %s: %v\n", section.ID, err)
			resp = fallbackHeadlinePlan(section, candidates)
		}
		resp = normalizeHeadlinePlan(section, candidates, resp)

		if err := applyHeadlinePlan(r.Dir, section.ID, resp); err != nil {
			return err
		}
		if err := markBatchDoneInDir(r.Dir, "headlines", 0, 0, section.ID); err != nil {
			return err
		}
		fmt.Printf("Headlines for %s complete\n", section.ID)
	}

	return nil
}

func (r *OvernightRunner) initializeFrontPageCandidateLimit() {
	if r.FrontPageCandidateLimit > 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(r.frontPageModelName())) {
	case "qwen3.5:2b":
		r.FrontPageCandidateLimit = 24
	}
}

func (r *OvernightRunner) lowerFrontPageCandidateLimit(newCap int, maxStories int) {
	if newCap < maxStories {
		newCap = maxStories
	}
	if r.FrontPageCandidateLimit > 0 && newCap >= r.FrontPageCandidateLimit {
		return
	}

	previous := r.FrontPageCandidateLimit
	r.FrontPageCandidateLimit = newCap
	if previous > 0 {
		fmt.Printf("Lowering front-page candidate cap to %d for %s (was %d)\n", newCap, r.frontPageModelName(), previous)
		return
	}
	fmt.Printf("Setting front-page candidate cap to %d for %s\n", newCap, r.frontPageModelName())
}

func (r *OvernightRunner) selectFrontPageCandidates(ctx context.Context, maxStories int, candidates []FrontPageCandidate) (EditorialFrontPageSelection, error) {
	r.initializeFrontPageCandidateLimit()

	shortlisted := shortlistFrontPageCandidates(candidates, r.FrontPageCandidateLimit)
	promptChars, promptErr := frontPagePromptChars(maxStories, shortlisted)
	if promptErr == nil && promptChars > maxFrontPagePromptChars && len(shortlisted) > maxStories {
		newCap := len(shortlisted) / 2
		r.lowerFrontPageCandidateLimit(newCap, maxStories)
		fmt.Printf("Front-page selection exceeds prompt budget (%d chars); retrying with top %d candidates\n",
			promptChars, r.FrontPageCandidateLimit,
		)
		return r.selectFrontPageCandidates(ctx, maxStories, candidates)
	}

	resp, err := r.frontPageEngine().SelectFrontPage(ctx, "front-page-selection", maxStories, shortlisted)
	if err != nil {
		if len(shortlisted) > maxStories && shouldSplitCategorizationBatch(err) {
			newCap := len(shortlisted) / 2
			r.lowerFrontPageCandidateLimit(newCap, maxStories)
			fmt.Printf("Front-page selection failed; retrying with top %d candidates: %v\n",
				r.FrontPageCandidateLimit, err,
			)
			return r.selectFrontPageCandidates(ctx, maxStories, candidates)
		}
		return EditorialFrontPageSelection{}, err
	}

	return normalizeFrontPageSelection(shortlisted, maxStories, resp), nil
}

func shortlistFrontPageCandidates(candidates []FrontPageCandidate, limit int) []FrontPageCandidate {
	if limit <= 0 || len(candidates) <= limit {
		out := make([]FrontPageCandidate, len(candidates))
		copy(out, candidates)
		return out
	}

	var shortlisted []FrontPageCandidate
	selected := make(map[string]bool, limit)
	seenSections := make(map[string]bool)

	for _, candidate := range candidates {
		if seenSections[candidate.SectionID] {
			continue
		}
		shortlisted = append(shortlisted, candidate)
		selected[candidate.StoryID] = true
		seenSections[candidate.SectionID] = true
		if len(shortlisted) >= limit {
			return shortlisted
		}
	}

	for _, candidate := range candidates {
		if selected[candidate.StoryID] {
			continue
		}
		shortlisted = append(shortlisted, candidate)
		if len(shortlisted) >= limit {
			break
		}
	}

	return shortlisted
}

func (r *OvernightRunner) initializeHeadlineBatchSize() {
	if r.HeadlineBatchSize > 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(r.headlinesModelName())) {
	case "qwen3.5:2b":
		r.HeadlineBatchSize = 8
	}
}

func (r *OvernightRunner) lowerHeadlineBatchSize(newCap int) {
	if newCap <= 0 {
		newCap = 1
	}
	if r.HeadlineBatchSize > 0 && newCap >= r.HeadlineBatchSize {
		return
	}

	previous := r.HeadlineBatchSize
	r.HeadlineBatchSize = newCap
	if previous > 0 {
		fmt.Printf("Lowering headline batch cap to %d for %s (was %d)\n", newCap, r.headlinesModelName(), previous)
		return
	}
	fmt.Printf("Setting headline batch cap to %d for %s\n", newCap, r.headlinesModelName())
}

func (r *OvernightRunner) writeHeadlinePlan(ctx context.Context, section NewspaperSection, candidates []HeadlineCandidate, traceLabel string) (EditorialHeadlinePlan, error) {
	r.initializeHeadlineBatchSize()

	if r.HeadlineBatchSize > 0 && len(candidates) > r.HeadlineBatchSize {
		fmt.Printf("Headlines %s exceed learned batch cap (%d stories); chunking %d stories into <=%d-story batches\n",
			section.ID, r.HeadlineBatchSize, len(candidates), r.HeadlineBatchSize,
		)
		var revisions []EditorialStoryRevision
		for start := 0; start < len(candidates); {
			end := min(start+r.HeadlineBatchSize, len(candidates))
			part, err := r.writeHeadlinePlan(
				ctx,
				section,
				candidates[start:end],
				fmt.Sprintf("%s-part-%04d-%04d", traceLabel, start, end),
			)
			if err != nil {
				return EditorialHeadlinePlan{}, err
			}
			revisions = append(revisions, part.Stories...)
			start = end
		}
		return normalizeMergedHeadlinePlan(section, candidates, revisions), nil
	}

	promptChars, promptErr := headlinesPromptChars(section, candidates)
	if promptErr == nil && promptChars > maxHeadlinesPromptChars && len(candidates) > 1 {
		r.lowerHeadlineBatchSize(len(candidates) / 2)
		fmt.Printf("Headlines %s exceed prompt budget (%d chars); retrying in smaller batches\n",
			section.ID, promptChars,
		)
		return r.writeHeadlinePlan(ctx, section, candidates, traceLabel)
	}

	resp, err := r.headlinesEngine().WriteHeadlines(ctx, traceLabel, section, candidates)
	if err != nil {
		if len(candidates) > 1 && shouldSplitCategorizationBatch(err) {
			r.lowerHeadlineBatchSize(len(candidates) / 2)
			fmt.Printf("Headlines %s failed; retrying in smaller batches: %v\n", section.ID, err)
			return r.writeHeadlinePlan(ctx, section, candidates, traceLabel)
		}
		return EditorialHeadlinePlan{}, err
	}

	return normalizeHeadlinePlan(section, candidates, resp), nil
}

func normalizeMergedHeadlinePlan(section NewspaperSection, candidates []HeadlineCandidate, revisions []EditorialStoryRevision) EditorialHeadlinePlan {
	byID := make(map[string]EditorialStoryRevision, len(revisions))
	for _, revision := range revisions {
		if _, exists := byID[revision.StoryID]; exists {
			continue
		}
		byID[revision.StoryID] = revision
	}

	merged := make([]EditorialStoryRevision, 0, len(candidates))
	for _, candidate := range candidates {
		revision, ok := byID[candidate.StoryID]
		if !ok {
			revision = fallbackHeadlineRevision(section, candidate, len(merged)+1)
		}
		if strings.TrimSpace(revision.Headline) == "" {
			revision.Headline = fallbackHeadlineCandidate(candidate)
		}
		if strings.TrimSpace(revision.Summary) == "" {
			revision.Summary = fallbackSummaryCandidate(candidate)
		}
		revision.Priority = len(merged) + 1
		revision.Role = normalizeStoryRole(revision.Role, revision.IsOpinion)
		if revision.Role == "opinion" {
			revision.IsOpinion = true
		}
		merged = append(merged, revision)
	}

	normalizeHeadlineRoles(section, merged)
	return EditorialHeadlinePlan{Stories: merged}
}

func categorizationBatchComplete(bp *BatchProgress, offset, limit int) bool {
	if bp == nil {
		return false
	}

	targetEnd := offset + limit
	coveredUntil := offset

	batches := append([]CatBatch(nil), bp.Categorization...)
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].Offset == batches[j].Offset {
			return batches[i].Limit < batches[j].Limit
		}
		return batches[i].Offset < batches[j].Offset
	})

	for _, batch := range batches {
		batchStart := batch.Offset
		batchEnd := batch.Offset + batch.Limit

		if batchEnd <= coveredUntil {
			continue
		}
		if batchStart > coveredUntil {
			break
		}

		coveredUntil = batchEnd
		if coveredUntil >= targetEnd {
			return true
		}
	}

	return false
}

func shouldSplitCategorizationBatch(err error) bool {
	if err == nil {
		return false
	}
	if shouldRetryStructuredOutput(err) {
		return true
	}

	message := err.Error()
	return strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "Client.Timeout exceeded")
}

func categorizationPromptChars(sections []NewspaperSection, posts []DisplayPost) (int, error) {
	prompt, err := buildCategorizationPrompt(sections, posts)
	if err != nil {
		return 0, err
	}
	return len(prompt), nil
}

func consolidationPromptChars(section NewspaperSection, posts []DisplayPost) (int, error) {
	prompt, err := buildConsolidationPrompt(section, posts)
	if err != nil {
		return 0, err
	}
	return len(prompt), nil
}

func frontPagePromptChars(maxStories int, candidates []FrontPageCandidate) (int, error) {
	prompt, err := buildFrontPagePrompt(maxStories, candidates)
	if err != nil {
		return 0, err
	}
	return len(prompt), nil
}

func headlinesPromptChars(section NewspaperSection, candidates []HeadlineCandidate) (int, error) {
	prompt, err := buildHeadlinesPrompt(section, candidates)
	if err != nil {
		return 0, err
	}
	return len(prompt), nil
}

func postsForRkeys(wd *WorkspaceData, rkeys []string) []Post {
	posts := make([]Post, 0, len(rkeys))
	for _, rkey := range rkeys {
		if idx, ok := wd.Index[rkey]; ok && idx < len(wd.Posts) {
			posts = append(posts, wd.Posts[idx])
		}
	}
	return posts
}

func uniqueStrings(values []string) []string {
	var result []string
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		result = append(result, value)
		seen[value] = true
	}
	return result
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func replaceStoryGroupsForSection(dir, sectionID string, drafts []EditorialStoryDraft) error {
	return withLock(dir, "story-groups.lock", func() error {
		storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
		if err != nil {
			return err
		}

		for id, story := range storyGroups {
			if story.SectionID == sectionID {
				delete(storyGroups, id)
			}
		}

		for _, draft := range drafts {
			if len(draft.PostRkeys) == 0 {
				continue
			}
			id := nextStoryGroupID(storyGroups)
			primary := draft.PrimaryRkey
			if primary == "" || !contains(draft.PostRkeys, primary) {
				primary = draft.PostRkeys[0]
			}
			storyGroups[id] = StoryGroup{
				ID:            id,
				DraftHeadline: strings.TrimSpace(draft.DraftHeadline),
				Summary:       strings.TrimSpace(draft.Summary),
				PostRkeys:     uniqueStrings(draft.PostRkeys),
				PrimaryRkey:   primary,
				SectionID:     sectionID,
			}
		}

		return SaveStoryGroups(filepath.Join(dir, "story-groups.json"), storyGroups)
	})
}

func buildFrontPageCandidates(storyGroups StoryGroups, wd *WorkspaceData, newspaperConfig NewspaperConfig) []FrontPageCandidate {
	postIndex := make(map[string]Post, len(wd.Posts))
	for _, post := range wd.Posts {
		postIndex[post.Rkey] = post
	}
	sections := sectionMap(newsSections(newspaperConfig))

	var candidates []FrontPageCandidate
	for _, story := range storyGroups {
		if story.SectionID == "front-page" {
			continue
		}
		if _, ok := sections[story.SectionID]; !ok {
			continue
		}

		primary, ok := storyPrimaryPost(story, postIndex)
		if !ok {
			continue
		}

		candidates = append(candidates, FrontPageCandidate{
			StoryID:       story.ID,
			SectionID:     story.SectionID,
			Headline:      story.Headline,
			DraftHeadline: story.DraftHeadline,
			Summary:       story.Summary,
			PostCount:     len(story.PostRkeys),
			Engagement:    storyEngagementScore(story, postIndex),
			PrimaryPost:   formatSinglePost(primary),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Engagement == candidates[j].Engagement {
			return candidates[i].StoryID < candidates[j].StoryID
		}
		return candidates[i].Engagement > candidates[j].Engagement
	})

	return candidates
}

func applyFrontPageSelection(dir string, selectedIDs []string) error {
	storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
	if err != nil {
		return err
	}

	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}

	for _, id := range sortedStoryIDs(storyGroups, "front-page") {
		story := storyGroups[id]
		if selected[id] || story.OriginalSection == "" {
			continue
		}
		if err := moveStoryBetweenSectionsInDir(dir, id, story.OriginalSection); err != nil {
			return err
		}
	}

	for _, id := range selectedIDs {
		story, ok := storyGroups[id]
		if !ok || story.SectionID == "front-page" {
			continue
		}
		if err := moveStoryBetweenSectionsInDir(dir, id, "front-page"); err != nil {
			return err
		}
	}

	return nil
}

func autoGroupRemainingInDir(dir string) (int, error) {
	cats, err := LoadCategories(filepath.Join(dir, "categories.json"))
	if err != nil {
		return 0, err
	}

	created := 0
	err = withLock(dir, "story-groups.lock", func() error {
		storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
		if err != nil {
			return err
		}

		groupedBySection := make(map[string]map[string]bool)
		for _, story := range storyGroups {
			if groupedBySection[story.SectionID] == nil {
				groupedBySection[story.SectionID] = make(map[string]bool)
			}
			for _, rkey := range story.PostRkeys {
				groupedBySection[story.SectionID][rkey] = true
			}
		}

		for section, catData := range cats {
			if catData.IsHidden {
				continue
			}
			grouped := groupedBySection[section]
			if grouped == nil {
				grouped = make(map[string]bool)
			}

			for _, rkey := range catData.Visible {
				if grouped[rkey] {
					continue
				}
				id := nextStoryGroupID(storyGroups)
				storyGroups[id] = StoryGroup{
					ID:          id,
					PostRkeys:   []string{rkey},
					PrimaryRkey: rkey,
					SectionID:   section,
				}
				created++
			}
		}

		return SaveStoryGroups(filepath.Join(dir, "story-groups.json"), storyGroups)
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}

func buildHeadlineCandidates(storyGroups StoryGroups, wd *WorkspaceData, sectionID string) []HeadlineCandidate {
	postIndex := make(map[string]Post, len(wd.Posts))
	for _, post := range wd.Posts {
		postIndex[post.Rkey] = post
	}

	var candidates []HeadlineCandidate
	for _, storyID := range sortedStoryIDs(storyGroups, sectionID) {
		story := storyGroups[storyID]
		primary, ok := storyPrimaryPost(story, postIndex)
		if !ok {
			continue
		}
		posts := make([]Post, 0, len(story.PostRkeys))
		for _, rkey := range story.PostRkeys {
			if post, ok := postIndex[rkey]; ok {
				posts = append(posts, post)
			}
		}

		candidates = append(candidates, HeadlineCandidate{
			StoryID:       story.ID,
			SectionID:     story.SectionID,
			DraftHeadline: story.DraftHeadline,
			Headline:      story.Headline,
			Summary:       story.Summary,
			PostCount:     len(story.PostRkeys),
			Engagement:    storyEngagementScore(story, postIndex),
			PrimaryPost:   formatSinglePost(primary),
			Posts:         FormatForDisplay(posts),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Engagement == candidates[j].Engagement {
			return candidates[i].StoryID < candidates[j].StoryID
		}
		return candidates[i].Engagement > candidates[j].Engagement
	})

	return candidates
}

func applyHeadlinePlan(dir, sectionID string, plan EditorialHeadlinePlan) error {
	return withLock(dir, "story-groups.lock", func() error {
		storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
		if err != nil {
			return err
		}

		for _, revision := range plan.Stories {
			story, ok := storyGroups[revision.StoryID]
			if !ok || story.SectionID != sectionID {
				continue
			}
			story.Headline = strings.TrimSpace(revision.Headline)
			story.Priority = revision.Priority
			story.Role = normalizeStoryRole(revision.Role, revision.IsOpinion)
			story.IsOpinion = revision.IsOpinion || story.Role == "opinion"
			if summary := strings.TrimSpace(revision.Summary); summary != "" {
				story.Summary = summary
			}
			storyGroups[revision.StoryID] = story
		}

		return SaveStoryGroups(filepath.Join(dir, "story-groups.json"), storyGroups)
	})
}

func init() {
	overnightCmd.Flags().String("provider", "ollama", "Editorial backend for the local pipeline")
	overnightCmd.Flags().String("output", "markdown", "Output format for the final digest (markdown|html)")
	overnightCmd.Flags().String("since", "", "Start time for fetching (RFC3339, default: 24h ago)")
	overnightCmd.Flags().Int("limit", 0, "Max posts to fetch (0 = unlimited)")
	overnightCmd.Flags().Int("batch-size", 40, "Categorization batch size for the local pipeline")
	overnightCmd.Flags().String("model", "", "Override OLLAMA_MODEL for this run")
	overnightCmd.Flags().String("categorization-model", "", "Override OLLAMA_CATEGORIZATION_MODEL for this run")
	overnightCmd.Flags().String("consolidation-model", "", "Override OLLAMA_CONSOLIDATION_MODEL for this run")
	overnightCmd.Flags().String("front-page-model", "", "Override OLLAMA_FRONT_PAGE_MODEL for this run")
	overnightCmd.Flags().String("headlines-model", "", "Override OLLAMA_HEADLINES_MODEL for this run")
	overnightCmd.Flags().String("ollama-username", "", "Override OLLAMA_USERNAME for this run")
	overnightCmd.Flags().String("ollama-password", "", "Override OLLAMA_PASSWORD for this run")
	overnightCmd.Flags().Int("ollama-timeout-seconds", 0, "Override OLLAMA_TIMEOUT_SECONDS for this run")
	overnightCmd.Flags().Bool("allow-fallbacks", false, "Allow heuristic fallbacks when an Ollama call fails or times out")
}
