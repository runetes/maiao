package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkdownFormatterSectionWithTitle(t *testing.T) {
	f := MarkdownBodyFormatter{}
	result := f.Section("Related changes", []string{"item one", "item two"})
	assert.Equal(t, []string{
		"###### Related changes",
		"- item one",
		"- item two",
	}, result)
}

func TestMarkdownFormatterSectionWithoutTitle(t *testing.T) {
	f := MarkdownBodyFormatter{}
	result := f.Section("", []string{"item one"})
	assert.Equal(t, []string{
		"- item one",
	}, result)
}

func TestMarkdownFormatterLink(t *testing.T) {
	f := MarkdownBodyFormatter{}
	assert.Equal(t, "[my link](https://example.com)", f.Link("https://example.com", "my link"))
}

func TestMarkdownFormatterLineBreak(t *testing.T) {
	f := MarkdownBodyFormatter{}
	assert.Equal(t, "", f.LineBreak())
}
