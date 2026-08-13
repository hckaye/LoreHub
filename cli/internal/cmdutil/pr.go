package cmdutil

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
)

type prListFlags struct {
	state     string
	author    string
	assignee  string
	labels    []string
	milestone string
	search    string
	source    string
	target    string
}

type mergeRequestComment struct {
	ID             string     `json:"id"`
	MergeRequestID string     `json:"mergeRequestId"`
	Author         string     `json:"author"`
	Body           string     `json:"body"`
	CreatedAt      time.Time  `json:"createdAt"`
	EditedAt       *time.Time `json:"editedAt"`
}

func newPRCommand(state *rootState) *cobra.Command {
	prCommand := &cobra.Command{
		Use:   "pr",
		Short: "Manage pull requests",
	}
	prCommand.AddCommand(
		newPRListCommand(state),
		newPRViewCommand(state),
		newPRCreateCommand(state),
		newPREditCommand(state),
		newPRCommentCommand(state),
		newPRStateCommand(state, "close", "closed"),
		newPRStateCommand(state, "reopen", "open"),
		newPRMergeCommand(state),
	)
	return prCommand
}

func newPREditCommand(state *rootState) *cobra.Command {
	var title string
	var body string
	command := &cobra.Command{
		Use:   "edit NUMBER",
		Short: "Edit a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			input := map[string]string{}
			if command.Flags().Changed("title") {
				input["title"] = title
			}
			if command.Flags().Changed("body") {
				input["body"] = body
			}
			if len(input) == 0 {
				return fmt.Errorf("at least one of --title or --body is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response mergeRequest
			if err := patchJSON(command.Context(), client,
				methodPath(repository, "/merge-requests/"+number), input, &response); err != nil {
				return statusError(command, "edit pull request", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Edited pull request #%s\n", number)
			return err
		},
	}
	command.Flags().StringVar(&title, "title", "", "new pull request title")
	command.Flags().StringVar(&body, "body", "", "new pull request body")
	return command
}

func newPRCommentCommand(state *rootState) *cobra.Command {
	var body string
	command := &cobra.Command{
		Use:   "comment NUMBER",
		Short: "Comment on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response mergeRequestComment
			if err := postJSON(command.Context(), client,
				methodPath(repository, "/merge-requests/"+number+"/comments"),
				map[string]string{"body": body}, &response); err != nil {
				return statusError(command, "comment on pull request", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Commented on pull request #%s\n", number)
			return err
		},
	}
	command.Flags().StringVar(&body, "body", "", "comment body")
	_ = command.MarkFlagRequired("body")
	return command
}

func newPRStateCommand(state *rootState, verb string, targetState string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " NUMBER",
		Short: verb + " a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response mergeRequest
			if err := patchJSON(command.Context(), client, methodPath(repository, "/merge-requests/"+number),
				map[string]string{"state": targetState}, &response); err != nil {
				return statusError(command, verb+" pull request", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			label := strings.ToUpper(verb[:1]) + verb[1:]
			_, err = fmt.Fprintf(command.OutOrStdout(), "%s pull request #%s\n", label, number)
			return err
		},
	}
}

func newPRListCommand(state *rootState) *cobra.Command {
	var flags prListFlags
	command := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			values := url.Values{}
			addStringQuery(values, "state", flags.state)
			addStringQuery(values, "author", flags.author)
			addStringQuery(values, "assignee", flags.assignee)
			addStringQuery(values, "milestone", flags.milestone)
			addStringQuery(values, "q", flags.search)
			addStringQuery(values, "source", flags.source)
			addStringQuery(values, "target", flags.target)
			for _, label := range flags.labels {
				if strings.TrimSpace(label) != "" {
					values.Add("label", strings.TrimSpace(label))
				}
			}
			var response mergeRequestPage
			if err := getJSON(command.Context(), client,
				queryPath(methodPath(repository, "/merge-requests"), values), &response); err != nil {
				return statusError(command, "list pull requests", err)
			}
			rows := make([][]string, 0, len(response.MergeRequests))
			for _, item := range response.MergeRequests {
				rows = append(rows, []string{strconv.FormatInt(item.Number, 10), item.Title, item.State,
					item.Author, item.SourceBranch, item.TargetBranch})
			}
			return writeResource(command, state.json, response,
				[]string{"NUMBER", "TITLE", "STATE", "AUTHOR", "SOURCE", "TARGET"}, rows)
		},
	}
	command.Flags().StringVar(&flags.state, "state", "", "filter by state (open, closed, merged, or all)")
	command.Flags().StringVar(&flags.author, "author", "", "filter by author")
	command.Flags().StringVar(&flags.assignee, "assignee", "", "filter by assignee")
	command.Flags().StringArrayVar(&flags.labels, "label", nil, "filter by label (repeatable)")
	command.Flags().StringVar(&flags.milestone, "milestone", "", "filter by milestone number")
	command.Flags().StringVarP(&flags.search, "search", "S", "", "search pull request text")
	command.Flags().StringVar(&flags.source, "source", "", "filter by source branch")
	command.Flags().StringVar(&flags.target, "target", "", "filter by target branch")
	return command
}

func newPRViewCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "view NUMBER",
		Short: "View a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response mergeRequest
			if err := getJSON(command.Context(), client,
				methodPath(repository, "/merge-requests/"+number), &response); err != nil {
				return statusError(command, "get pull request", err)
			}
			rows := [][]string{
				{"Number", strconv.FormatInt(response.Number, 10)},
				{"Title", response.Title},
				{"State", response.State},
				{"Author", response.Author},
				{"Source", response.SourceBranch},
				{"Target", response.TargetBranch},
				{"Body", response.Body},
			}
			return writeResource(command, state.json, response, []string{"Field", "Value"}, rows)
		},
	}
}

func newPRCreateCommand(state *rootState) *cobra.Command {
	var title string
	var body string
	var source string
	var target string
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response mergeRequest
			input := struct {
				Title        string `json:"title"`
				Body         string `json:"body"`
				SourceBranch string `json:"sourceBranch"`
				TargetBranch string `json:"targetBranch"`
			}{Title: title, Body: body, SourceBranch: source, TargetBranch: target}
			path := methodPath(repository, "/merge-requests")
			if err := postJSON(command.Context(), client, path, input, &response); err != nil {
				return statusError(command, "create pull request", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Created pull request #%d\n", response.Number)
			return err
		},
	}
	command.Flags().StringVar(&title, "title", "", "pull request title")
	command.Flags().StringVar(&body, "body", "", "pull request body")
	command.Flags().StringVar(&source, "source", "", "source branch")
	command.Flags().StringVar(&target, "target", "", "target branch")
	_ = command.MarkFlagRequired("title")
	_ = command.MarkFlagRequired("source")
	_ = command.MarkFlagRequired("target")
	return command
}

func newPRMergeCommand(state *rootState) *cobra.Command {
	var timeout time.Duration
	var pollInterval time.Duration
	command := &cobra.Command{
		Use:   "merge NUMBER",
		Short: "Merge a pull request when it is ready",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			if timeout <= 0 || pollInterval <= 0 {
				return fmt.Errorf("timeout and poll interval must be positive")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			mergeContext, cancel := context.WithTimeout(command.Context(), timeout)
			defer cancel()
			readinessPath := methodPath(repository, "/merge-requests/"+number+"/merge-readiness")
			var readiness mergeReadiness
			if err := getJSON(mergeContext, client, readinessPath, &readiness); err != nil {
				return statusError(command, "check merge readiness", err)
			}
			if !readiness.Ready {
				if err := printMergeBlockers(command, readiness.Blockers); err != nil {
					return err
				}
				return fmt.Errorf("merge is not ready")
			}

			operation := readiness.Operation
			startPath := methodPath(repository, "/merge-requests/"+number+"/merge/start")
			var started mergeOperation
			if err := postJSON(mergeContext, client, startPath, nil, &started); err != nil {
				return statusError(command, "start merge", err)
			}
			operation = &started

			operation, err = waitForMergeOperation(mergeContext, client, repository, number, operation, pollInterval)
			if err != nil {
				if mergeContext.Err() != nil {
					return fmt.Errorf("merge timed out after %s", timeout)
				}
				return err
			}
			if operation.State == "pushed" || operation.State == "merged" {
				if state.json {
					return state.writeJSON(operation)
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "Pull request #%s is already merged\n", number)
				return err
			}

			pushPath := methodPath(repository, "/merge-requests/"+number+"/merge/push")
			var merged any
			if err := postJSON(mergeContext, client, pushPath, nil, &merged); err != nil {
				return statusError(command, "push merge", err)
			}
			if state.json {
				return state.writeJSON(merged)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Merged pull request #%s\n", number)
			return err
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "maximum time to wait for the merge")
	command.Flags().DurationVar(&pollInterval, "poll-interval", 250*time.Millisecond,
		"time between merge operation checks")
	return command
}

func waitForMergeOperation(
	ctx context.Context,
	client interface {
		GetJSON(context.Context, string, any) error
	},
	repository RepoContext,
	number string,
	operation *mergeOperation,
	pollInterval time.Duration,
) (*mergeOperation, error) {
	if operation == nil {
		operation = &mergeOperation{}
	}
	firstCheck := true
	for {
		switch operation.State {
		case "ready_to_push":
			if len(operation.ConflictPaths) > 0 {
				return nil, mergeOperationError(operation)
			}
			return operation, nil
		case "pushed", "merged":
			return operation, nil
		case "conflicts", "aborted":
			return nil, mergeOperationError(operation)
		}

		if !firstCheck {
			timer := time.NewTimer(pollInterval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		firstCheck = false
		var current mergeOperation
		path := methodPath(repository, "/merge-requests/"+number+"/merge-operation")
		if err := client.GetJSON(ctx, path, &current); err != nil {
			return nil, statusError(nil, "get merge operation", err)
		}
		operation = &current
	}
}

func mergeOperationError(operation *mergeOperation) error {
	detail := operation.ErrorDetail
	if detail == "" {
		detail = "the merge operation did not complete"
	}
	if len(operation.ConflictPaths) > 0 {
		detail += ": unresolved conflicts"
	}
	return fmt.Errorf("merge blocked (%s): %s", operation.State, detail)
}

func printMergeBlockers(command *cobra.Command, blockers []mergeBlocker) error {
	rows := make([][]string, 0, len(blockers))
	for _, blocker := range blockers {
		rows = append(rows, []string{blocker.Code, blocker.Detail})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"not_ready", "The pull request cannot be merged yet"})
	}
	return text.NewWriter(command.OutOrStdout()).Table([]string{"BLOCKER", "DETAIL"}, rows)
}
