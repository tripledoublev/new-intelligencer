package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileDigestMarkdown_ReturnsNonEmpty(t *testing.T) {
	posts := []Post{{Rkey: "test1", Text: "Test post", Author: Author{Handle: "test.bsky.social"}}}
	cats := Categories{"tech": {Visible: []string{"test1"}}}
	storyGroups := StoryGroups{
		"sg1": {ID: "sg1", SectionID: "tech", PrimaryRkey: "test1", Headline: "Test Story", Priority: 1},
	}
	newspaperConfig := NewspaperConfig{
		Sections: []NewspaperSection{{ID: "tech", Name: "Technology"}},
	}
	contentPicks := AllContentPicks{}
	config := Config{CreatedAt: time.Now()}

	result, err := CompileDigestMarkdown(posts, cats, storyGroups, newspaperConfig, contentPicks, config)

	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "# The New Intelligencer")
	assert.Contains(t, result, "## Technology")
	assert.Contains(t, result, "### Test Story")
}

func TestCompileDigestMarkdown_IncludesFixtureContent(t *testing.T) {
	posts, err := LoadPosts("testdata/fixtures/posts.json")
	require.NoError(t, err)

	cats, err := LoadCategories("testdata/fixtures/categories.json")
	require.NoError(t, err)

	storyGroups, err := LoadStoryGroups("testdata/fixtures/story-groups.json")
	require.NoError(t, err)

	newspaperConfig, err := LoadNewspaperConfig("testdata/fixtures/newspaper.json")
	require.NoError(t, err)

	config, err := loadConfig("testdata/fixtures/config.json")
	require.NoError(t, err)

	result, err := CompileDigestMarkdown(posts, cats, storyGroups, newspaperConfig, AllContentPicks{}, config)
	require.NoError(t, err)

	assert.Contains(t, result, "## Front Page")
	assert.Contains(t, result, "## Technology")
	assert.Contains(t, result, "- @")
	assert.True(t, strings.HasSuffix(result, "\n"))
}

func TestCompileDigestMarkdown_StripsInternalMetadata(t *testing.T) {
	posts := []Post{{
		Rkey:      "test1",
		Text:      "Test post",
		Author:    Author{Handle: "test.bsky.social"},
		CreatedAt: time.Now(),
		Images:    []Image{{URL: "https://example.com/test.jpg"}},
	}}
	storyGroups := StoryGroups{
		"sg1": {
			ID:          "sg1",
			SectionID:   "tech",
			PrimaryRkey: "test1",
			Headline:    "Test Story",
			Priority:    4,
			Role:        "featured",
			PostRkeys:   []string{"test1"},
		},
	}
	newspaperConfig := NewspaperConfig{
		Sections: []NewspaperSection{{ID: "tech", Name: "Technology"}},
	}

	result, err := CompileDigestMarkdown(posts, Categories{}, storyGroups, newspaperConfig, AllContentPicks{}, Config{CreatedAt: time.Now()})
	require.NoError(t, err)
	assert.NotContains(t, result, "Priority:")
	assert.NotContains(t, result, " | Role:")
	assert.NotContains(t, result, " | Posts:")
	assert.NotContains(t, result, "Images:")
}
