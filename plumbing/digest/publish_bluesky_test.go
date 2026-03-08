package main

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeBlueskyRichText_UsesUTF8ByteOffsets(t *testing.T) {
	text, facets := composeBlueskyRichText([]blueskyTextSegment{
		{Text: "cafe "},
		{Text: "cafe ", LinkURI: "https://example.com/plain"},
		{Text: "cafe "},
		{Text: "cafe "},
		{Text: "cafe "},
		{Text: "caf\u00e9 "},
		{Text: "@alice.bsky.social", MentionDID: "did:plc:alice"},
		{Text: " "},
		{Text: "example.com/story", LinkURI: "https://example.com/story"},
	})

	require.Equal(t, "cafe cafe cafe cafe cafe caf\u00e9 @alice.bsky.social example.com/story", text)
	require.Len(t, facets, 3)

	assert.Equal(t, "cafe ", facetSlice(text, facets[0]))
	assert.Equal(t, "https://example.com/plain", facets[0].Features[0].RichtextFacet_Link.Uri)

	assert.Equal(t, "@alice.bsky.social", facetSlice(text, facets[1]))
	assert.Equal(t, "did:plc:alice", facets[1].Features[0].RichtextFacet_Mention.Did)

	assert.Equal(t, "example.com/story", facetSlice(text, facets[2]))
	assert.Equal(t, "https://example.com/story", facets[2].Features[0].RichtextFacet_Link.Uri)
}

func TestBuildBlueskyPublishPlan_GeneratesThreadedDryRun(t *testing.T) {
	createdAt := time.Date(2026, time.March, 7, 9, 30, 0, 0, time.UTC)
	wd := &WorkspaceData{
		Dir: "digest-07-03-2026",
		Config: Config{
			CreatedAt: createdAt,
		},
		Posts: []Post{
			{
				Rkey: "r1",
				URI:  "at://did:plc:alice/app.bsky.feed.post/r1",
				CID:  "bafy-primary",
				Text: "AT Proto tooling keeps improving quickly.",
				Author: Author{
					DID:    "did:plc:alice",
					Handle: "alice.bsky.social",
				},
				ExternalLink: &ExternalLink{
					URL: "https://example.com/atproto/tooling",
				},
			},
		},
	}
	newspaperConfig := NewspaperConfig{
		Sections: []NewspaperSection{
			{ID: "front-page", Name: "Front Page", Type: "news"},
			{ID: "tech", Name: "Technology", Type: "news"},
		},
	}
	storyGroups := StoryGroups{
		"sg_001": {
			ID:              "sg_001",
			SectionID:       "tech",
			PrimaryRkey:     "r1",
			PostRkeys:       []string{"r1"},
			Headline:        "AT Proto publishing comes into focus",
			Summary:         "A short summary with enough detail for a thread post.",
			ArticleURL:      "https://example.com/atproto/tooling",
			OriginalSection: "tech",
			Priority:        1,
		},
	}

	plan, err := buildBlueskyPublishPlan(wd, newspaperConfig, storyGroups, "digest.bsky.social")
	require.NoError(t, err)

	require.Len(t, plan.Posts, 3)
	assert.Equal(t, "issue", plan.Posts[0].ID)
	assert.Equal(t, "section-tech", plan.Posts[1].ID)
	assert.Equal(t, "story-sg_001", plan.Posts[2].ID)

	storyPost := plan.Posts[2]
	require.NotNil(t, storyPost.Reply)
	assert.Equal(t, "section-tech", storyPost.Reply.ParentID)
	assert.Equal(t, "issue", storyPost.Reply.RootID)
	assert.Equal(t, []string{"r1"}, storyPost.SourceRkeys)
	assert.Equal(t, blueskyPublishCollection, storyPost.CreateRecord.Collection)
	assert.Equal(t, "digest.bsky.social", storyPost.CreateRecord.Repo)

	record := storyPost.CreateRecord.Record
	require.NotNil(t, record)
	assert.Equal(t, "app.bsky.feed.post", record.LexiconTypeID)
	require.Len(t, record.Facets, 2)
	assert.Equal(t, "@alice.bsky.social", facetSlice(record.Text, record.Facets[0]))
	assert.Equal(t, "did:plc:alice", record.Facets[0].Features[0].RichtextFacet_Mention.Did)
	assert.Equal(t, "example.com/atproto/tooling", facetSlice(record.Text, record.Facets[1]))
	assert.Equal(t, "https://example.com/atproto/tooling", record.Facets[1].Features[0].RichtextFacet_Link.Uri)
	require.NotNil(t, record.Embed)
	require.NotNil(t, record.Embed.EmbedRecord)
	assert.Equal(t, "at://did:plc:alice/app.bsky.feed.post/r1", record.Embed.EmbedRecord.Record.Uri)
	assert.Equal(t, "bafy-primary", record.Embed.EmbedRecord.Record.Cid)
	assert.LessOrEqual(t, len(record.Text), maxBlueskyPostBytes)
}

func facetSlice(text string, facet *bsky.RichtextFacet) string {
	start := int(facet.Index.ByteStart)
	end := int(facet.Index.ByteEnd)
	return string([]byte(text)[start:end])
}
