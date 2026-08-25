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
		"",
		"- item one",
		"- item two",
	}, result)
}

func TestMarkdownFormatterSectionPreservesNestedHeadings(t *testing.T) {
	f := MarkdownBodyFormatter{}
	inner := f.Section("Parent changes", []string{"first", "second"})
	result := f.Section("Related changes", inner)
	assert.Equal(t, []string{
		"###### Related changes",
		"",
		"###### Parent changes",
		"",
		"- first",
		"- second",
	}, result)
}

func TestMarkdownFormatterSectionPreservesExistingBullets(t *testing.T) {
	f := MarkdownBodyFormatter{}
	result := f.Section("Items", []string{"- already a bullet", "plain text"})
	assert.Equal(t, []string{
		"###### Items",
		"",
		"- already a bullet",
		"- plain text",
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
