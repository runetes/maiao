package maiao

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"github.com/adevinta/maiao/pkg/api"
	lgit "github.com/adevinta/maiao/pkg/git"
)

func details(body []string, summary string) []string {
	r := []string{"<details>"}
	if summary != "" {
		r = append(r,
			"<summary>",
			summary,
			"</summary>",
		)
	}
	r = append(r, body...)
	r = append(r, "</details>")
	return r
}

func topicDetails(prAPI api.PullRequester, topic string) []string {
	sha := sha1.New()
	sha.Write([]byte("topic: "))
	sha.Write([]byte(topic))
	topicSha := fmt.Sprintf("%x", sha.Sum(nil))
	return details(
		[]string{
			"This change is part of a broader topic that can be in multiple repositories.",
			"<br/>",
			fmt.Sprintf(`Topic: <a href="%s" searchSha="%v">%s</a>`, prAPI.LinkedTopicIssues(topicSha), topicSha, topic),
		},
		"Broader related changes",
	)
}

func committerDetails(branch string) []string {
	return details([]string{"Local-Branch: " + branch}, "Committer details")
}

func changeDetails(changes []*change) []string {
	r := []string{}
	for _, change := range changes {
		t := change.message.Title
		if change.pr != nil {
			t = fmt.Sprintf("%s (#%s)", t, change.pr.ID)
		}
		r = append(r, details([]string{change.message.Body}, t)...)
	}
	return r
}

func relatedChanges(parents, futures []*change) []string {
	if len(parents) == 0 && len(futures) == 0 {
		return []string{}
	}
	content := []string{}
	if len(parents) > 0 {
		content = append(content, details(changeDetails(parents), "Parent changes")...)
	}
	if len(futures) > 0 {
		content = append(content, details(changeDetails(futures), "Future changes")...)
	}
	return details(content, "Related changes")
}

func prOptions(repo lgit.Repository, prAPI api.PullRequester, options ReviewOptions, change *change, parents, futures []*change) api.PullRequestOptions {
	base := options.Branch
	title := change.message.Title
	wip := options.WorkInProgress || strings.HasPrefix(title, "Draft: ") || strings.HasPrefix(title, "WIP: ")
	title = strings.TrimPrefix(title, "Draft: ")
	title = strings.TrimPrefix(title, "WIP: ")
	if change.parent != nil {
		if change.parent.branch != "" {
			base = change.parent.branch
		}
		if change.parent.pr != nil {
			title = fmt.Sprintf("[need #%s] %s", change.parent.pr.ID, title)
		}
	}
	additions := []string{}
	head, err := repo.Head()
	if err == nil {
		additions = committerDetails(head.Name().Short())
	}
	additions = append(
		additions,
		relatedChanges(parents, futures)...,
	)
	if options.Topic != "" {
		additions = append(additions, topicDetails(prAPI, options.Topic)...)
	}

	return api.PullRequestOptions{
		Base:  base,
		Head:  change.branch,
		Title: title,
		Body:  strings.Join(append([]string{change.message.Body}, additions...), "\n"),
		Ready: options.Ready,
		WIP:   wip,
	}
}
