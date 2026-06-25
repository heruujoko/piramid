package runtime

import (
	"fmt"
	"regexp"
	"strings"
)

type TemplateValues struct {
	Prompt     string
	PromptFile string
	Workspace  string
	TaskID     string
	Attempt    string
	Model      string
}

var templatePattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)

func ExpandArgs(args []string, values TemplateValues) ([]string, error) {
	replacements := map[string]string{
		"{{prompt}}":      values.Prompt,
		"{{prompt_file}}": values.PromptFile,
		"{{workspace}}":   values.Workspace,
		"{{task_id}}":     values.TaskID,
		"{{attempt}}":     values.Attempt,
		"{{model}}":       values.Model,
	}
	expanded := make([]string, len(args))
	for i, argument := range args {
		for _, placeholder := range templatePattern.FindAllString(argument, -1) {
			value, ok := replacements[placeholder]
			if !ok {
				return nil, fmt.Errorf("unknown placeholder %s", placeholder)
			}
			argument = strings.ReplaceAll(argument, placeholder, value)
		}
		expanded[i] = argument
	}
	return expanded, nil
}
