package runner

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultUbuntuLatestImage = "ghcr.io/catthehacker/ubuntu:act-24.04"

var runnerLabelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func DefaultRunnerPlatformImages() map[string]string {
	return map[string]string{"ubuntu-latest": DefaultUbuntuLatestImage}
}

func ValidateRunnerPlatformImages(images map[string]string) error {
	if len(images) == 0 {
		return errors.New("at least one act runner platform mapping is required")
	}
	for label, image := range images {
		if !runnerLabelPattern.MatchString(label) {
			return fmt.Errorf("runner label %q is invalid", label)
		}
		if !validContainerImageReference(image) {
			return fmt.Errorf("runner image for label %q is invalid", label)
		}
	}
	return nil
}

func validContainerImageReference(image string) bool {
	if image == "" || strings.TrimSpace(image) != image || strings.ContainsAny(image, "\r\n\t ;'\"$`()") {
		return false
	}
	if strings.Contains(image, "@") || !strings.Contains(image, ":") {
		return false
	}
	return true
}

func mergedRunnerPlatformImages(images map[string]string) (map[string]string, error) {
	merged := DefaultRunnerPlatformImages()
	for label, image := range images {
		merged[label] = image
	}
	if err := ValidateRunnerPlatformImages(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func DiscoverRunnerLabels(workflowPath string) ([]string, error) {
	contents, err := os.ReadFile(workflowPath)
	if err != nil {
		return nil, fmt.Errorf("read workflow runner labels: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("parse workflow runner labels: %w", err)
	}
	root, err := documentRoot(&document)
	if err != nil {
		return nil, err
	}
	return runnerLabelsFromJobs(mappingValue(root, "jobs"))
}

func validateWorkflowRunnerLabels(path string, images map[string]string) ([]string, error) {
	jobs, err := workflowJobDefinitionsFromFile(path, images)
	if err != nil {
		return nil, err
	}
	return combinedRunnerLabels(jobs), nil
}

func runnerLabelsFromJobs(jobs *yaml.Node) ([]string, error) {
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return nil, errors.New("workflow must define jobs before resolving runner labels")
	}
	allLabels := make([]string, 0)
	seen := make(map[string]struct{})
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		jobID := jobs.Content[index].Value
		job := jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("job %q must be a map", jobID)
		}
		runsOn := mappingValue(job, "runs-on")
		jobLabels, err := normalizedRunsOnLabels(runsOn, jobID)
		if err != nil {
			return nil, err
		}
		for _, label := range jobLabels {
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			allLabels = append(allLabels, label)
		}
	}
	sort.Strings(allLabels)
	return allLabels, nil
}

func workflowJobDefinitionsFromFile(path string, images map[string]string) ([]WorkflowJobDefinition, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow jobs: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("parse workflow jobs: %w", err)
	}
	root, err := documentRoot(&document)
	if err != nil {
		return nil, err
	}
	return workflowJobDefinitions(mappingValue(root, "jobs"), images)
}

func workflowJobDefinitions(jobs *yaml.Node, images map[string]string) ([]WorkflowJobDefinition, error) {
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return nil, errors.New("workflow must define at least one job")
	}
	if err := ValidateRunnerPlatformImages(images); err != nil {
		return nil, err
	}
	definitions := make([]WorkflowJobDefinition, 0, len(jobs.Content)/2)
	jobNames := make(map[string]struct{}, len(jobs.Content)/2)
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		jobName, err := scalarString(jobs.Content[index], "workflow job name")
		if err != nil || len(jobName) > 255 || strings.ContainsAny(jobName, "\x00\r\n") {
			return nil, errors.New("workflow job name must be a literal string of at most 255 characters")
		}
		if _, duplicate := jobNames[jobName]; duplicate {
			return nil, fmt.Errorf("workflow job %q is defined more than once", jobName)
		}
		jobNames[jobName] = struct{}{}
		job := jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("job %q must be a map", jobName)
		}
		labels, err := normalizedRunsOnLabels(mappingValue(job, "runs-on"), jobName)
		if err != nil {
			return nil, err
		}
		if err := validateJobRunnerPlatform(jobName, labels, images); err != nil {
			return nil, err
		}
		needs, err := jobNeeds(jobName, mappingValue(job, "needs"))
		if err != nil {
			return nil, err
		}
		environment, err := jobEnvironment(jobName, mappingValue(job, "environment"))
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, WorkflowJobDefinition{
			JobName: jobName, RunnerLabels: labels, Needs: needs, Environment: environment,
		})
	}
	if err := validateWorkflowJobDependencies(definitions, jobNames); err != nil {
		return nil, err
	}
	return definitions, nil
}

func validateJobRunnerPlatform(jobName string, labels []string, images map[string]string) error {
	if containsRunnerLabel(labels, "self-hosted") {
		return nil
	}
	if len(labels) != 1 {
		return fmt.Errorf("managed job %q runs-on must use one platform label", jobName)
	}
	if _, ok := images[labels[0]]; !ok {
		return fmt.Errorf("workflow runner label %q is not mapped for act", labels[0])
	}
	return nil
}

func jobNeeds(jobName string, node *yaml.Node) ([]string, error) {
	if node == nil || isNullNode(node) {
		return []string{}, nil
	}
	values := make([]string, 0, 1)
	switch node.Kind {
	case yaml.ScalarNode:
		value, err := scalarString(node, fmt.Sprintf("job %q needs", jobName))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	case yaml.SequenceNode:
		for _, item := range node.Content {
			value, err := scalarString(item, fmt.Sprintf("job %q needs entry", jobName))
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	default:
		return nil, fmt.Errorf("job %q needs must contain literal job names", jobName)
	}
	needs := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(value) > 255 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("job %q needs contains an invalid job name", jobName)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		needs = append(needs, value)
	}
	return needs, nil
}

func jobEnvironment(jobName string, node *yaml.Node) (string, error) {
	if node == nil || isNullNode(node) {
		return "", nil
	}
	if node.Kind == yaml.MappingNode {
		node = mappingValue(node, "name")
	}
	value, err := scalarString(node, fmt.Sprintf("job %q environment", jobName))
	if err != nil || strings.Contains(value, "${{") || !validActionsEnvironmentName(value) {
		return "", fmt.Errorf("job %q environment must be one literal name", jobName)
	}
	return value, nil
}

func validateWorkflowJobDependencies(
	jobs []WorkflowJobDefinition,
	jobNames map[string]struct{},
) error {
	dependencies := make(map[string][]string, len(jobs))
	for _, job := range jobs {
		dependencies[job.JobName] = job.Needs
		for _, dependency := range job.Needs {
			if _, ok := jobNames[dependency]; !ok {
				return fmt.Errorf("job %q needs unknown job %q", job.JobName, dependency)
			}
		}
	}
	state := make(map[string]uint8, len(jobs))
	var visit func(string) error
	visit = func(jobName string) error {
		if state[jobName] == 1 {
			return fmt.Errorf("workflow job dependency cycle includes %q", jobName)
		}
		if state[jobName] == 2 {
			return nil
		}
		state[jobName] = 1
		for _, dependency := range dependencies[jobName] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[jobName] = 2
		return nil
	}
	for _, job := range jobs {
		if err := visit(job.JobName); err != nil {
			return err
		}
	}
	return nil
}

func combinedRunnerLabels(jobs []WorkflowJobDefinition) []string {
	seen := make(map[string]struct{})
	labels := make([]string, 0)
	for _, job := range jobs {
		for _, label := range job.RunnerLabels {
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	return labels
}

func commonWorkflowEnvironment(jobs []WorkflowJobDefinition) string {
	environment := ""
	for _, job := range jobs {
		if job.Environment == "" {
			continue
		}
		if environment != "" && !strings.EqualFold(environment, job.Environment) {
			return ""
		}
		environment = job.Environment
	}
	return environment
}

func workflowJobForExecution(
	path string,
	jobName string,
	images map[string]string,
) (WorkflowJobDefinition, error) {
	if jobName == "" {
		labels, err := validateWorkflowRunnerLabels(path, images)
		if err != nil {
			return WorkflowJobDefinition{}, err
		}
		environment, err := workflowEnvironmentName(path)
		if err != nil {
			return WorkflowJobDefinition{}, err
		}
		return WorkflowJobDefinition{RunnerLabels: labels, Needs: []string{}, Environment: environment}, nil
	}
	jobs, err := workflowJobDefinitionsFromFile(path, images)
	if err != nil {
		return WorkflowJobDefinition{}, err
	}
	for _, job := range jobs {
		if job.JobName == jobName {
			return job, nil
		}
	}
	return WorkflowJobDefinition{}, fmt.Errorf("workflow job %q no longer exists", jobName)
}

func normalizedRunsOnLabels(node *yaml.Node, jobID string) ([]string, error) {
	if node == nil || isNullNode(node) {
		return nil, fmt.Errorf("job %q runs-on must contain literal runner labels", jobID)
	}
	values := make([]string, 0, 1)
	switch node.Kind {
	case yaml.ScalarNode:
		value, err := scalarString(node, fmt.Sprintf("job %q runs-on", jobID))
		if err != nil {
			return nil, fmt.Errorf("job %q runs-on must contain literal runner labels", jobID)
		}
		values = append(values, value)
	case yaml.SequenceNode:
		for _, item := range node.Content {
			value, err := scalarString(item, fmt.Sprintf("job %q runs-on label", jobID))
			if err != nil {
				return nil, fmt.Errorf("job %q runs-on must contain literal runner labels", jobID)
			}
			values = append(values, value)
		}
	default:
		return nil, fmt.Errorf("job %q runs-on must contain literal runner labels", jobID)
	}
	if len(values) == 0 || len(values) > 100 {
		return nil, fmt.Errorf("job %q runs-on must contain 1 to 100 literal runner labels", jobID)
	}
	labels := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		label := strings.ToLower(strings.TrimSpace(value))
		if !runnerLabelPattern.MatchString(label) {
			return nil, fmt.Errorf("job %q runs-on label %q is invalid", jobID, value)
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels, nil
}

func equalRunnerLabels(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsRunnerLabel(labels []string, expected string) bool {
	for _, label := range labels {
		if label == expected {
			return true
		}
	}
	return false
}

func workflowEnvironmentName(workflowPath string) (string, error) {
	contents, err := os.ReadFile(workflowPath)
	if err != nil {
		return "", fmt.Errorf("read workflow environment: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return "", fmt.Errorf("parse workflow environment: %w", err)
	}
	root, err := documentRoot(&document)
	if err != nil {
		return "", err
	}
	return workflowEnvironmentFromJobs(mappingValue(root, "jobs"))
}

func workflowEnvironmentFromJobs(jobs *yaml.Node) (string, error) {
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return "", errors.New("workflow jobs are required for environment resolution")
	}
	environment := ""
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		job := jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return "", errors.New("workflow job is not a map")
		}
		node := mappingValue(job, "environment")
		if node == nil || isNullNode(node) {
			continue
		}
		if node.Kind == yaml.MappingNode {
			node = mappingValue(node, "name")
		}
		value, valueErr := scalarString(node, "job environment")
		if valueErr != nil || strings.Contains(value, "${{") || !validActionsEnvironmentName(value) {
			return "", errors.New("job environment must be one literal name")
		}
		if environment != "" && !strings.EqualFold(environment, value) {
			return "", errors.New("multiple job environments are not supported by one act execution")
		}
		environment = value
	}
	return environment, nil
}
