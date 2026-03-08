package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func validateStoryGroupsForCompile(storyGroups StoryGroups) error {
	var unprocessed []string
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
	}
	if len(unprocessed) > 0 {
		sort.Strings(unprocessed)
		return fmt.Errorf("compile blocked: %d stories are unprocessed\n%s\n\nRun `./bin/digest show-unprocessed` for details",
			len(unprocessed), joinStrings(unprocessed, "\n"))
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
