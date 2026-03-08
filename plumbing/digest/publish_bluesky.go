package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/spf13/cobra"
)

const (
	blueskyPublishPlanVersion = "1"
	blueskyPublishCollection  = "app.bsky.feed.post"
	maxBlueskyPostBytes       = 300
	maxBlueskyHeadlineBytes   = 80
	maxBlueskySummaryBytes    = 140
	maxBlueskyURLTextBytes    = 48
)

type BlueskyPublishPlan struct {
	Version         string               `json:"version"`
	CreatedAt       time.Time            `json:"created_at"`
	WorkspaceDir    string               `json:"workspace_dir"`
	Repo            string               `json:"repo,omitempty"`
	DryRun          bool                 `json:"dry_run"`
	SourcePostCount int                  `json:"source_post_count"`
	SectionCount    int                  `json:"section_count"`
	StoryCount      int                  `json:"story_count"`
	Posts           []PlannedBlueskyPost `json:"posts"`
}

type PlannedBlueskyPost struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	SectionID    string              `json:"section_id,omitempty"`
	SectionName  string              `json:"section_name,omitempty"`
	StoryID      string              `json:"story_id,omitempty"`
	Reply        *PlannedReplyRef    `json:"reply,omitempty"`
	SourceRkeys  []string            `json:"source_rkeys,omitempty"`
	CreateRecord PlannedCreateRecord `json:"create_record"`
}

type PlannedReplyRef struct {
	ParentID string `json:"parent_id"`
	RootID   string `json:"root_id"`
}

type PlannedCreateRecord struct {
	Collection string         `json:"collection"`
	Repo       string         `json:"repo,omitempty"`
	Record     *bsky.FeedPost `json:"record"`
}

type blueskyTextSegment struct {
	Text       string
	MentionDID string
	LinkURI    string
}

var publishBlueskyCmd = &cobra.Command{
	Use:   "publish-bluesky",
	Short: "Generate a dry-run Bluesky publishing plan from a workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !dryRun {
			return fmt.Errorf("live Bluesky publishing is not implemented yet; use --dry-run")
		}

		dir, err := GetWorkspaceDir()
		if err != nil {
			return err
		}

		wd, err := LoadWorkspace(dir)
		if err != nil {
			return err
		}

		newspaperConfig, err := loadProjectNewspaperConfig(dir)
		if err != nil {
			return err
		}

		storyGroups, err := LoadStoryGroups(filepath.Join(dir, "story-groups.json"))
		if err != nil {
			return err
		}
		if len(storyGroups) == 0 {
			return fmt.Errorf("no story groups found in %s; run the editorial pipeline first", dir)
		}

		plan, err := buildBlueskyPublishPlan(wd, newspaperConfig, storyGroups, os.Getenv("BSKY_HANDLE"))
		if err != nil {
			return err
		}

		planPath := filepath.Join(dir, "publish-plan-bluesky.json")
		if err := saveBlueskyPublishPlan(planPath, plan); err != nil {
			return err
		}

		fmt.Printf("Wrote Bluesky dry-run plan to %s (%d planned posts)\n", planPath, len(plan.Posts))
		return nil
	},
}

func init() {
	publishBlueskyCmd.Flags().Bool("dry-run", true, "Generate a Bluesky publish plan without creating records")
}

func buildBlueskyPublishPlan(wd *WorkspaceData, newspaperConfig NewspaperConfig, storyGroups StoryGroups, repo string) (BlueskyPublishPlan, error) {
	if wd == nil {
		return BlueskyPublishPlan{}, fmt.Errorf("workspace is required")
	}

	postIndex := make(map[string]Post, len(wd.Posts))
	for _, post := range wd.Posts {
		postIndex[post.Rkey] = post
	}

	publishedAt := time.Now().UTC()
	digestDate := wd.Config.CreatedAt
	if digestDate.IsZero() {
		digestDate = publishedAt
	}

	sections := sectionsWithStoriesOrdered(storyGroups, newspaperConfig)
	var plannedPosts []PlannedBlueskyPost
	sectionCount := 0
	storyCount := 0

	for _, section := range sections {
		stories := orderedStoriesForSection(storyGroups, section.ID)
		if len(stories) == 0 {
			continue
		}

		sectionID := "section-" + section.ID
		var storyPosts []PlannedBlueskyPost
		for _, story := range stories {
			primary, ok := storyPrimaryPost(*story, postIndex)
			if !ok {
				continue
			}

			record := buildBlueskyStoryRecord(publishedAt, *story, primary)
			storyPosts = append(storyPosts, PlannedBlueskyPost{
				ID:          "story-" + story.ID,
				Kind:        "story",
				SectionID:   section.ID,
				SectionName: section.Name,
				StoryID:     story.ID,
				Reply: &PlannedReplyRef{
					ParentID: sectionID,
					RootID:   "issue",
				},
				SourceRkeys: append([]string(nil), story.PostRkeys...),
				CreateRecord: PlannedCreateRecord{
					Collection: blueskyPublishCollection,
					Repo:       repo,
					Record:     record,
				},
			})
		}
		if len(storyPosts) == 0 {
			continue
		}

		plannedPosts = append(plannedPosts, PlannedBlueskyPost{
			ID:          sectionID,
			Kind:        "section",
			SectionID:   section.ID,
			SectionName: section.Name,
			Reply: &PlannedReplyRef{
				ParentID: "issue",
				RootID:   "issue",
			},
			CreateRecord: PlannedCreateRecord{
				Collection: blueskyPublishCollection,
				Repo:       repo,
				Record:     buildBlueskySectionRecord(publishedAt, section, len(storyPosts)),
			},
		})
		plannedPosts = append(plannedPosts, storyPosts...)
		sectionCount++
		storyCount += len(storyPosts)
	}

	if storyCount == 0 {
		return BlueskyPublishPlan{}, fmt.Errorf("no publishable stories found in %s", wd.Dir)
	}

	root := PlannedBlueskyPost{
		ID:   "issue",
		Kind: "issue",
		CreateRecord: PlannedCreateRecord{
			Collection: blueskyPublishCollection,
			Repo:       repo,
			Record:     buildBlueskyIssueRecord(publishedAt, digestDate, sectionCount, storyCount, len(wd.Posts)),
		},
	}

	plan := BlueskyPublishPlan{
		Version:         blueskyPublishPlanVersion,
		CreatedAt:       publishedAt,
		WorkspaceDir:    wd.Dir,
		Repo:            repo,
		DryRun:          true,
		SourcePostCount: len(wd.Posts),
		SectionCount:    sectionCount,
		StoryCount:      storyCount,
		Posts:           append([]PlannedBlueskyPost{root}, plannedPosts...),
	}
	return plan, nil
}

func orderedStoriesForSection(storyGroups StoryGroups, sectionID string) []*StoryGroup {
	var groups GroupedStories
	if sectionID == "front-page" {
		groups = getFrontPageGroups(storyGroups)
	} else {
		groups = getSectionGroups(storyGroups, sectionID)
	}

	stories := make([]*StoryGroup, 0, len(groups.Stories)+len(groups.Opinions)+1)
	if groups.Headline != nil {
		stories = append(stories, groups.Headline)
	}
	stories = append(stories, groups.Stories...)
	stories = append(stories, groups.Opinions...)
	return stories
}

func buildBlueskyIssueRecord(createdAt, digestDate time.Time, sectionCount, storyCount, sourcePostCount int) *bsky.FeedPost {
	text := fmt.Sprintf(
		"The New Intelligencer\n%s\n%d sections, %d stories from %d source posts. Thread below.",
		formatDigestDate(digestDate),
		sectionCount,
		storyCount,
		sourcePostCount,
	)

	return &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		CreatedAt:     createdAt.Format(time.RFC3339),
		Text:          truncateUTF8BytesWithEllipsis(text, maxBlueskyPostBytes),
	}
}

func buildBlueskySectionRecord(createdAt time.Time, section NewspaperSection, storyCount int) *bsky.FeedPost {
	text := fmt.Sprintf("%s\n%d stories in this section.", section.Name, storyCount)
	if section.ID == "front-page" {
		text = fmt.Sprintf("%s\n%d stories selected for this issue.", section.Name, storyCount)
	}

	return &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		CreatedAt:     createdAt.Format(time.RFC3339),
		Text:          truncateUTF8BytesWithEllipsis(text, maxBlueskyPostBytes),
	}
}

func buildBlueskyStoryRecord(createdAt time.Time, story StoryGroup, primary Post) *bsky.FeedPost {
	headline := getHeadline(&story, primary)
	if story.IsOpinion || story.Role == "opinion" {
		headline = "Opinion: " + headline
	}
	headline = truncateUTF8BytesWithEllipsis(strings.TrimSpace(headline), maxBlueskyHeadlineBytes)

	summary := strings.TrimSpace(story.Summary)
	if summary == "" {
		summary = strings.TrimSpace(primary.Text)
	}
	summary = squashWhitespace(summary)

	linkURI := strings.TrimSpace(story.ArticleURL)
	linkLabel := "Source: "
	if linkURI == "" {
		linkURI = postURL(primary)
		linkLabel = "Post: "
	}
	linkText := compactURLText(linkURI, maxBlueskyURLTextBytes)

	mentionText := "@" + primary.Author.Handle
	mentionDID := primary.Author.DID

	baseSegments := []blueskyTextSegment{
		{Text: headline},
		{Text: "\n"},
		{Text: "Via "},
		{Text: mentionText, MentionDID: mentionDID},
		{Text: "\n"},
		{Text: linkLabel},
		{Text: linkText, LinkURI: linkURI},
	}

	remainingSummaryBytes := maxBlueskyPostBytes - richTextSegmentsByteLen(baseSegments)
	if remainingSummaryBytes > 1 && summary != "" {
		summaryBudget := remainingSummaryBytes - 1
		if summaryBudget > maxBlueskySummaryBytes {
			summaryBudget = maxBlueskySummaryBytes
		}
		summary = truncateUTF8BytesWithEllipsis(summary, summaryBudget)
		if summary != "" {
			baseSegments = append(
				[]blueskyTextSegment{
					{Text: headline},
					{Text: "\n"},
					{Text: summary},
					{Text: "\n"},
				},
				baseSegments[2:]...,
			)
		}
	}

	text, facets := composeBlueskyRichText(baseSegments)
	record := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		CreatedAt:     createdAt.Format(time.RFC3339),
		Text:          text,
		Facets:        facets,
	}

	if primary.URI != "" && primary.CID != "" {
		record.Embed = &bsky.FeedPost_Embed{
			EmbedRecord: &bsky.EmbedRecord{
				LexiconTypeID: "app.bsky.embed.record",
				Record: &atproto.RepoStrongRef{
					Uri: primary.URI,
					Cid: primary.CID,
				},
			},
		}
	}

	return record
}

func composeBlueskyRichText(segments []blueskyTextSegment) (string, []*bsky.RichtextFacet) {
	var b strings.Builder
	var facets []*bsky.RichtextFacet
	var byteOffset int64

	for _, segment := range segments {
		if segment.Text == "" {
			continue
		}

		start := byteOffset
		b.WriteString(segment.Text)
		byteOffset += int64(len(segment.Text))

		var features []*bsky.RichtextFacet_Features_Elem
		if segment.MentionDID != "" {
			features = append(features, &bsky.RichtextFacet_Features_Elem{
				RichtextFacet_Mention: &bsky.RichtextFacet_Mention{Did: segment.MentionDID},
			})
		}
		if segment.LinkURI != "" {
			features = append(features, &bsky.RichtextFacet_Features_Elem{
				RichtextFacet_Link: &bsky.RichtextFacet_Link{Uri: segment.LinkURI},
			})
		}
		if len(features) == 0 {
			continue
		}

		facets = append(facets, &bsky.RichtextFacet{
			Features: features,
			Index: &bsky.RichtextFacet_ByteSlice{
				ByteStart: start,
				ByteEnd:   byteOffset,
			},
		})
	}

	return b.String(), facets
}

func richTextSegmentsByteLen(segments []blueskyTextSegment) int {
	total := 0
	for _, segment := range segments {
		total += len(segment.Text)
	}
	return total
}

func compactURLText(raw string, maxBytes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	display := raw
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		display = parsed.Host
		if parsed.Path != "" && parsed.Path != "/" {
			display += parsed.EscapedPath()
		}
		if parsed.RawQuery != "" {
			display += "?" + parsed.RawQuery
		}
	}

	return truncateUTF8BytesWithEllipsis(display, maxBytes)
}

func truncateUTF8BytesWithEllipsis(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	if maxBytes <= 3 {
		return truncateUTF8Bytes(text, maxBytes)
	}
	return truncateUTF8Bytes(text, maxBytes-3) + "..."
}

func truncateUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}

	var b strings.Builder
	byteLen := 0
	for _, r := range text {
		runeLen := len(string(r))
		if byteLen+runeLen > maxBytes {
			break
		}
		b.WriteRune(r)
		byteLen += runeLen
	}
	return b.String()
}

func saveBlueskyPublishPlan(path string, plan BlueskyPublishPlan) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling Bluesky publish plan: %w", err)
	}

	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tempFile, path); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
