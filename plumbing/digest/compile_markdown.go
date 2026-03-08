package main

import (
	"fmt"
	"strings"
)

// CompileDigestMarkdown generates a portable markdown edition of the digest.
func CompileDigestMarkdown(
	posts []Post,
	_ Categories,
	storyGroups StoryGroups,
	newspaperConfig NewspaperConfig,
	_ AllContentPicks,
	config Config,
) (string, error) {
	postIndex := make(map[string]Post, len(posts))
	for _, post := range posts {
		postIndex[post.Rkey] = post
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# The New Intelligencer\n\n")
	fmt.Fprintf(&b, "_%s_\n", formatDigestDate(config.CreatedAt))

	for _, section := range orderedMarkdownSections(newspaperConfig) {
		groups := getSectionGroups(storyGroups, section.ID)
		if section.MaxStories > 0 {
			truncateStories(&groups, section.MaxStories)
		}
		if groups.Headline == nil && len(groups.Stories) == 0 && len(groups.Opinions) == 0 {
			continue
		}

		fmt.Fprintf(&b, "\n\n## %s\n", section.Name)
		writeMarkdownStories(&b, groups, postIndex)
	}

	return strings.TrimSpace(b.String()) + "\n", nil
}

func orderedMarkdownSections(newspaperConfig NewspaperConfig) []NewspaperSection {
	sections := make([]NewspaperSection, 0, len(newspaperConfig.Sections))
	for _, section := range newspaperConfig.Sections {
		if section.ID == "front-page" {
			sections = append(sections, section)
		}
	}
	for _, section := range newspaperConfig.Sections {
		if section.ID != "front-page" {
			sections = append(sections, section)
		}
	}
	return sections
}

func writeMarkdownStories(b *strings.Builder, groups GroupedStories, postIndex map[string]Post) {
	if groups.Headline != nil {
		writeMarkdownStory(b, groups.Headline, postIndex, true)
	}
	for _, story := range groups.Stories {
		writeMarkdownStory(b, story, postIndex, false)
	}
	for _, story := range groups.Opinions {
		writeMarkdownStory(b, story, postIndex, false)
	}
}

func writeMarkdownStory(b *strings.Builder, story *StoryGroup, postIndex map[string]Post, isSectionHeadline bool) {
	primary, ok := postIndex[story.PrimaryRkey]
	if !ok && len(story.PostRkeys) > 0 {
		primary, ok = postIndex[story.PostRkeys[0]]
	}
	if !ok {
		return
	}

	title := getHeadline(story, primary)
	if story.IsOpinion || story.Role == "opinion" {
		title = "Opinion: " + title
	}

	if isSectionHeadline {
		fmt.Fprintf(b, "\n### %s\n", title)
	} else {
		fmt.Fprintf(b, "\n### %s\n", title)
	}

	if story.Summary != "" {
		fmt.Fprintf(b, "\n%s\n", story.Summary)
	}
	if story.ArticleURL != "" {
		fmt.Fprintf(b, "\nSource: %s\n", story.ArticleURL)
	}

	fmt.Fprintf(b, "\nPriority: %d", story.Priority)
	if story.Role != "" {
		fmt.Fprintf(b, " | Role: %s", story.Role)
	}
	fmt.Fprintf(b, " | Posts: %d\n", len(story.PostRkeys))

	for _, rkey := range story.PostRkeys {
		post, ok := postIndex[rkey]
		if !ok {
			continue
		}
		writeMarkdownPost(b, post)
	}
}

func writeMarkdownPost(b *strings.Builder, post Post) {
	text := strings.TrimSpace(post.Text)
	if text == "" {
		text = "(no text)"
	}

	fmt.Fprintf(b, "\n- @%s", post.Author.Handle)
	if post.Author.DisplayName != "" {
		fmt.Fprintf(b, " (%s)", post.Author.DisplayName)
	}
	fmt.Fprintf(b, " · %s\n", formatPostTime(post.CreatedAt))
	fmt.Fprintf(b, "  %s\n", squashWhitespace(text))
	fmt.Fprintf(b, "  %s\n", postURL(post))

	if post.ExternalLink != nil {
		fmt.Fprintf(b, "  Link: [%s](%s)\n", markdownLinkLabel(post.ExternalLink.Title, post.ExternalLink.URL), post.ExternalLink.URL)
	}
	if post.Quote != nil {
		fmt.Fprintf(b, "  Quote: @%s - %s\n", post.Quote.Author.Handle, squashWhitespace(post.Quote.Text))
	}
	if len(post.Images) > 0 {
		fmt.Fprintf(b, "  Images: %d attachment(s)\n", len(post.Images))
	}
}

func squashWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func markdownLinkLabel(title string, fallback string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return strings.ReplaceAll(title, "]", "\\]")
	}
	return fallback
}
