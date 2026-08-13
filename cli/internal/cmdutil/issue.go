package cmdutil

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type issueListFlags struct {
	state     string
	author    string
	assignee  string
	labels    []string
	milestone string
	search    string
}

func newIssueCommand(state *rootState) *cobra.Command {
	issueCommand := &cobra.Command{
		Use:   "issue",
		Short: "Manage issues",
	}
	issueCommand.AddCommand(
		newIssueListCommand(state),
		newIssueViewCommand(state),
		newIssueCreateCommand(state),
		newIssueCommentCommand(state),
		newIssueStateCommand(state, "close", "closed"),
		newIssueStateCommand(state, "reopen", "open"),
	)
	return issueCommand
}

func newIssueListCommand(state *rootState) *cobra.Command {
	var flags issueListFlags
	command := &cobra.Command{
		Use:   "list",
		Short: "List issues",
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
			for _, label := range flags.labels {
				if strings.TrimSpace(label) != "" {
					values.Add("label", strings.TrimSpace(label))
				}
			}
			var response issuePage
			if err := getJSON(command.Context(), client, queryPath(methodPath(repository, "/issues"), values), &response); err != nil {
				return statusError(command, "list issues", err)
			}
			rows := make([][]string, 0, len(response.Issues))
			for _, item := range response.Issues {
				rows = append(rows, []string{strconv.FormatInt(item.Number, 10), item.Title, item.State,
					item.Author, strconv.FormatInt(item.CommentCount, 10)})
			}
			return writeResource(command, state.json, response,
				[]string{"NUMBER", "TITLE", "STATE", "AUTHOR", "COMMENTS"}, rows)
		},
	}
	command.Flags().StringVar(&flags.state, "state", "", "filter by state (open, closed, or all)")
	command.Flags().StringVar(&flags.author, "author", "", "filter by author")
	command.Flags().StringVar(&flags.assignee, "assignee", "", "filter by assignee")
	command.Flags().StringArrayVar(&flags.labels, "label", nil, "filter by label (repeatable)")
	command.Flags().StringVar(&flags.milestone, "milestone", "", "filter by milestone number")
	command.Flags().StringVarP(&flags.search, "search", "S", "", "search issue text")
	return command
}

func newIssueViewCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "view NUMBER",
		Short: "View an issue and its comments",
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
			var response issue
			if err := getJSON(command.Context(), client, methodPath(repository, "/issues/"+number), &response); err != nil {
				return statusError(command, "get issue", err)
			}
			var comments commentPage
			if err := getJSON(command.Context(), client, methodPath(repository, "/issues/"+number+"/comments"), &comments); err != nil {
				return statusError(command, "list issue comments", err)
			}
			view := issueView{issue: response, Comments: comments}
			if state.json {
				return state.writeJSON(view)
			}
			rows := [][]string{
				{"Number", strconv.FormatInt(response.Number, 10)},
				{"Title", response.Title},
				{"State", response.State},
				{"Author", response.Author},
				{"Body", response.Body},
			}
			if err := writeResource(command, false, response, []string{"Field", "Value"}, rows); err != nil {
				return err
			}
			commentRows := make([][]string, 0, len(comments.Items))
			for _, comment := range comments.Items {
				commentRows = append(commentRows, []string{comment.Author, comment.Body,
					comment.CreatedAt.UTC().Format("2006-01-02 15:04:05Z")})
			}
			return writeResource(command, false, comments,
				[]string{"COMMENTER", "BODY", "CREATED"}, commentRows)
		},
	}
}

func newIssueCreateCommand(state *rootState) *cobra.Command {
	var title string
	var body string
	command := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
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
			var response issue
			input := struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			}{Title: title, Body: body}
			if err := postJSON(command.Context(), client, methodPath(repository, "/issues"), input, &response); err != nil {
				return statusError(command, "create issue", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Created issue #%d\n", response.Number)
			return err
		},
	}
	command.Flags().StringVar(&title, "title", "", "issue title")
	command.Flags().StringVar(&body, "body", "", "issue body")
	_ = command.MarkFlagRequired("title")
	return command
}

func newIssueCommentCommand(state *rootState) *cobra.Command {
	var body string
	command := &cobra.Command{
		Use:   "comment NUMBER",
		Short: "Comment on an issue",
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
			var response issueComment
			if err := postJSON(command.Context(), client, methodPath(repository, "/issues/"+number+"/comments"),
				struct {
					Body string `json:"body"`
				}{Body: body}, &response); err != nil {
				return statusError(command, "comment on issue", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Commented on issue #%s\n", number)
			return err
		},
	}
	command.Flags().StringVar(&body, "body", "", "comment body")
	_ = command.MarkFlagRequired("body")
	return command
}

func newIssueStateCommand(state *rootState, verb string, targetState string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " NUMBER",
		Short: verb + " an issue",
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
			var response issue
			if err := patchJSON(command.Context(), client, methodPath(repository, "/issues/"+number),
				struct {
					State string `json:"state"`
				}{State: targetState}, &response); err != nil {
				return statusError(command, verb+" issue", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			label := strings.ToUpper(verb[:1]) + verb[1:]
			_, err = fmt.Fprintf(command.OutOrStdout(), "%s issue #%s\n", label, number)
			return err
		},
	}
}
