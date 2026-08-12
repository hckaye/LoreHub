package merge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

type unsuccessfulStatusCheck struct {
	Context string
	State   string
}

func evaluateRequiredStatusChecks(
	checks []collab.RevisionStatusCheck,
	required []string,
) ([]collab.RevisionStatusCheck, []unsuccessfulStatusCheck) {
	requiredSet := make(map[string]struct{}, len(required))
	for _, contextName := range required {
		requiredSet[strings.ToLower(contextName)] = struct{}{}
	}
	states := make(map[string]string, len(checks))
	result := make([]collab.RevisionStatusCheck, len(checks))
	copy(result, checks)
	for index := range result {
		key := strings.ToLower(result[index].Context)
		_, result[index].Required = requiredSet[key]
		states[key] = result[index].State
	}
	sort.SliceStable(result, func(left, right int) bool {
		return strings.ToLower(result[left].Context) < strings.ToLower(result[right].Context)
	})

	unsuccessful := make([]unsuccessfulStatusCheck, 0)
	for _, contextName := range required {
		state, found := states[strings.ToLower(contextName)]
		if !found {
			state = "missing"
		}
		if state != "success" {
			unsuccessful = append(unsuccessful, unsuccessfulStatusCheck{
				Context: contextName,
				State:   state,
			})
		}
	}
	return result, unsuccessful
}

func requiredStatusChecksBlocker(checks []unsuccessfulStatusCheck) (collab.MergeBlocker, bool) {
	if len(checks) == 0 {
		return collab.MergeBlocker{}, false
	}
	values := make([]string, 0, len(checks))
	for _, check := range checks {
		values = append(values, fmt.Sprintf("%s (%s)", check.Context, check.State))
	}
	return collab.MergeBlocker{
		Code:   "required_status_checks",
		Detail: "Required status checks are not successful: " + strings.Join(values, ", "),
	}, true
}
