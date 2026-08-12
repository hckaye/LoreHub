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

type WorkflowDispatchInput struct {
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     *string  `json:"default,omitempty"`
	Type        string   `json:"type"`
	Options     []string `json:"options,omitempty"`
}

type WorkflowDispatchConfig struct {
	Inputs map[string]WorkflowDispatchInput `json:"inputs,omitempty"`
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
	DispatchInputs     map[string]WorkflowDispatchInput
	Environment        string
	TriggerConfig      json.RawMessage
}

func DiscoverWorkflows(workspace string, platformImages ...map[string]string) ([]WorkflowDefinition, error) {
	images := DefaultRunnerPlatformImages()
	if len(platformImages) > 0 && platformImages[0] != nil {
		var err error
		images, err = mergedRunnerPlatformImages(platformImages[0])
		if err != nil {
			return nil, err
		}
	}
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
		workflow, err := parseWorkflowFile(filepath.Join(directory, entry.Name()), path, images)
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
		WorkflowDispatch   *WorkflowDispatchConfig    `json:"workflow_dispatch"`
		PullRequest        *PullRequestTrigger        `json:"pull_request"`
		Schedules          []ScheduleTrigger          `json:"schedule"`
		RepositoryDispatch *RepositoryDispatchTrigger `json:"repository_dispatch"`
		Environment        string                     `json:"environment"`
	}
	if len(triggerConfig) > 0 && string(triggerConfig) != "null" {
		if err := json.Unmarshal(triggerConfig, &config); err != nil {
			return WorkflowDefinition{}, fmt.Errorf("decode workflow trigger configuration: %w", err)
		}
	}
	return WorkflowDefinition{
		Path: path, Name: name, Enabled: enabled, State: state, Push: config.Push,
		WorkflowDispatch: config.WorkflowDispatch != nil,
		DispatchInputs: func() map[string]WorkflowDispatchInput {
			if config.WorkflowDispatch == nil {
				return nil
			}
			return config.WorkflowDispatch.Inputs
		}(),
		PullRequest: config.PullRequest, Schedules: config.Schedules,
		RepositoryDispatch: config.RepositoryDispatch, Environment: config.Environment,
		TriggerConfig: triggerConfig,
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

func parseWorkflowFile(
	filePath string,
	workflowPath string,
	platformImages map[string]string,
) (WorkflowDefinition, error) {
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
	workflow.Environment, err = workflowEnvironmentFromJobs(jobs)
	if err != nil {
		return workflow, err
	}
	if err := validateWorkflowRunnerLabels(filePath, platformImages); err != nil {
		return workflow, err
	}
	on := mappingValue(root, "on")
	if on == nil {
		return workflow, errors.New("workflow must define an on trigger")
	}
	push, dispatch, dispatchInputs, pullRequest, schedules, repositoryDispatch, err := parseTriggers(on)
	if err != nil {
		return workflow, err
	}
	workflow.Push = push
	workflow.WorkflowDispatch = dispatch
	workflow.DispatchInputs = dispatchInputs
	workflow.PullRequest = pullRequest
	workflow.Schedules = schedules
	workflow.RepositoryDispatch = repositoryDispatch
	workflow.TriggerConfig, err = encodeTriggerConfig(
		push, dispatch, dispatchInputs, pullRequest, schedules, repositoryDispatch,
		workflow.Environment,
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
	if strings.Contains(err.Error(), "unsupported container") || strings.Contains(err.Error(), "unsupported service") ||
		strings.Contains(err.Error(), "runner label") || strings.Contains(err.Error(), "runs-on") {
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
