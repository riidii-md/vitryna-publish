package scaffold

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"
	"text/template"
)

//go:embed assets/*
var assets embed.FS

type actionPin struct{ Version, Commit, Module string }
type renderInput struct {
	Options
	Checkout, Upload actionPin
	PublishCondition string
}

func normalizeVerifyCommand(command string) (string, error) {
	command = strings.ReplaceAll(command, "\r\n", "\n")
	if strings.ContainsAny(command, "\r\u0085\u2028\u2029") {
		return "", setupError("setup_command_separator_forbidden")
	}
	return command, nil
}

func render(name string, options Options) ([]byte, error) {
	for _, input := range []string{options.SourceRepository, options.GitHub.Repository, options.GitHub.BaseBranch, options.GitHub.PromotionBranch, options.GitHub.TrustedSourceBranch, options.VerifyCommand} {
		if strings.Contains(input, "${{") {
			return nil, setupError("setup_workflow_expression_forbidden")
		}
	}
	command, err := normalizeVerifyCommand(options.VerifyCommand)
	if err != nil {
		return nil, err
	}
	options.VerifyCommand = command
	if strings.TrimSpace(options.VerifyCommand) == "" {
		options.VerifyCommand = "echo 'Set a project verification command in this workflow before publishing.' >&2\nexit 1"
	}
	var pins map[string]actionPin
	data, err := assets.ReadFile("assets/action-pins.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		return nil, err
	}
	literal := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	publish := "github.repository == " + literal(options.SourceRepository) + " && ((github.event_name == 'push' && github.ref == " + literal("refs/heads/"+options.GitHub.TrustedSourceBranch) + ") || (github.event_name == 'workflow_dispatch' && inputs.publish))"
	input := renderInput{options, pins["actions/checkout"], pins["actions/upload-artifact"], publish}
	data, err = assets.ReadFile("assets/" + name)
	if err != nil {
		return nil, err
	}
	functions := template.FuncMap{
		"q": func(value string) string { data, _ := json.Marshal(value); return string(data) },
		"indent": func(count int, value string) string {
			pad := strings.Repeat(" ", count)
			return pad + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n"+pad)
		},
	}
	parser := template.New(name).Funcs(functions).Option("missingkey=error")
	if name == "workflow.yml.tmpl" {
		parser = parser.Delims("[[", "]]")
	}
	t, err := parser.Parse(string(data))
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	if err := t.Execute(&result, input); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func RenderWorkflow(options Options) ([]byte, error) { return render("workflow.yml.tmpl", options) }
func RenderGuide(options Options) ([]byte, error)    { return render("setup.md.tmpl", options) }
