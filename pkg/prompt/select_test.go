package prompt

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectOutputContainsItems(t *testing.T) {
	defer setStdInOut(stdin, stdout)
	inr, inw, _ := os.Pipe()
	outr, outw, _ := os.Pipe()
	setStdInOut(inr, outw)

	items := []string{"gitlab", "github", "gitea"}
	done := make(chan int)
	go func() {
		idx, _ := Select("Which provider?", items)
		done <- idx
	}()

	q := make([]byte, 1024)
	for c := 0; c < 10; c, _ = outr.Read(q) {
	}
	output := string(q)
	assert.Contains(t, output, "Which provider?")

	// Send enter to select first item
	inw.Write([]byte("\n"))
	idx := <-done
	assert.Equal(t, 0, idx)
}

func TestSelectReturnsSelectedIndex(t *testing.T) {
	defer setStdInOut(stdin, stdout)
	inr, inw, _ := os.Pipe()
	_, outw, _ := os.Pipe()
	setStdInOut(inr, outw)

	items := []string{"gitlab", "github", "gitea"}
	done := make(chan int)
	go func() {
		idx, _ := Select("Which provider?", items)
		done <- idx
	}()

	// Move down once and select (arrow down = \x1b[B, enter = \n)
	inw.Write([]byte("\x1b[B\n"))
	idx := <-done
	assert.Equal(t, 1, idx)
}
