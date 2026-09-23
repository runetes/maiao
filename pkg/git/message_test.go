package git

import (
	"testing"

	"github.com/runetes/maiao/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestCommit(t *testing.T) {
	commitMessage := `This is the commit Title

And the body


with multiple lines

Header : bla

`

	m := Parse(commitMessage)
	assert.Equal(t, "This is the commit Title", m.Title)
	assert.Equal(t, "And the body\n\n\nwith multiple lines", m.Body)
	assert.Equal(t, map[string]string{"Header": "bla"}, m.Headers)
}

func TestCommitHeaders(t *testing.T) {
	commitMessage := `This is the commit Title

https://github.com
Header : bla
Change-Id: Iabcd
`

	m := Parse(commitMessage)
	assert.Equal(t, "This is the commit Title", m.Title)
	assert.Equal(t, "https://github.com", m.Body)
	assert.Equal(t, map[string]string{"Header": "bla", "Change-Id": "Iabcd"}, m.Headers)
}

func TestCommitSupportWindowsStyle(t *testing.T) {
	m := Parse("This is the commit Title\r\n\r\nAnd the body\r\nwith multiple lines\r\nHeader : bla\r\n")
	assert.Equal(t, "This is the commit Title", m.Title)
	assert.Equal(t, "And the body\nwith multiple lines", m.Body)
	assert.Equal(t, map[string]string{"Header": "bla"}, m.Headers)
}

func TestToString(t *testing.T) {
	testToString(t, `Hello world commit`)
	testToString(t, `Hello world commit\n\nbody`)
	testToString(t, `This is the commit Title

And the body


with multiple lines

Header:bla`)
}

func TestGetExistingChangeID(t *testing.T) {
	testChangeID(t, &Message{Headers: map[string]string{"Change-Id": "09123"}}, "09123", true)
}
func TestGetNonExistingChangeID(t *testing.T) {
	testChangeID(t, &Message{}, "", false)
	testChangeID(t, &Message{Headers: map[string]string{}}, "", false)
}

func testChangeID(t *testing.T, m *Message, changeID string, found bool) {
	c, ok := m.GetChangeID()
	assert.Equal(t, changeID, c)
	assert.Equal(t, found, ok)
}

func testToString(t *testing.T, message string) {
	assert.Equal(t, Parse(message).String(), message)
}

// get all logs when running tests
func init() {
	log.Logger.SetLevel(logrus.DebugLevel)
}
