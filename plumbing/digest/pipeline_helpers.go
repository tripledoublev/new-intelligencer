package main

import (
	"fmt"
	"path/filepath"
	"sort"
)

func categorizationUnitCount(wd *WorkspaceData) int {
	if wd == nil {
		return 0
	}
	if wd.Config.Pipeline.Provider == "ollama" {
		return len(wd.GetThreadRoots())
	}
	return len(wd.Posts)
}

func nextStoryGroupID(storyGroups StoryGroups) string {
	nextNum := len(storyGroups) + 1
	id := fmt.Sprintf("sg_%03d", nextNum)
	for {
		if _, exists := storyGroups[id]; !exists {
			return id
		}
		nextNum++
		id = fmt.Sprintf("sg_%03d", nextNum)
	}
}

func loadProjectNewspaperConfig(workspaceDir string) (NewspaperConfig, error) {
	return LoadNewspaperConfig(filepath.Join(filepath.Dir(workspaceDir), "newspaper.json"))
}

func storyPrimaryPost(story StoryGroup, postIndex map[string]Post) (Post, bool) {
	if post, ok := postIndex[story.PrimaryRkey]; ok {
		return post, true
	}
	for _, rkey := range story.PostRkeys {
		if post, ok := postIndex[rkey]; ok {
			return post, true
		}
	}
	return Post{}, false
}

func storyEngagementScore(story StoryGroup, postIndex map[string]Post) int64 {
	var score int64
	for _, rkey := range story.PostRkeys {
		post, ok := postIndex[rkey]
		if !ok {
			continue
		}
		score += post.LikeCount + post.ReplyCount + post.RepostCount + post.QuoteCount
	}
	return score
}

func moveStoryBetweenSectionsInDir(dir, storyID, toSection string) error {
	return withLock(dir, "categories.lock", func() error {
		storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
		if err != nil {
			return err
		}

		story, ok := storyGroups[storyID]
		if !ok {
			return fmt.Errorf("story not found: %s", storyID)
		}

		fromSection := story.SectionID
		if fromSection == toSection {
			return nil
		}

		cats, err := LoadCategories(filepath.Join(dir, "categories.json"))
		if err != nil {
			return err
		}

		for _, rkey := range story.PostRkeys {
			removeFromCategory(rkey, cats)
		}

		destCat := cats[toSection]
		destCat.Visible = append(destCat.Visible, story.PostRkeys...)
		cats[toSection] = destCat

		if story.OriginalSection == "" {
			story.OriginalSection = fromSection
		}
		if toSection != "front-page" {
			story.OriginalSection = ""
		}
		story.SectionID = toSection
		storyGroups[storyID] = story

		if err := SaveCategories(filepath.Join(dir, "categories.json"), cats); err != nil {
			return err
		}
		if err := SaveStoryGroups(filepath.Join(dir, "story-groups.json"), storyGroups); err != nil {
			return err
		}

		return nil
	})
}

func sectionMap(sections []NewspaperSection) map[string]NewspaperSection {
	result := make(map[string]NewspaperSection, len(sections))
	for _, section := range sections {
		result[section.ID] = section
	}
	return result
}

func newsSections(newspaperConfig NewspaperConfig) []NewspaperSection {
	var sections []NewspaperSection
	for _, section := range newspaperConfig.Sections {
		if section.Type == "news" && section.ID != "front-page" {
			sections = append(sections, section)
		}
	}
	return sections
}

func sectionsWithPostsOrdered(cats Categories, newspaperConfig NewspaperConfig) []NewspaperSection {
	var sections []NewspaperSection
	for _, section := range newspaperConfig.Sections {
		catData, ok := cats[section.ID]
		if !ok || section.ID == "front-page" {
			continue
		}
		if len(catData.Visible) == 0 && len(catData.Hidden) == 0 {
			continue
		}
		sections = append(sections, section)
	}
	return sections
}

func sectionsWithStoriesOrdered(storyGroups StoryGroups, newspaperConfig NewspaperConfig) []NewspaperSection {
	have := make(map[string]bool)
	for _, story := range storyGroups {
		have[story.SectionID] = true
	}

	var sections []NewspaperSection
	for _, section := range newspaperConfig.Sections {
		if have[section.ID] {
			sections = append(sections, section)
		}
	}
	return sections
}

func sortedStoryIDs(storyGroups StoryGroups, sectionID string) []string {
	var ids []string
	for id, story := range storyGroups {
		if story.SectionID == sectionID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
