package maiao

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"github.com/adevinta/maiao/pkg/api"
	lgit "github.com/adevinta/maiao/pkg/git"
)

func topicDetails(f api.BodyFormatter, prAPI api.PullRequester, topic string) []string {
	sha := sha1.New()
	sha.Write([]byte("topic: "))
	sha.Write([]byte(topic))
	topicSha := fmt.Sprintf("%x", sha.Sum(nil))
	return f.Section(
		"Broader related changes",
		[]string{
			"This change is part of a broader topic that can be in multiple repositories.",
			f.LineBreak(),
			fmt.Sprintf("Topic: %s", f.Link(prAPI.LinkedTopicIssues(topicSha), topic)),
		},
	)
}

func committerDetails(f api.BodyFormatter, branch string) []string {
	return f.Section("Committer details", []string{"Local-Branch: " + branch})
}

func changeDetails(f api.BodyFormatter, changes []*change) []string {
	r := []string{}
	for _, change := range changes {
		t := change.message.Title
		if change.pr != nil {
			t = fmt.Sprintf("%s (#%s)", t, change.pr.ID)
		}
		r = append(r, f.Section(t, []string{change.message.Body})...)
	}
	return r
}

func relatedChanges(f api.BodyFormatter, parents, futures []*change) []string {
	if len(parents) == 0 && len(futures) == 0 {
		return []string{}
	}
	content := []string{}
	if len(parents) > 0 {
		content = append(content, f.Section("Parent changes", changeDetails(f, parents))...)
	}
	if len(futures) > 0 {
		content = append(content, f.Section("Future changes", changeDetails(f, futures))...)
	}
	return f.Section("Related changes", content)
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

	f := prAPI.BodyFormatter()
	additions := []string{}
	head, err := repo.Head()
	if err == nil {
		additions = committerDetails(f, head.Name().Short())
	}
	additions = append(
		additions,
		relatedChanges(f, parents, futures)...,
	)
	if options.Topic != "" {
		additions = append(additions, topicDetails(f, prAPI, options.Topic)...)
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
