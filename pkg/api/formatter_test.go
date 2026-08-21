package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTMLFormatterSectionWithTitle(t *testing.T) {
	f := HTMLBodyFormatter{}
	result := f.Section("summary", []string{"hello world"})
	assert.Equal(t, []string{
		"<details>",
		"<summary>",
		"summary",
		"</summary>",
		"hello world",
		"</details>",
	}, result)
}

func TestHTMLFormatterSectionWithoutTitle(t *testing.T) {
	f := HTMLBodyFormatter{}
	result := f.Section("", []string{"hello world"})
	assert.Equal(t, []string{
		"<details>",
		"hello world",
		"</details>",
	}, result)
}

func TestHTMLFormatterLink(t *testing.T) {
	f := HTMLBodyFormatter{}
	assert.Equal(t, `<a href="https://example.com">my link</a>`, f.Link("https://example.com", "my link"))
}

func TestHTMLFormatterLineBreak(t *testing.T) {
	f := HTMLBodyFormatter{}
	assert.Equal(t, "<br/>", f.LineBreak())
}
