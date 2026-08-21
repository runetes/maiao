package maiao

import (
	"testing"

	"github.com/adevinta/maiao/pkg/api"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/stretchr/testify/assert"
)

func TestDetailsSkipsMissingSummaries(t *testing.T) {
	assert.Equal(t,
		[]string{
			"<details>",
			"hello world",
			"</details>",
		},
		details(
			[]string{
				"hello world",
			},
			"",
		),
	)
}

func TestDetailsIncludesSummary(t *testing.T) {
	assert.Equal(t,
		[]string{
			"<details>",
			"<summary>",
			"summary",
			"</summary>",
			"hello world",
			"</details>",
		},
		details(
			[]string{
				"hello world",
			},
			"summary",
		),
	)
}

func TestTopicDetailsProvidesLink(t *testing.T) {
	assert.Equal(t,
		[]string{
			"<details>",
			"<summary>",
			"Broader related changes",
			"</summary>",
			"This change is part of a broader topic that can be in multiple repositories.",
			"<br/>",
			`Topic: <a href="https://search.example.com/topic" searchSha="89889b28e9672bff47fa4286f4aff4a80e09eade">some topic</a>`,
			"</details>",
		},
		topicDetails(&linkedTopicIssuesFunc{
			linkedTopicIssuesFunc: func(topicSearchString string) string {
				assert.Equal(t, "89889b28e9672bff47fa4286f4aff4a80e09eade", topicSearchString)
				return "https://search.example.com/topic"
			},
		}, "some topic"),
	)
}

type linkedTopicIssuesFunc struct {
	api.PullRequester
	linkedTopicIssuesFunc func(topicSearchString string) string
}

func (l linkedTopicIssuesFunc) LinkedTopicIssues(topicSearchString string) string {
	return l.linkedTopicIssuesFunc(topicSearchString)
}

func TestPrOptionsTitleStripsWIPBeforeNeedPrefix(t *testing.T) {
	prAPI := &linkedTopicIssuesFunc{
		linkedTopicIssuesFunc: func(s string) string { return "" },
	}
	repo := &testRepository{}

	parentChange := &change{
		message: &lgit.Message{Title: "Parent commit"},
		branch:  "maiao.parent",
		pr:      &api.PullRequest{ID: "6", URL: "http://example.com/6"},
	}
	childChange := &change{
		message: &lgit.Message{Title: "WIP: My Feature", Body: "description"},
		branch:  "maiao.child",
		parent:  parentChange,
	}

	opts := prOptions(repo, prAPI, ReviewOptions{Branch: "main", WorkInProgress: true}, childChange, []*change{parentChange}, []*change{})
	assert.Equal(t, "[need #6] My Feature", opts.Title)
	assert.True(t, opts.WIP)
}

func TestPrOptionsWIPDetectedFromTitlePrefix(t *testing.T) {
	prAPI := &linkedTopicIssuesFunc{
		linkedTopicIssuesFunc: func(s string) string { return "" },
	}
	repo := &testRepository{}

	childChange := &change{
		message: &lgit.Message{Title: "WIP: My Feature", Body: "description"},
		branch:  "maiao.child",
	}

	opts := prOptions(repo, prAPI, ReviewOptions{Branch: "main", WorkInProgress: false}, childChange, []*change{}, []*change{})
	assert.Equal(t, "My Feature", opts.Title)
	assert.True(t, opts.WIP, "WIP should be true when commit title starts with 'WIP: '")
}

func TestPrOptionsDraftDetectedFromTitlePrefix(t *testing.T) {
	prAPI := &linkedTopicIssuesFunc{
		linkedTopicIssuesFunc: func(s string) string { return "" },
	}
	repo := &testRepository{}

	childChange := &change{
		message: &lgit.Message{Title: "Draft: My Feature", Body: "description"},
		branch:  "maiao.child",
	}

	opts := prOptions(repo, prAPI, ReviewOptions{Branch: "main", WorkInProgress: false}, childChange, []*change{}, []*change{})
	assert.Equal(t, "My Feature", opts.Title)
	assert.True(t, opts.WIP, "WIP should be true when commit title starts with 'Draft: '")
}

func TestPrOptionsTitleStripsDraftBeforeNeedPrefix(t *testing.T) {
	prAPI := &linkedTopicIssuesFunc{
		linkedTopicIssuesFunc: func(s string) string { return "" },
	}
	repo := &testRepository{}

	parentChange := &change{
		message: &lgit.Message{Title: "Parent commit"},
		branch:  "maiao.parent",
		pr:      &api.PullRequest{ID: "3", URL: "http://example.com/3"},
	}
	childChange := &change{
		message: &lgit.Message{Title: "Draft: My Feature", Body: "description"},
		branch:  "maiao.child",
		parent:  parentChange,
	}

	opts := prOptions(repo, prAPI, ReviewOptions{Branch: "main"}, childChange, []*change{parentChange}, []*change{})
	assert.Equal(t, "[need #3] My Feature", opts.Title)
	assert.True(t, opts.WIP, "WIP should be true when commit title starts with 'Draft: '")
}

func TestPrOptionsTitleWithoutPrefixUnchanged(t *testing.T) {
	prAPI := &linkedTopicIssuesFunc{
		linkedTopicIssuesFunc: func(s string) string { return "" },
	}
	repo := &testRepository{}

	childChange := &change{
		message: &lgit.Message{Title: "My Feature", Body: "description"},
		branch:  "maiao.child",
	}

	opts := prOptions(repo, prAPI, ReviewOptions{Branch: "main"}, childChange, []*change{}, []*change{})
	assert.Equal(t, "My Feature", opts.Title)
}
