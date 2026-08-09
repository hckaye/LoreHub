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
	path := filepath.Join(workspace, filepath.FromSlash(workflowPath))
	if err := validateWorkflowFile(path); err != nil {
		return 0, err
	}
	return validateCheckoutWorkflow(path)
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

	adapted := 0
	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join(".github", "workflows", entry.Name()))
		count, err := AdaptWorkflow(workspace, path)
		if err != nil {
			return 0, err
		}
		adapted += count
	}
	return adapted, nil
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
	if err := validateJobRuntimeDefinitions(jobs); err != nil {
		return workflow, err
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

func validateJobRuntimeDefinitions(jobs *yaml.Node) error {
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		jobID := jobs.Content[index].Value
		job := jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return fmt.Errorf("job %q must be a map", jobID)
		}
		if err := validateJobContainer(jobID, mappingValue(job, "container")); err != nil {
			return err
		}
		if err := validateJobServices(jobID, mappingValue(job, "services")); err != nil {
			return err
		}
	}
	return nil
}

func validateJobContainer(jobID string, node *yaml.Node) error {
	if node == nil || isNullNode(node) {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		_, err := scalarString(node, fmt.Sprintf("job %q container image", jobID))
		return err
	case yaml.MappingNode:
		if mappingValue(node, "image") == nil {
			return fmt.Errorf("unsupported container definition in job %q: image is required", jobID)
		}
		if image := mappingValue(node, "image"); image != nil {
			if _, err := scalarString(image, fmt.Sprintf("job %q container image", jobID)); err != nil {
				return err
			}
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			switch key {
			case "image":
			case "options":
				if err := validateEmptyRuntimeOptions(node.Content[index+1], "container", jobID); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported container field %q in job %q", key, jobID)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported container definition in job %q: expected an image or map", jobID)
	}
}

func validateJobServices(jobID string, node *yaml.Node) error {
	if node == nil || isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("unsupported service definition in job %q: services must be a map", jobID)
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		serviceID := node.Content[index].Value
		service := node.Content[index+1]
		switch service.Kind {
		case yaml.ScalarNode:
			if _, err := scalarString(service, fmt.Sprintf("job %q service %q image", jobID, serviceID)); err != nil {
				return err
			}
		case yaml.MappingNode:
			if mappingValue(service, "image") == nil {
				return fmt.Errorf(
					"unsupported service definition %q in job %q: image is required",
					serviceID,
					jobID,
				)
			}
			if image := mappingValue(service, "image"); image != nil {
				if _, err := scalarString(
					image,
					fmt.Sprintf("job %q service %q image", jobID, serviceID),
				); err != nil {
					return err
				}
			}
			for fieldIndex := 0; fieldIndex+1 < len(service.Content); fieldIndex += 2 {
				key := service.Content[fieldIndex].Value
				switch key {
				case "image":
				case "options":
					if err := validateEmptyRuntimeOptions(
						service.Content[fieldIndex+1],
						"service",
						jobID+"/"+serviceID,
					); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unsupported service field %q in job %q", key, jobID)
				}
			}
		default:
			return fmt.Errorf("unsupported service definition %q in job %q", serviceID, jobID)
		}
	}
	return nil
}

func validateEmptyRuntimeOptions(node *yaml.Node, kind string, name string) error {
	if node == nil || isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("unsupported %s options in %q: options must be empty", kind, name)
	}
	if strings.TrimSpace(node.Value) != "" {
		return fmt.Errorf("unsupported %s options in %q: non-empty options are not supported", kind, name)
	}
	return nil
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
	if strings.Contains(err.Error(), "unsupported container") || strings.Contains(err.Error(), "unsupported service") {
		return "unsupported_runtime_definition"
	}
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

func validateCheckoutWorkflow(path string) (int, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read workflow %q: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return 0, fmt.Errorf("parse workflow %q: %w", path, err)
	}
	// act handles actions/checkout by copying the prepared --directory workspace into the job.
	// Keep the reference intact: a local action would be resolved inside the remote job first.
	adapted, err := validateCheckoutUses(&document)
	if err != nil {
		return 0, fmt.Errorf("validate Lore checkout adapter in %q: %w", path, err)
	}
	return adapted, nil
}

func validateCheckoutUses(node *yaml.Node) (int, error) {
	adapted := 0
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key.Value == "uses" && value.Kind == yaml.ScalarNode &&
				strings.HasPrefix(value.Value, "actions/checkout@") {
				adapted++
				if err := validateCheckoutInputs(mappingValue(node, "with")); err != nil {
					return 0, err
				}
			}
		}
	}
	for _, child := range node.Content {
		count, err := validateCheckoutUses(child)
		if err != nil {
			return 0, err
		}
		adapted += count
	}
	return adapted, nil
}

func validateCheckoutInputs(node *yaml.Node) error {
	if node == nil || isNullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("actions/checkout with must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		value := node.Content[index+1]
		switch key {
		case "clean", "fetch-depth", "persist-credentials":
		case "filter", "path", "ref", "repository", "sparse-checkout", "ssh-key":
			if value.Kind != yaml.ScalarNode || !isNullNode(value) && strings.TrimSpace(value.Value) != "" {
				return fmt.Errorf("actions/checkout input %q is not supported by the Lore adapter", key)
			}
		case "lfs":
			if value.Kind != yaml.ScalarNode {
				return errors.New("actions/checkout input lfs must be a scalar")
			}
			if strings.EqualFold(strings.TrimSpace(value.Value), "true") {
				return errors.New("actions/checkout input lfs=true is not supported by the Lore adapter")
			}
		case "submodules":
			if value.Kind != yaml.ScalarNode {
				return errors.New("actions/checkout input submodules must be a scalar")
			}
			if !isNullNode(value) && strings.TrimSpace(value.Value) != "" &&
				!strings.EqualFold(strings.TrimSpace(value.Value), "false") {
				return errors.New("actions/checkout submodules are not supported by the Lore adapter")
			}
		default:
			return fmt.Errorf("actions/checkout input %q is not supported by the Lore adapter", key)
		}
	}
	return nil
}
