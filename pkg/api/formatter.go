package api

// BodyFormatter controls how PR body sections (committer details, related changes, topics) are rendered.
type BodyFormatter interface {
	Section(title string, content []string) []string
	Link(url, text string) string
	LineBreak() string
}

// HTMLBodyFormatter renders sections as collapsible <details> blocks.
type HTMLBodyFormatter struct{}

func (HTMLBodyFormatter) Section(title string, content []string) []string {
	r := []string{"<details>"}
	if title != "" {
		r = append(r, "<summary>", title, "</summary>")
	}
	r = append(r, content...)
	r = append(r, "</details>")
	return r
}

func (HTMLBodyFormatter) Link(url, text string) string {
	return `<a href="` + url + `">` + text + `</a>`
}

func (HTMLBodyFormatter) LineBreak() string {
	return "<br/>"
}
