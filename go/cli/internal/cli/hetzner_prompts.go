package cli

import (
	"fmt"
	"os"

	"charm.land/huh/v2"
)

type hetznerAuthPrompter interface {
	CanPrompt() bool
	Confirm(title string, affirmative bool) (bool, error)
	SelectHcloudContext(candidates []hcloudTokenCandidate) (hcloudTokenCandidate, bool, error)
	InputToken() (string, error)
}

type huhPrompter struct{}

func (huhPrompter) CanPrompt() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

func (huhPrompter) Confirm(title string, affirmative bool) (bool, error) {
	value := affirmative
	if err := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		Value(&value).
		Run(); err != nil {
		return false, err
	}
	return value, nil
}

func (huhPrompter) SelectHcloudContext(candidates []hcloudTokenCandidate) (hcloudTokenCandidate, bool, error) {
	const enterNewToken = -1

	options := make([]huh.Option[int], 0, len(candidates)+1)
	selected := enterNewToken
	for index, candidate := range candidates {
		label := candidate.Name
		if candidate.Active {
			label = fmt.Sprintf("%s (active)", label)
			selected = index
		} else if selected == enterNewToken && index == 0 {
			selected = index
		}
		options = append(options, huh.NewOption(label, index))
	}
	options = append(options, huh.NewOption("Enter a new token", enterNewToken))

	if err := huh.NewSelect[int]().
		Title("Select a Hetzner hcloud context to import").
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return hcloudTokenCandidate{}, false, err
	}

	if selected == enterNewToken {
		return hcloudTokenCandidate{}, false, nil
	}
	return candidates[selected], true, nil
}

func (huhPrompter) InputToken() (string, error) {
	var token string
	if err := huh.NewInput().
		Title("Hetzner API token").
		Password(true).
		Value(&token).
		Run(); err != nil {
		return "", err
	}
	return token, nil
}

func (huhPrompter) Input(title string, defaultValue string, validate func(string) error) (string, error) {
	value := defaultValue
	if err := huh.NewInput().
		Title(title).
		Value(&value).
		Validate(validate).
		Run(); err != nil {
		return "", err
	}
	return value, nil
}

func isTerminal(file *os.File) bool {
	stat, err := file.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}
