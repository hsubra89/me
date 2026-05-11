package cli

import (
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
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

func (huhPrompter) Password(title string) (string, error) {
	var value string
	if err := huh.NewInput().
		Title(title).
		Password(true).
		Value(&value).
		Run(); err != nil {
		return "", err
	}
	return value, nil
}

func (huhPrompter) SelectSSHIdentity(choices []sshIdentityPromptChoice, selected int) (sshIdentityPromptChoice, error) {
	options := make([]huh.Option[int], 0, len(choices))
	for index, choice := range choices {
		options = append(options, huh.NewOption(choice.Label, index))
	}

	if selected < 0 || selected >= len(choices) {
		selected = 0
	}
	if err := huh.NewSelect[int]().
		Title("Select SSH identity for remote server access").
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return sshIdentityPromptChoice{}, err
	}

	return choices[selected], nil
}

func (huhPrompter) SelectPersonalServerLocation(choices []personalServerLocationChoice, selected int) (personalServerLocationChoice, error) {
	options := make([]huh.Option[int], 0, len(choices))
	for index, choice := range choices {
		options = append(options, huh.NewOption(choice.Label, index))
	}

	if selected < 0 || selected >= len(choices) {
		selected = 0
	}
	if err := huh.NewSelect[int]().
		Title("Select Personal Server Location").
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return personalServerLocationChoice{}, err
	}

	return choices[selected], nil
}

func (huhPrompter) SelectPersonalServerType(choices []personalServerTypeChoice, selected int) (personalServerTypeChoice, error) {
	options := make([]huh.Option[int], 0, len(choices))
	for index, choice := range choices {
		options = append(options, huh.NewOption(choice.Label, index))
	}

	if selected < 0 || selected >= len(choices) {
		selected = 0
	}
	if err := huh.NewSelect[int]().
		Title("Select Personal Server Type").
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return personalServerTypeChoice{}, err
	}

	return choices[selected], nil
}

func isTerminal(file *os.File) bool {
	return file != nil && term.IsTerminal(file.Fd())
}
