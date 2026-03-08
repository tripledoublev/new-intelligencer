package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func validateStoryGroupsForCompile(storyGroups StoryGroups) error {
	var unprocessed []string
	var truncated []string
	for id, story := range storyGroups {
		if story.Headline == "" || story.Priority == 0 {
			missing := []string{}
			if story.Headline == "" {
				missing = append(missing, "headline")
			}
			if story.Priority == 0 {
				missing = append(missing, "priority")
			}
			unprocessed = append(unprocessed, fmt.Sprintf("  %s [%s] (missing: %s)", id, story.SectionID, joinStrings(missing, ", ")))
		}
		if hasTrailingEllipsis(story.Headline) {
			truncated = append(truncated, fmt.Sprintf("  %s [%s] (headline ends with ellipsis)", id, story.SectionID))
		}
		if hasTrailingEllipsis(story.Summary) {
			truncated = append(truncated, fmt.Sprintf("  %s [%s] (summary ends with ellipsis)", id, story.SectionID))
		}
	}
	if len(unprocessed) > 0 {
		sort.Strings(unprocessed)
		return fmt.Errorf("compile blocked: %d stories are unprocessed\n%s\n\nRun `./bin/digest show-unprocessed` for details",
			len(unprocessed), joinStrings(unprocessed, "\n"))
	}
	if len(truncated) > 0 {
		sort.Strings(truncated)
		return fmt.Errorf("compile blocked: %d stories have truncated editorial text\n%s",
			len(truncated), joinStrings(truncated, "\n"))
	}

	var frontPageHeadlines []string
	for id := range storyGroups {
		story := storyGroups[id]
		if story.SectionID != "front-page" || story.Role != "headline" {
			continue
		}
		frontPageHeadlines = append(frontPageHeadlines, fmt.Sprintf("%s: %s", id, story.Headline))
		if story.IsOpinion {
			return fmt.Errorf("front page headline cannot be an opinion piece: %s", id)
		}
	}
	if len(frontPageHeadlines) > 1 {
		return fmt.Errorf("multiple front-page headlines found (only one allowed):\n  - %s",
			joinStrings(frontPageHeadlines, "\n  - "))
	}

	return nil
}

func hasTrailingEllipsis(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return strings.HasSuffix(text, "...") || strings.HasSuffix(text, "\u2026")
}

func compileWorkspaceOutput(wd *WorkspaceData, format string) (string, error) {
	if wd == nil {
		return "", fmt.Errorf("workspace is required")
	}

	if format == "" {
		format = "html"
	}

	config := wd.Config
	if config.CreatedAt.IsZero() {
		config.CreatedAt = time.Now()
	}

	newspaperPath := filepath.Join(filepath.Dir(wd.Dir), "newspaper.json")
	newspaperConfig, err := LoadNewspaperConfig(newspaperPath)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", newspaperPath, err)
	}

	storyGroups, err := LoadStoryGroups(filepath.Join(wd.Dir, "story-groups.json"))
	if err != nil {
		return "", err
	}
	contentPicks, err := LoadContentPicks(filepath.Join(wd.Dir, "content-picks.json"))
	if err != nil {
		return "", err
	}

	if err := validateStoryGroupsForCompile(storyGroups); err != nil {
		return "", err
	}

	var (
		outputPath string
		content    string
	)

	switch format {
	case "html":
		content, err = CompileDigestHTML(wd.Posts, wd.Categories, storyGroups, newspaperConfig, contentPicks, config)
		outputPath = filepath.Join(wd.Dir, "digest.html")
	case "markdown", "md":
		content, err = CompileDigestMarkdown(wd.Posts, wd.Categories, storyGroups, newspaperConfig, contentPicks, config)
		outputPath = filepath.Join(wd.Dir, "digest.md")
	default:
		return "", fmt.Errorf("unsupported format %q: use html or markdown", format)
	}
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return "", err
	}

	return outputPath, nil
}
