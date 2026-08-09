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
	"time"

	"gopkg.in/yaml.v3"
)

type PushTrigger struct {
	Branches       []string `json:"branches,omitempty"`
	BranchesIgnore []string `json:"branches_ignore,omitempty"`
}

type PullRequestTrigger struct {
	Branches       []string `json:"branches,omitempty"`
	BranchesIgnore []string `json:"branches_ignore,omitempty"`
	Types          []string `json:"types,omitempty"`
}

type ScheduleTrigger struct {
	Cron string `json:"cron"`
}

type RepositoryDispatchTrigger struct {
	Types []string `json:"types,omitempty"`
}

type WorkflowDefinition struct {
	Path               string
	Name               string
	Enabled            bool
	State              string
	ErrorCode          string
	ErrorMessage       string
	Push               *PushTrigger
	PullRequest        *PullRequestTrigger
	Schedules          []ScheduleTrigger
	RepositoryDispatch *RepositoryDispatchTrigger
	WorkflowDispatch   bool
	TriggerConfig      json.RawMessage
}

func DiscoverWorkflows(workspace string) ([]WorkflowDefinition, error) {
	directory, err := findWorkflowDirectory(workspace)
	if errors.Is(err, fs.ErrNotExist) {
		return []WorkflowDefinition{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
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

func (definition WorkflowDefinition) MatchesPullRequest(branch string, action string) bool {
	trigger := definition.PullRequest
	if !definition.Enabled || definition.State != "active" || trigger == nil {
		return false
	}
	if !matchesBranchList(trigger.Branches, branch, true) ||
		matchesBranchList(trigger.BranchesIgnore, branch, false) {
		return false
	}
	return matchesEventType(trigger.Types, action)
}

func (definition WorkflowDefinition) MatchesRepositoryDispatch(eventType string) bool {
	trigger := definition.RepositoryDispatch
	if !definition.Enabled || definition.State != "active" || trigger == nil {
		return false
	}
	return matchesEventType(trigger.Types, eventType)
}

func (definition WorkflowDefinition) ScheduleOccurrences(now time.Time) []ScheduleOccurrence {
	if !definition.Enabled || definition.State != "active" {
		return nil
	}
	occurences := make([]ScheduleOccurrence, 0, len(definition.Schedules))
	for _, schedule := range definition.Schedules {
		occurrence, ok := LastScheduleOccurrence(schedule.Cron, now)
		if ok {
			occurences = append(occurences, ScheduleOccurrence{Key: schedule.Cron, At: occurrence})
		}
	}
	return occurences
}

type ScheduleOccurrence struct {
	Key string
	At  time.Time
}

func workflowFromTriggerConfig(
	path string,
	name string,
	enabled bool,
	state string,
	triggerConfig json.RawMessage,
) (WorkflowDefinition, error) {
	var config struct {
		Push               *PushTrigger               `json:"push"`
		WorkflowDispatch   json.RawMessage            `json:"workflow_dispatch"`
		PullRequest        *PullRequestTrigger        `json:"pull_request"`
		Schedules          []ScheduleTrigger          `json:"schedule"`
		RepositoryDispatch *RepositoryDispatchTrigger `json:"repository_dispatch"`
	}
	if len(triggerConfig) > 0 && string(triggerConfig) != "null" {
		if err := json.Unmarshal(triggerConfig, &config); err != nil {
			return WorkflowDefinition{}, fmt.Errorf("decode workflow trigger configuration: %w", err)
		}
	}
	return WorkflowDefinition{
		Path: path, Name: name, Enabled: enabled, State: state, Push: config.Push,
		WorkflowDispatch: len(config.WorkflowDispatch) > 0,
		PullRequest:      config.PullRequest, Schedules: config.Schedules,
		RepositoryDispatch: config.RepositoryDispatch, TriggerConfig: triggerConfig,
	}, nil
}

func AdaptWorkflow(workspace string, workflowPath string) (int, error) {
	if err := validateWorkflowPath(workflowPath); err != nil {
		return 0, err
	}
	if _, err := findWorkflowDirectory(workspace); err != nil {
		return 0, err
	}
	path := filepath.Join(workspace, filepath.FromSlash(workflowPath))
	if err := validateWorkflowFile(path); err != nil {
		return 0, err
	}
	return validateCheckoutWorkflow(path)
}

func AdaptWorkflows(workspace string) (int, error) {
	directory, err := findWorkflowDirectory(workspace)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(directory)
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

func findWorkflowDirectory(workspace string) (string, error) {
	githubDirectory := filepath.Join(workspace, ".github")
	workflowDirectory := filepath.Join(githubDirectory, "workflows")
	for _, directory := range []string{githubDirectory, workflowDirectory} {
		info, err := os.Lstat(directory)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fs.ErrNotExist
			}
			return "", fmt.Errorf("inspect workflow directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("workflow directory is not a real directory")
		}
	}
	return workflowDirectory, nil
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
	push, dispatch, pullRequest, schedules, repositoryDispatch, err := parseTriggers(on)
	if err != nil {
		return workflow, err
	}
	workflow.Push = push
	workflow.WorkflowDispatch = dispatch
	workflow.PullRequest = pullRequest
	workflow.Schedules = schedules
	workflow.RepositoryDispatch = repositoryDispatch
	workflow.TriggerConfig, err = encodeTriggerConfig(
		push, dispatch, pullRequest, schedules, repositoryDispatch,
	)
	if err != nil {
		return workflow, err
	}
	if push == nil && !dispatch && pullRequest == nil && len(schedules) == 0 && repositoryDispatch == nil {
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

func parseTriggers(node *yaml.Node) (
	*PushTrigger,
	bool,
	*PullRequestTrigger,
	[]ScheduleTrigger,
	*RepositoryDispatchTrigger,
	error,
) {
	push := (*PushTrigger)(nil)
	dispatch := false
	pullRequest := (*PullRequestTrigger)(nil)
	schedules := make([]ScheduleTrigger, 0)
	repositoryDispatch := (*RepositoryDispatchTrigger)(nil)
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
		case "pull_request":
			if pullRequest != nil {
				return errors.New("workflow declares pull_request more than once")
			}
			parsed, err := parsePullRequestTrigger(value)
			if err != nil {
				return err
			}
			pullRequest = parsed
		case "schedule":
			parsed, err := parseScheduleTriggers(value)
			if err != nil {
				return err
			}
			schedules = append(schedules, parsed...)
		case "repository_dispatch":
			if repositoryDispatch != nil {
				return errors.New("workflow declares repository_dispatch more than once")
			}
			parsed, err := parseRepositoryDispatchTrigger(value)
			if err != nil {
				return err
			}
			repositoryDispatch = parsed
		default:
			return fmt.Errorf("unsupported workflow event %q", name)
		}
		return nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		name, err := scalarString(node, "workflow event")
		if err != nil {
			return nil, false, nil, nil, nil, err
		}
		if err := visit(name, nil); err != nil {
			return nil, false, nil, nil, nil, err
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return nil, false, nil, nil, nil, errors.New("workflow event list must not be empty")
		}
		for _, item := range node.Content {
			name, err := scalarString(item, "workflow event")
			if err != nil {
				return nil, false, nil, nil, nil, err
			}
			if err := visit(name, nil); err != nil {
				return nil, false, nil, nil, nil, err
			}
		}
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return nil, false, nil, nil, nil, errors.New("workflow event map must not be empty")
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			name, err := scalarString(node.Content[index], "workflow event")
			if err != nil {
				return nil, false, nil, nil, nil, err
			}
			if err := visit(name, node.Content[index+1]); err != nil {
				return nil, false, nil, nil, nil, err
			}
		}
	default:
		return nil, false, nil, nil, nil, errors.New("workflow on must be a scalar, list, or map")
	}
	return push, dispatch, pullRequest, schedules, repositoryDispatch, nil
}

func parsePullRequestTrigger(node *yaml.Node) (*PullRequestTrigger, error) {
	trigger := &PullRequestTrigger{}
	if node == nil || isNullNode(node) {
		return trigger, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("pull_request trigger must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		switch key {
		case "branches", "branches-ignore", "types":
			values, err := parseStringList(node.Content[index+1], key)
			if err != nil {
				return nil, err
			}
			switch key {
			case "branches":
				trigger.Branches = values
			case "branches-ignore":
				trigger.BranchesIgnore = values
			case "types":
				trigger.Types = values
			}
		default:
			return nil, fmt.Errorf("unsupported pull_request filter %q", key)
		}
	}
	if len(trigger.Branches) > 0 && len(trigger.BranchesIgnore) > 0 {
		return nil, errors.New("pull_request trigger cannot use both branches and branches-ignore")
	}
	return trigger, nil
}

func parseScheduleTriggers(node *yaml.Node) ([]ScheduleTrigger, error) {
	if node == nil || node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, errors.New("schedule trigger must be a non-empty list")
	}
	triggers := make([]ScheduleTrigger, 0, len(node.Content))
	seen := make(map[string]struct{}, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, errors.New("schedule entries must be maps")
		}
		cronNode := mappingValue(item, "cron")
		cron, err := scalarString(cronNode, "schedule cron")
		if err != nil {
			return nil, err
		}
		cron = strings.Join(strings.Fields(cron), " ")
		if _, err := parseCron(cron); err != nil {
			return nil, err
		}
		for index := 0; index+1 < len(item.Content); index += 2 {
			if item.Content[index].Value != "cron" {
				return nil, fmt.Errorf("unsupported schedule field %q", item.Content[index].Value)
			}
		}
		if _, ok := seen[cron]; ok {
			return nil, fmt.Errorf("schedule cron %q is declared more than once", cron)
		}
		seen[cron] = struct{}{}
		triggers = append(triggers, ScheduleTrigger{Cron: cron})
	}
	return triggers, nil
}

func parseRepositoryDispatchTrigger(node *yaml.Node) (*RepositoryDispatchTrigger, error) {
	trigger := &RepositoryDispatchTrigger{}
	if node == nil || isNullNode(node) {
		return trigger, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("repository_dispatch trigger must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if key != "types" {
			return nil, fmt.Errorf("unsupported repository_dispatch field %q", key)
		}
		values, err := parseStringList(node.Content[index+1], key)
		if err != nil {
			return nil, err
		}
		trigger.Types = values
	}
	return trigger, nil
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
	values, err := parseStringList(node, field)
	if err != nil {
		return nil, fmt.Errorf("push filter %q must be a non-empty string list: %w", field, err)
	}
	hasPositive := false
	for _, value := range values {
		if !strings.HasPrefix(value, "!") {
			hasPositive = true
			break
		}
	}
	if !hasPositive {
		return nil, fmt.Errorf("push filter %q must contain a non-negative pattern", field)
	}
	return values, nil
}

func parseStringList(node *yaml.Node, field string) ([]string, error) {
	if node == nil {
		return nil, fmt.Errorf("%s must be a non-empty string list", field)
	}
	if node.Kind == yaml.ScalarNode && !isNullNode(node) {
		value, err := scalarString(node, field)
		if err != nil {
			return nil, err
		}
		return []string{value}, nil
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty string list", field)
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

func encodeTriggerConfig(
	push *PushTrigger,
	dispatch bool,
	pullRequest *PullRequestTrigger,
	schedules []ScheduleTrigger,
	repositoryDispatch *RepositoryDispatchTrigger,
) (json.RawMessage, error) {
	config := make(map[string]any, 5)
	if push != nil {
		config["push"] = push
	}
	if pullRequest != nil {
		config["pull_request"] = pullRequest
	}
	if len(schedules) > 0 {
		config["schedule"] = schedules
	}
	if repositoryDispatch != nil {
		config["repository_dispatch"] = repositoryDispatch
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
	if node == nil || node.Kind != yaml.ScalarNode || isNullNode(node) || strings.TrimSpace(node.Value) == "" {
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
				matched = false
			} else {
				matched = true
			}
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
