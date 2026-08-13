package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseTriggers(node *yaml.Node) (
	*PushTrigger,
	bool,
	map[string]WorkflowDispatchInput,
	*PullRequestTrigger,
	[]ScheduleTrigger,
	*RepositoryDispatchTrigger,
	error,
) {
	push := (*PushTrigger)(nil)
	dispatch := false
	var dispatchInputs map[string]WorkflowDispatchInput
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
			parsed, err := parseWorkflowDispatch(value)
			if err != nil {
				return err
			}
			dispatchInputs = parsed
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
			return nil, false, nil, nil, nil, nil, err
		}
		if err := visit(name, nil); err != nil {
			return nil, false, nil, nil, nil, nil, err
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return nil, false, nil, nil, nil, nil, errors.New("workflow event list must not be empty")
		}
		for _, item := range node.Content {
			name, err := scalarString(item, "workflow event")
			if err != nil {
				return nil, false, nil, nil, nil, nil, err
			}
			if err := visit(name, nil); err != nil {
				return nil, false, nil, nil, nil, nil, err
			}
		}
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return nil, false, nil, nil, nil, nil, errors.New("workflow event map must not be empty")
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			name, err := scalarString(node.Content[index], "workflow event")
			if err != nil {
				return nil, false, nil, nil, nil, nil, err
			}
			if err := visit(name, node.Content[index+1]); err != nil {
				return nil, false, nil, nil, nil, nil, err
			}
		}
	default:
		return nil, false, nil, nil, nil, nil, errors.New("workflow on must be a scalar, list, or map")
	}
	return push, dispatch, dispatchInputs, pullRequest, schedules, repositoryDispatch, nil
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

func parseWorkflowDispatch(node *yaml.Node) (map[string]WorkflowDispatchInput, error) {
	inputsConfig := make(map[string]WorkflowDispatchInput)
	if node == nil || isNullNode(node) {
		return inputsConfig, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("workflow_dispatch trigger must be a map")
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if key != "inputs" {
			return nil, fmt.Errorf("unsupported workflow_dispatch field %q", key)
		}
		inputs := node.Content[index+1]
		if inputs.Kind != yaml.MappingNode || isNullNode(inputs) {
			return nil, errors.New("workflow_dispatch inputs must be a map")
		}
		for inputIndex := 0; inputIndex+1 < len(inputs.Content); inputIndex += 2 {
			name := inputs.Content[inputIndex].Value
			if !validWorkflowInputName(name) {
				return nil, fmt.Errorf("workflow_dispatch input name %q is invalid", name)
			}
			input := inputs.Content[inputIndex+1]
			if input.Kind != yaml.MappingNode {
				return nil, errors.New("workflow_dispatch input definitions must be maps")
			}
			definition := WorkflowDispatchInput{Type: "string"}
			for fieldIndex := 0; fieldIndex+1 < len(input.Content); fieldIndex += 2 {
				field := input.Content[fieldIndex].Value
				value := input.Content[fieldIndex+1]
				switch field {
				case "description":
					parsed, err := workflowInputScalar(value, "workflow_dispatch input description")
					if err != nil {
						return nil, err
					}
					definition.Description = parsed
				case "required":
					parsed, err := parseBooleanScalar(value, "workflow_dispatch input required")
					if err != nil {
						return nil, err
					}
					definition.Required = parsed
				case "default":
					parsed, err := workflowInputScalar(value, "workflow_dispatch input default")
					if err != nil {
						return nil, err
					}
					definition.Default = &parsed
				case "type":
					parsed, err := scalarString(value, "workflow_dispatch input type")
					if err != nil {
						return nil, err
					}
					definition.Type = strings.ToLower(parsed)
				case "options":
					parsed, err := parseWorkflowInputOptions(value)
					if err != nil {
						return nil, err
					}
					definition.Options = parsed
				default:
					return nil, fmt.Errorf("unsupported workflow_dispatch input field %q", field)
				}
			}
			if err := validateWorkflowDispatchInput(name, definition); err != nil {
				return nil, err
			}
			inputsConfig[name] = definition
		}
	}
	return inputsConfig, nil
}

func validWorkflowInputName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9' && index > 0) || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func workflowInputScalar(node *yaml.Node, field string) (string, error) {
	if node == nil || node.Kind != yaml.ScalarNode || isNullNode(node) {
		return "", fmt.Errorf("%s must be a scalar", field)
	}
	return node.Value, nil
}

func parseBooleanScalar(node *yaml.Node, field string) (bool, error) {
	value, err := workflowInputScalar(node, field)
	if err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value)))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", field)
	}
	return parsed, nil
}

func parseWorkflowInputOptions(node *yaml.Node) ([]string, error) {
	if node == nil || node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, errors.New("workflow_dispatch input options must be a non-empty string list")
	}
	options := make([]string, 0, len(node.Content))
	seen := make(map[string]struct{}, len(node.Content))
	for _, item := range node.Content {
		value, err := scalarString(item, "workflow_dispatch input option")
		if err != nil {
			return nil, err
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("workflow_dispatch input option %q is duplicated", value)
		}
		seen[value] = struct{}{}
		options = append(options, value)
	}
	return options, nil
}

func validateWorkflowDispatchInput(name string, definition WorkflowDispatchInput) error {
	switch definition.Type {
	case "string", "boolean", "choice", "number", "environment":
	default:
		return fmt.Errorf("workflow_dispatch input %q has unsupported type %q", name, definition.Type)
	}
	if definition.Type == "choice" && len(definition.Options) == 0 {
		return fmt.Errorf("workflow_dispatch choice input %q must define options", name)
	}
	if definition.Type != "choice" && len(definition.Options) > 0 {
		return fmt.Errorf("workflow_dispatch input %q options require type choice", name)
	}
	if definition.Default == nil {
		return nil
	}
	if err := validateWorkflowInputValue(name, definition, *definition.Default); err != nil {
		return fmt.Errorf("workflow_dispatch input %q has an invalid default: %w", name, err)
	}
	return nil
}

func validateWorkflowInputValue(name string, definition WorkflowDispatchInput, value string) error {
	switch definition.Type {
	case "boolean":
		if _, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(value))); err != nil {
			return errors.New("value must be true or false")
		}
	case "number":
		if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil {
			return errors.New("value must be a number")
		}
	case "choice":
		for _, option := range definition.Options {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("value %q is not one of the configured options", value)
	case "string", "environment":
	default:
		return fmt.Errorf("input %q has unsupported type %q", name, definition.Type)
	}
	return nil
}

func ResolveWorkflowDispatchInputs(
	definition WorkflowDefinition,
	submitted map[string]string,
) (map[string]string, error) {
	if !definition.WorkflowDispatch {
		return nil, errors.New("workflow does not support workflow_dispatch")
	}
	resolved := make(map[string]string, len(definition.DispatchInputs))
	for name, input := range submitted {
		inputDefinition, ok := definition.DispatchInputs[name]
		if !ok {
			return nil, fmt.Errorf("workflow_dispatch input %q is not defined", name)
		}
		if err := validateWorkflowInputValue(name, inputDefinition, input); err != nil {
			return nil, fmt.Errorf("workflow_dispatch input %q is invalid: %w", name, err)
		}
		if inputDefinition.Required && strings.TrimSpace(input) == "" {
			return nil, fmt.Errorf("workflow_dispatch input %q is required", name)
		}
		resolved[name] = input
	}
	for name, definition := range definition.DispatchInputs {
		if _, ok := resolved[name]; ok {
			continue
		}
		if definition.Default != nil {
			resolved[name] = *definition.Default
			continue
		}
		if definition.Required {
			return nil, fmt.Errorf("workflow_dispatch input %q is required", name)
		}
	}
	return resolved, nil
}

func encodeTriggerConfig(
	push *PushTrigger,
	dispatch bool,
	dispatchInputs map[string]WorkflowDispatchInput,
	pullRequest *PullRequestTrigger,
	schedules []ScheduleTrigger,
	repositoryDispatch *RepositoryDispatchTrigger,
	environment string,
	runnerLabels []string,
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
		config["workflow_dispatch"] = WorkflowDispatchConfig{Inputs: dispatchInputs}
	}
	if environment != "" {
		config["environment"] = environment
	}
	if len(runnerLabels) > 0 {
		config["runner_labels"] = runnerLabels
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode workflow triggers: %w", err)
	}
	return encoded, nil
}
