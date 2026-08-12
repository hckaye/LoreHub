package collab

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultRepositoryWorkItemPageSize = 25
	maxRepositoryWorkItemPageSize     = 100
	maxRepositoryWorkItemPage         = 10_000
	maxRepositoryWorkItemSearchRunes  = 256
	maxRepositoryFilterRunes          = 100
	maxRepositoryBranchFilterRunes    = 255
	maxRepositoryLabelFilters         = 20
)

func NormalizeRepositoryIssueQuery(query RepositoryIssueQuery) (RepositoryIssueQuery, error) {
	query.State = strings.TrimSpace(query.State)
	if query.State == "" {
		query.State = "open"
	}
	if query.State != "open" && query.State != "closed" && query.State != "all" {
		return RepositoryIssueQuery{}, invalidRepositoryWorkItemQuery("issue state is invalid")
	}
	normalized, err := normalizeRepositoryWorkItemQuery(
		query.Search, query.Author, query.Assignee, query.Labels,
		query.MilestoneNumber, query.WithoutMilestone,
		query.Sort, query.Direction, query.Page, query.PerPage,
	)
	if err != nil {
		return RepositoryIssueQuery{}, err
	}
	query.Search = normalized.search
	query.Author = normalized.author
	query.Assignee = normalized.assignee
	query.Labels = normalized.labels
	query.Sort = normalized.sort
	query.Direction = normalized.direction
	query.Page = normalized.page
	query.PerPage = normalized.perPage
	return query, nil
}

func NormalizeRepositoryMergeRequestQuery(
	query RepositoryMergeRequestQuery,
) (RepositoryMergeRequestQuery, error) {
	query.State = strings.TrimSpace(query.State)
	if query.State == "" {
		query.State = "open"
	}
	if query.State != "open" && query.State != "closed" &&
		query.State != "merged" && query.State != "all" {
		return RepositoryMergeRequestQuery{}, invalidRepositoryWorkItemQuery("pull request state is invalid")
	}
	normalized, err := normalizeRepositoryWorkItemQuery(
		query.Search, query.Author, query.Assignee, query.Labels,
		query.MilestoneNumber, query.WithoutMilestone,
		query.Sort, query.Direction, query.Page, query.PerPage,
	)
	if err != nil {
		return RepositoryMergeRequestQuery{}, err
	}
	query.Search = normalized.search
	query.Author = normalized.author
	query.Assignee = normalized.assignee
	query.Labels = normalized.labels
	query.Sort = normalized.sort
	query.Direction = normalized.direction
	query.Page = normalized.page
	query.PerPage = normalized.perPage
	query.SourceBranch = strings.TrimSpace(query.SourceBranch)
	query.TargetBranch = strings.TrimSpace(query.TargetBranch)
	if !validRepositoryFilter(query.SourceBranch, maxRepositoryBranchFilterRunes) ||
		!validRepositoryFilter(query.TargetBranch, maxRepositoryBranchFilterRunes) {
		return RepositoryMergeRequestQuery{}, invalidRepositoryWorkItemQuery("branch filter is invalid")
	}
	return query, nil
}

type normalizedRepositoryWorkItemQuery struct {
	search    string
	author    string
	assignee  string
	labels    []string
	sort      string
	direction string
	page      int
	perPage   int
}

func normalizeRepositoryWorkItemQuery(
	search string,
	author string,
	assignee string,
	labels []string,
	milestoneNumber *int64,
	withoutMilestone bool,
	sortName string,
	direction string,
	page int,
	perPage int,
) (normalizedRepositoryWorkItemQuery, error) {
	search = strings.TrimSpace(search)
	author = strings.TrimSpace(author)
	assignee = strings.TrimSpace(assignee)
	if !validRepositoryFilter(search, maxRepositoryWorkItemSearchRunes) ||
		!validRepositoryFilter(author, maxRepositoryFilterRunes) ||
		!validRepositoryFilter(assignee, maxRepositoryFilterRunes) {
		return normalizedRepositoryWorkItemQuery{}, invalidRepositoryWorkItemQuery("text filter is invalid")
	}
	if milestoneNumber != nil && (*milestoneNumber < 1 || withoutMilestone) {
		return normalizedRepositoryWorkItemQuery{}, invalidRepositoryWorkItemQuery("milestone filter is invalid")
	}
	labels, err := normalizeRepositoryLabels(labels)
	if err != nil {
		return normalizedRepositoryWorkItemQuery{}, err
	}
	sortName = strings.TrimSpace(sortName)
	if sortName == "" {
		sortName = "updated"
	}
	if sortName != "created" && sortName != "updated" && sortName != "comments" {
		return normalizedRepositoryWorkItemQuery{}, invalidRepositoryWorkItemQuery("sort is invalid")
	}
	direction = strings.TrimSpace(direction)
	if direction == "" {
		direction = "desc"
	}
	if direction != "asc" && direction != "desc" {
		return normalizedRepositoryWorkItemQuery{}, invalidRepositoryWorkItemQuery("direction is invalid")
	}
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = defaultRepositoryWorkItemPageSize
	}
	if page < 1 || page > maxRepositoryWorkItemPage ||
		perPage < 1 || perPage > maxRepositoryWorkItemPageSize {
		return normalizedRepositoryWorkItemQuery{}, invalidRepositoryWorkItemQuery("pagination is invalid")
	}
	return normalizedRepositoryWorkItemQuery{
		search: search, author: author, assignee: assignee, labels: labels,
		sort: sortName, direction: direction, page: page, perPage: perPage,
	}, nil
}

func normalizeRepositoryLabels(labels []string) ([]string, error) {
	if len(labels) > maxRepositoryLabelFilters {
		return nil, invalidRepositoryWorkItemQuery("too many label filters")
	}
	unique := make(map[string]string, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || !validRepositoryFilter(label, maxRepositoryFilterRunes) {
			return nil, invalidRepositoryWorkItemQuery("label filter is invalid")
		}
		key := strings.ToLower(label)
		if _, found := unique[key]; !found {
			unique[key] = label
		}
	}
	result := make([]string, 0, len(unique))
	for _, label := range unique {
		result = append(result, label)
	}
	sort.Slice(result, func(left, right int) bool {
		leftValue := strings.ToLower(result[left])
		rightValue := strings.ToLower(result[right])
		if leftValue == rightValue {
			return result[left] < result[right]
		}
		return leftValue < rightValue
	})
	return result, nil
}

func validRepositoryFilter(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func invalidRepositoryWorkItemQuery(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRepositoryWorkItemQuery, detail)
}
