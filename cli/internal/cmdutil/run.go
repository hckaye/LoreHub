package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/api"
	"github.com/spf13/cobra"
)

type actionRun struct {
	ID            string     `json:"id"`
	WorkflowID    string     `json:"workflowId"`
	WorkflowName  string     `json:"workflowName"`
	WorkflowPath  string     `json:"workflowPath"`
	RunNumber     int64      `json:"runNumber"`
	RunAttempt    int        `json:"runAttempt"`
	RerunOf       *string    `json:"rerunOf,omitempty"`
	EventName     string     `json:"eventName"`
	Branch        string     `json:"branch"`
	Revision      string     `json:"revision"`
	ActorID       *string    `json:"actorId,omitempty"`
	Status        string     `json:"status"`
	Conclusion    *string    `json:"conclusion"`
	FailureReason *string    `json:"failureReason,omitempty"`
	QueuedAt      time.Time  `json:"queuedAt"`
	StartedAt     *time.Time `json:"startedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
}

type actionJob struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Conclusion   *string    `json:"conclusion"`
	Attempt      int        `json:"attempt"`
	LogAvailable bool       `json:"logAvailable"`
	QueuedAt     time.Time  `json:"queuedAt"`
	StartedAt    *time.Time `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

type actionArtifact struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type actionRunDetail struct {
	Run       actionRun        `json:"run"`
	Workflow  map[string]any   `json:"workflow"`
	Jobs      []actionJob      `json:"jobs"`
	Artifacts []actionArtifact `json:"artifacts"`
}

type actionRunPage struct {
	TotalCount int64       `json:"totalCount"`
	Runs       []actionRun `json:"runs"`
	Page       int         `json:"page"`
	PerPage    int         `json:"perPage"`
	HasMore    bool        `json:"hasMore"`
}

type runListFlags struct {
	event   string
	branch  string
	status  string
	page    int
	perPage int
}

func newRunCommand(state *rootState) *cobra.Command {
	runCommand := &cobra.Command{
		Use:   "run",
		Short: "Manage Actions workflow runs",
	}
	runCommand.AddCommand(
		newRunListCommand(state),
		newRunViewCommand(state),
		newRunWatchCommand(state),
		newRunMutationCommand(state, "cancel"),
		newRunMutationCommand(state, "rerun"),
	)
	return runCommand
}

func newRunMutationCommand(state *rootState, operation string) *cobra.Command {
	return &cobra.Command{
		Use:   operation + " NUMBER",
		Short: operation + " an Actions workflow run",
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
			var response actionRun
			path := methodPath(repository, "/actions/runs/"+number+"/"+operation)
			if err := postJSON(command.Context(), client, path, nil, &response); err != nil {
				return statusError(command, operation+" workflow run", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			pastTense := "Cancelled"
			if operation == "rerun" {
				pastTense = "Reran"
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "%s workflow run #%s\n", pastTense, number)
			return err
		},
	}
}

func newRunListCommand(state *rootState) *cobra.Command {
	var flags runListFlags
	command := &cobra.Command{
		Use:   "list",
		Short: "List Actions workflow runs",
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
			addStringQuery(values, "event", flags.event)
			addStringQuery(values, "branch", flags.branch)
			addStringQuery(values, "status", flags.status)
			if flags.page != 0 {
				values.Set("page", strconv.Itoa(flags.page))
			}
			if flags.perPage != 0 {
				values.Set("per_page", strconv.Itoa(flags.perPage))
			}
			var response actionRunPage
			if err := getJSON(command.Context(), client,
				queryPath(methodPath(repository, "/actions/runs"), values), &response); err != nil {
				return statusError(command, "list workflow runs", err)
			}
			return writeResource(command, state.json, response,
				[]string{"NUMBER", "WORKFLOW", "STATUS", "CONCLUSION", "BRANCH", "EVENT"},
				runRows(response.Runs))
		},
	}
	command.Flags().StringVar(&flags.event, "event", "", "filter by event")
	command.Flags().StringVar(&flags.branch, "branch", "", "filter by branch")
	command.Flags().StringVar(&flags.status, "status", "", "filter by status")
	command.Flags().IntVar(&flags.page, "page", 0, "page number")
	command.Flags().IntVar(&flags.perPage, "per-page", 0, "number of runs per page")
	return command
}

func newRunViewCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "view NUMBER",
		Short: "View an Actions workflow run",
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
			var response actionRunDetail
			if err := getJSON(command.Context(), client,
				methodPath(repository, "/actions/runs/"+number), &response); err != nil {
				return statusError(command, "get workflow run", err)
			}
			return writeRunDetail(command, state.json, response)
		},
	}
}

func newRunWatchCommand(state *rootState) *cobra.Command {
	var interval time.Duration
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "watch NUMBER",
		Short: "Watch an Actions workflow run until it finishes",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			number, err := checkNumber(args[0])
			if err != nil {
				return err
			}
			if interval <= 0 {
				return fmt.Errorf("interval must be positive")
			}
			if timeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			response, err := watchRun(command.Context(), client, repository, number, interval, timeout)
			if err != nil {
				return err
			}
			if err := writeRunDetail(command, state.json, response); err != nil {
				return err
			}
			conclusion := actionRunConclusion(response.Run)
			if conclusion != "success" {
				if conclusion == "" {
					conclusion = "unknown"
				}
				return fmt.Errorf("workflow run #%s concluded with %s", number, conclusion)
			}
			return nil
		},
	}
	command.Flags().DurationVar(&interval, "interval", 10*time.Second, "time between status checks")
	command.Flags().DurationVar(&interval, "poll-interval", 10*time.Second, "time between status checks")
	command.Flags().DurationVar(&timeout, "timeout", time.Hour, "maximum watch time")
	return command
}

func watchRun(
	ctx context.Context,
	client *api.Client,
	repository RepoContext,
	number string,
	interval time.Duration,
	timeout time.Duration,
) (actionRunDetail, error) {
	watchContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	path := methodPath(repository, "/actions/runs/"+number)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		var response actionRunDetail
		err := getJSON(watchContext, client, path, &response)
		if err != nil {
			if errors.Is(watchContext.Err(), context.DeadlineExceeded) {
				return actionRunDetail{}, fmt.Errorf("watch timed out after %s", timeout)
			}
			return actionRunDetail{}, fmt.Errorf("watch workflow run: %s", problemDetail(err))
		}
		if actionRunTerminal(response.Run) {
			return response, nil
		}
		select {
		case <-watchContext.Done():
			if errors.Is(watchContext.Err(), context.DeadlineExceeded) {
				return actionRunDetail{}, fmt.Errorf("watch timed out after %s", timeout)
			}
			return actionRunDetail{}, watchContext.Err()
		case <-ticker.C:
		}
	}
}

func actionRunTerminal(run actionRun) bool {
	return run.Status == "completed" || run.Status == "cancelled" || run.Conclusion != nil
}

func actionRunConclusion(run actionRun) string {
	if run.Conclusion == nil {
		return ""
	}
	return strings.TrimSpace(*run.Conclusion)
}

func runRows(runs []actionRun) [][]string {
	rows := make([][]string, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, []string{
			strconv.FormatInt(run.RunNumber, 10), run.WorkflowName, run.Status,
			actionRunConclusion(run), run.Branch, run.EventName,
		})
	}
	return rows
}

func writeRunDetail(command *cobra.Command, jsonOutput bool, detail actionRunDetail) error {
	if jsonOutput {
		return writeResource(command, true, detail, nil, nil)
	}
	rows := [][]string{
		{"Number", strconv.FormatInt(detail.Run.RunNumber, 10)},
		{"Workflow", detail.Run.WorkflowName},
		{"Status", detail.Run.Status},
		{"Conclusion", actionRunConclusion(detail.Run)},
		{"Branch", detail.Run.Branch},
		{"Event", detail.Run.EventName},
		{"Revision", detail.Run.Revision},
	}
	if err := writeResource(command, false, detail.Run, []string{"Field", "Value"}, rows); err != nil {
		return err
	}
	jobRows := make([][]string, 0, len(detail.Jobs))
	for _, job := range detail.Jobs {
		jobRows = append(jobRows, []string{job.Name, job.Status, conclusionText(job.Conclusion)})
	}
	if len(jobRows) > 0 {
		return writeResource(command, false, detail.Jobs, []string{"JOB", "STATUS", "CONCLUSION"}, jobRows)
	}
	return nil
}

func conclusionText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
