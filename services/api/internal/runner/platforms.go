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

func validateWorkflowRunnerLabels(path string, images map[string]string) error {
	labels, err := DiscoverRunnerLabels(path)
	if err != nil {
		return err
	}
	if err := ValidateRunnerPlatformImages(images); err != nil {
		return err
	}
	for _, label := range labels {
		if _, ok := images[label]; !ok {
			return fmt.Errorf("workflow runner label %q is not mapped for act", label)
		}
	}
	return nil
}

func runnerLabelsFromJobs(jobs *yaml.Node) ([]string, error) {
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return nil, errors.New("workflow must define jobs before resolving runner labels")
	}
	labels := make([]string, 0, len(jobs.Content)/2)
	seen := make(map[string]struct{})
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		jobID := jobs.Content[index].Value
		job := jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("job %q must be a map", jobID)
		}
		runsOn := mappingValue(job, "runs-on")
		if runsOn == nil || runsOn.Kind != yaml.ScalarNode || isNullNode(runsOn) {
			return nil, fmt.Errorf("job %q runs-on must be one literal runner label", jobID)
		}
		label, err := scalarString(runsOn, fmt.Sprintf("job %q runs-on", jobID))
		if err != nil || !runnerLabelPattern.MatchString(label) {
			return nil, fmt.Errorf("job %q runs-on must be one literal runner label", jobID)
		}
		if _, ok := seen[label]; !ok {
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	return labels, nil
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
	jobs := mappingValue(root, "jobs")
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
		if valueErr != nil || strings.Contains(value, "${{") {
			return "", errors.New("job environment must be one literal name")
		}
		if environment != "" && environment != value {
			return "", errors.New("multiple job environments are not supported by one act execution")
		}
		environment = value
	}
	return environment, nil
}
