package bitbucket

import (
	"fmt"
	"strings"
)

// MarkdownBodyFormatter renders sections as h6 headings with bullet points.
type MarkdownBodyFormatter struct{}

func (MarkdownBodyFormatter) Section(title string, content []string) []string {
	r := []string{}
	if title != "" {
		r = append(r, "###### "+title)
	}
	for _, line := range content {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") {
			r = append(r, line)
		} else {
			r = append(r, "- "+line)
		}
	}
	return r
}

func (MarkdownBodyFormatter) Link(url, text string) string {
	return fmt.Sprintf("[%s|%s]", text, url)
}

func (MarkdownBodyFormatter) LineBreak() string {
	return ""
}
