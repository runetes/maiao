package prompt

import "github.com/manifoldco/promptui"

// Select prompts the user to select an item from a list and returns the selected index.
func Select(label string, items []string) (int, error) {
	p := promptui.Select{
		Label:  label,
		Items:  items,
		Stdin:  stdin,
		Stdout: stdout,
	}
	idx, _, err := p.Run()
	return idx, err
}
