package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const checkoutAction = `name: Lore checkout adapter
description: Uses the Lore revision that LoreHub prepared before the job starts.
inputs:
  clean:
    required: false
  filter:
    required: false
  fetch-depth:
    required: false
  lfs:
    required: false
  path:
    required: false
  persist-credentials:
    required: false
  ref:
    required: false
  repository:
    required: false
  ssh-key:
    required: false
  sparse-checkout:
    required: false
  submodules:
    required: false
runs:
  using: composite
  steps:
    - name: Validate checkout options
      shell: bash
      env:
        LORE_CHECKOUT_FILTER: ${{ inputs.filter }}
        LORE_CHECKOUT_LFS: ${{ inputs.lfs }}
        LORE_CHECKOUT_PATH: ${{ inputs.path }}
        LORE_CHECKOUT_REF: ${{ inputs.ref }}
        LORE_CHECKOUT_REPOSITORY: ${{ inputs.repository }}
        LORE_CHECKOUT_SPARSE: ${{ inputs.sparse-checkout }}
        LORE_CHECKOUT_SSH_KEY: ${{ inputs.ssh-key }}
        LORE_CHECKOUT_SUBMODULES: ${{ inputs.submodules }}
      run: |
        if [[ -n "$LORE_CHECKOUT_FILTER" || -n "$LORE_CHECKOUT_PATH" || -n "$LORE_CHECKOUT_REF" ]]; then
          echo "Lore checkout does not support filter, path, or ref inputs." >&2
          exit 1
        fi
        if [[ -n "$LORE_CHECKOUT_REPOSITORY" || -n "$LORE_CHECKOUT_SPARSE" || -n "$LORE_CHECKOUT_SSH_KEY" ]]; then
          echo "Lore checkout does not support repository, sparse-checkout, or ssh-key inputs." >&2
          exit 1
        fi
        if [[ "$LORE_CHECKOUT_LFS" == "true" || ( -n "$LORE_CHECKOUT_SUBMODULES" && \
          "$LORE_CHECKOUT_SUBMODULES" != "false" ) ]]; then
          echo "Lore checkout does not support Git LFS or submodules." >&2
          exit 1
        fi
`

type PushTrigger struct {
	Branches       []string `json:"branches,omitempty"`
	BranchesIgnore []string `json:"branches_ignore,omitempty"`
}

type WorkflowDefinition struct {
	Path             string
	Name             string
	Enabled          bool
	State            string
	ErrorCode        string
	ErrorMessage     string
	Push             *PushTrigger
	WorkflowDispatch bool
	TriggerConfig    json.RawMessage
}

func DiscoverWorkflows(workspace string) ([]WorkflowDefinition, error) {
	directory := filepath.Join(workspace, ".github", "workflows")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return []WorkflowDefinition{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}

	workflows := make([]WorkflowDefinition, 0)
	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join(".github", "workflows", entry.Name()))
		workflow, err := parseWorkflowFile(filepath.Join(directory, entry.Name()), path)
		if err != nil {
			workflow.Enabled = false
			workflow.State = "error"
			workflow.ErrorCode = workflowErrorCode(err)
			workflow.ErrorMessage = err.Error()
			workflow.TriggerConfig = json.RawMessage(`{}`)
		}
		workflows = append(workflows, workflow)
	}
	return workflows, nil
}

func (definition WorkflowDefinition) MatchesPush(branch string) bool {
	if !definition.Enabled || definition.State != "active" || definition.Push == nil {
		return false
	}
	if !matchesBranchList(definition.Push.Branches, branch, true) {
		return false
	}
	return !matchesBranchList(definition.Push.BranchesIgnore, branch, false)
}

func AdaptWorkflow(workspace string, workflowPath string) (int, error) {
	if err := validateWorkflowPath(workflowPath); err != nil {
		return 0, err
	}
	actionDirectory := filepath.Join(workspace, ".lorehub", "actions", "checkout")
	if err := os.MkdirAll(actionDirectory, 0o750); err != nil {
		return 0, fmt.Errorf("create checkout adapter directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(actionDirectory, "action.yml"), []byte(checkoutAction), 0o600); err != nil {
		return 0, fmt.Errorf("write checkout adapter: %w", err)
	}

	path := filepath.Join(workspace, filepath.FromSlash(workflowPath))
	if err := validateWorkflowFile(path); err != nil {
		return 0, err
	}
	return adaptWorkflow(path)
}

func AdaptWorkflows(workspace string) (int, error) {
	directory := filepath.Join(workspace, ".github", "workflows")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read workflow directory: %w", err)
	}

	replacements := 0
	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join(".github", "workflows", entry.Name()))
		count, err := AdaptWorkflow(workspace, path)
		if err != nil {
			return 0, err
		}
		replacements += count
	}
	return replacements, nil
}

func parseWorkflowFile(filePath string, workflowPath string) (WorkflowDefinition, error) {
	workflow := WorkflowDefinition{
		Path:    workflowPath,
		Name:    strings.TrimSuffix(filepath.Base(workflowPath), filepath.Ext(workflowPath)),
		Enabled: true,
		State:   "active",
	}
	fileInfo, err := os.Lstat(filePath)
	if err != nil {
		return workflow, fmt.Errorf("inspect workflow %q: %w", workflowPath, err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
		return workflow, fmt.Errorf("workflow %q is not a regular file", workflowPath)
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return workflow, fmt.Errorf("read workflow %q: %w", workflowPath, err)
	}
	if len(contents) > 1<<20 {
		return workflow, errors.New("workflow file exceeds the 1 MiB limit")
	}

	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return workflow, fmt.Errorf("parse workflow %q: %w", workflowPath, err)
	}
	root, err := documentRoot(&document)
	if err != nil {
		return workflow, err
	}
	if nameNode := mappingValue(root, "name"); nameNode != nil {
		name, err := scalarString(nameNode, "workflow name")
		if err != nil {
			return workflow, err
		}
		if strings.TrimSpace(name) != "" {
			workflow.Name = limitWorkflowText(strings.TrimSpace(name))
		}
	}
	jobs := mappingValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return workflow, errors.New("workflow must define at least one job")
	}
	on := mappingValue(root, "on")
	if on == nil {
		return workflow, errors.New("workflow must define an on trigger")
	}
	push, dispatch, err := parseTriggers(on)
	if err != nil {
		return workflow, err
	}
	workflow.Push = push
	workflow.WorkflowDispatch = dispatch
	workflow.TriggerConfig, err = encodeTriggerConfig(push, dispatch)
	if err != nil {
		return workflow, err
	}
	if push == nil && !dispatch {
		return workflow, errors.New("workflow has no supported trigger")
	}
	return workflow, nil
}

func parseTriggers(node *yaml.Node) (*PushTrigger, bool, error) {
	push := (*PushTrigger)(nil)
	dispatch := false
	visit := func(name string, value *yaml.Node) error {
		switch name {
		case "push":
			if push != nil {
				return errors.New("workflow declares push more than once")
			}
			parsed, err := parsePushTrigger(value)
			if err != nil {
				return err
			}
			push = parsed
		case "workflow_dispatch":
			if dispatch {
				return errors.New("workflow declares workflow_dispatch more than once")
			}
			if err := validateWorkflowDispatch(value); err != nil {
				return err
			}
			dispatch = true
		default:
			return fmt.Errorf("unsupported workflow event %q", name)
		}
		return nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		name, err := scalarString(node, "workflow event")
		if err != nil {
			return nil, false, err
		}
		if err := visit(name, nil); err != nil {
			return nil, false, err
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return nil, false, errors.New("workflow event list must not be empty")
		}
		for _, item := range node.Content {
			name, err := scalarString(item, "workflow event")
			if err != nil {
				return nil, false, err
			}
			if err := visit(name, nil); err != nil {
				return nil, false, err
			}
		}
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return nil, false, errors.New("workflow event map must not be empty")
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			name, err := scalarString(node.Content[index], "workflow event")
			if err != nil {
				return nil, false, err
			}
			if err := visit(name, node.Content[index+1]); err != nil {
				return nil, false, err
			}
		}
	default:
		return nil, false, errors.New("workflow on must be a scalar, list, or map")
	}
	return push, dispatch, nil
}

func parsePushTrigger(node *yaml.Node) (*PushTrigger, error) {
	trigger := &PushTrigger{}
	if node == nil || isNullNode(node) {
		return trigger, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("push trigger must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		values, err := parsePatternList(node.Content[index+1], key)
		if err != nil {
			return nil, err
		}
		switch key {
		case "branches":
			trigger.Branches = values
		case "branches-ignore":
			trigger.BranchesIgnore = values
		default:
			return nil, fmt.Errorf("unsupported push filter %q; only branch filters are supported", key)
		}
	}
	if len(trigger.Branches) > 0 && len(trigger.BranchesIgnore) > 0 {
		return nil, errors.New("push trigger cannot use both branches and branches-ignore")
	}
	return trigger, nil
}

func parsePatternList(node *yaml.Node, field string) ([]string, error) {
	if node.Kind == yaml.ScalarNode && !isNullNode(node) {
		value, err := scalarString(node, field)
		if err != nil {
			return nil, err
		}
		return []string{value}, nil
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, fmt.Errorf("push filter %q must be a non-empty string list", field)
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		value, err := scalarString(item, field)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func validateWorkflowDispatch(node *yaml.Node) error {
	if node == nil || isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("workflow_dispatch trigger must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if key != "inputs" {
			return fmt.Errorf("unsupported workflow_dispatch field %q", key)
		}
		inputs := node.Content[index+1]
		if inputs.Kind != yaml.MappingNode {
			return errors.New("workflow_dispatch inputs must be a map")
		}
		for inputIndex := 0; inputIndex+1 < len(inputs.Content); inputIndex += 2 {
			input := inputs.Content[inputIndex+1]
			if input.Kind != yaml.MappingNode {
				return errors.New("workflow_dispatch input definitions must be maps")
			}
			for fieldIndex := 0; fieldIndex+1 < len(input.Content); fieldIndex += 2 {
				field := input.Content[fieldIndex].Value
				switch field {
				case "description", "required", "default", "type", "options":
				default:
					return fmt.Errorf("unsupported workflow_dispatch input field %q", field)
				}
			}
		}
	}
	return nil
}

func encodeTriggerConfig(push *PushTrigger, dispatch bool) (json.RawMessage, error) {
	config := make(map[string]any, 2)
	if push != nil {
		config["push"] = push
	}
	if dispatch {
		config["workflow_dispatch"] = map[string]any{}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode workflow triggers: %w", err)
	}
	return encoded, nil
}

func documentRoot(document *yaml.Node) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 ||
		document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("workflow document must be a YAML map")
	}
	return document.Content[0], nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key || key == "on" && node.Content[index].Value == "true" {
			return node.Content[index+1]
		}
	}
	return nil
}

func scalarString(node *yaml.Node, field string) (string, error) {
	if node.Kind != yaml.ScalarNode || isNullNode(node) || strings.TrimSpace(node.Value) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", field)
	}
	return node.Value, nil
}

func isNullNode(node *yaml.Node) bool {
	return node.Tag == "!!null" || strings.EqualFold(node.Value, "null") || node.Value == "~"
}

func isWorkflowFile(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".yml" || extension == ".yaml"
}

func validateWorkflowPath(workflowPath string) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(workflowPath)))
	if clean != workflowPath || !strings.HasPrefix(clean, ".github/workflows/") ||
		strings.Contains(clean, "../") || !isWorkflowFile(clean) {
		return fmt.Errorf("invalid workflow path %q", workflowPath)
	}
	return nil
}

func workflowErrorCode(err error) string {
	if strings.Contains(err.Error(), "unsupported workflow event") ||
		strings.Contains(err.Error(), "unsupported push filter") ||
		strings.Contains(err.Error(), "unsupported workflow_dispatch") ||
		strings.Contains(err.Error(), "push trigger cannot use") {
		return "unsupported_trigger"
	}
	return "invalid_workflow"
}

func limitWorkflowText(value string) string {
	characters := []rune(value)
	if len(characters) <= 255 {
		return value
	}
	return string(characters[:255])
}

func matchesBranchList(patterns []string, branch string, includeDefault bool) bool {
	if len(patterns) == 0 {
		return includeDefault
	}
	matched := false
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		if globMatch(pattern, branch) {
			if negated {
				return false
			}
			matched = true
		}
	}
	return matched
}

func globMatch(pattern string, value string) bool {
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				expression.WriteString(".*")
				index++
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[index])))
		}
	}
	expression.WriteString("$")
	return regexp.MustCompile(expression.String()).MatchString(value)
}

func adaptWorkflow(path string) (int, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read workflow %q: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return 0, fmt.Errorf("parse workflow %q: %w", path, err)
	}
	replacements := replaceCheckoutUses(&document)
	if replacements == 0 {
		return 0, nil
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return 0, fmt.Errorf("encode workflow %q: %w", path, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return 0, fmt.Errorf("write adapted workflow %q: %w", path, err)
	}
	return replacements, nil
}

func replaceCheckoutUses(node *yaml.Node) int {
	replacements := 0
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key.Value == "uses" && value.Kind == yaml.ScalarNode &&
				strings.HasPrefix(value.Value, "actions/checkout@") {
				value.Value = "./.lorehub/actions/checkout"
				replacements++
			}
		}
	}
	for _, child := range node.Content {
		replacements += replaceCheckoutUses(child)
	}
	return replacements
}
