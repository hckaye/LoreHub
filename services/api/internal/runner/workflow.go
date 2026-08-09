package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

func AdaptWorkflows(workspace string) (int, error) {
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	entries, err := os.ReadDir(workflowDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read workflow directory: %w", err)
	}

	actionDirectory := filepath.Join(workspace, ".lorehub", "actions", "checkout")
	if err := os.MkdirAll(actionDirectory, 0o750); err != nil {
		return 0, fmt.Errorf("create checkout adapter directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(actionDirectory, "action.yml"), []byte(checkoutAction), 0o600); err != nil {
		return 0, fmt.Errorf("write checkout adapter: %w", err)
	}

	replacements := 0
	for _, entry := range entries {
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.IsDir() || extension != ".yml" && extension != ".yaml" {
			continue
		}
		path := filepath.Join(workflowDirectory, entry.Name())
		count, err := adaptWorkflow(path)
		if err != nil {
			return 0, err
		}
		replacements += count
	}
	return replacements, nil
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
