package runner

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

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
