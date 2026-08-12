package collab

import (
	"fmt"
	"strings"
)

type repositoryQueryBuilder struct {
	conditions []string
	arguments  []any
}

func newRepositoryQueryBuilder(repositoryID string) *repositoryQueryBuilder {
	return &repositoryQueryBuilder{arguments: []any{repositoryID}}
}

func (builder *repositoryQueryBuilder) bind(value any) string {
	builder.arguments = append(builder.arguments, value)
	return fmt.Sprintf("$%d", len(builder.arguments))
}

func (builder *repositoryQueryBuilder) add(condition string) {
	builder.conditions = append(builder.conditions, condition)
}

func (builder *repositoryQueryBuilder) where() string {
	if len(builder.conditions) == 0 {
		return ""
	}
	return " AND " + strings.Join(builder.conditions, " AND ")
}

func repositorySearchPattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return "%" + value + "%"
}

func repositoryWorkItemOrder(alias string, sortName string, direction string) string {
	column := alias + ".updated_at"
	switch sortName {
	case "created":
		column = alias + ".created_at"
	case "comments":
		column = "comment_count"
	}
	order := "DESC"
	if direction == "asc" {
		order = "ASC"
	}
	return column + " " + order + ", " + alias + ".id " + order
}
