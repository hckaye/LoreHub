package collab

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

func makeQueryValues(raw string) url.Values {
	values, err := url.ParseQuery(raw)
	if err != nil {
		panic(err)
	}
	return values
}

func TestValidateTitle(t *testing.T) {
	t.Parallel()
	if _, err := validateTitle("   "); !errors.Is(err, ErrBlankBody) {
		t.Errorf("blank title: got %v, want ErrBlankBody", err)
	}
	if title, err := validateTitle("  Fix bug  "); err != nil || title != "Fix bug" {
		t.Errorf("trim title: got %q %v", title, err)
	}
	long := makeTitle(600)
	if _, err := validateTitle(long); !errors.Is(err, ErrTitleTooLong) {
		t.Errorf("long title: got %v, want ErrTitleTooLong", err)
	}
}

func TestValidateBody(t *testing.T) {
	t.Parallel()
	if _, err := validateBody("   ", true); !errors.Is(err, ErrBlankBody) {
		t.Errorf("blank required body: got %v, want ErrBlankBody", err)
	}
	if _, err := validateBody("   ", false); err != nil {
		t.Errorf("blank optional body should be allowed: %v", err)
	}
	big := makeBody(maxBodyBytes + 1)
	if _, err := validateBody(big, false); !errors.Is(err, ErrBodyTooLong) {
		t.Errorf("oversized body: got %v, want ErrBodyTooLong", err)
	}
}

func TestValidateIssueState(t *testing.T) {
	t.Parallel()
	if _, err := validateIssueState("open"); err != nil {
		t.Errorf("open: %v", err)
	}
	if _, err := validateIssueState("closed"); err != nil {
		t.Errorf("closed: %v", err)
	}
	if _, err := validateIssueState("merged"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("merged: got %v, want ErrInvalidState", err)
	}
}

func TestValidateMergeRequestState(t *testing.T) {
	t.Parallel()
	if _, err := validateMergeRequestState("open"); err != nil {
		t.Errorf("open: %v", err)
	}
	if _, err := validateMergeRequestState("merged"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("merged should be rejected: got %v", err)
	}
}

func TestValidateLabelInput(t *testing.T) {
	t.Parallel()
	valid, err := validateLabelInput(LabelInput{Name: "bug", Description: "stuff", Color: "FfFfFf"})
	if err != nil || valid.Name != "bug" || valid.Color != "FfFfFf" {
		t.Errorf("valid label: got %+v %v", valid, err)
	}
	if _, err := validateLabelInput(LabelInput{Name: "  bug  x  ", Color: "000000"}); err != nil {
		t.Errorf("normalized name: %v", err)
	}
	if _, err := validateLabelInput(LabelInput{Name: "bug", Color: "xyz"}); !errors.Is(err, ErrInvalidColor) {
		t.Errorf("bad color: got %v, want ErrInvalidColor", err)
	}
	if _, err := validateLabelInput(LabelInput{Name: "bug", Color: "12345"}); !errors.Is(err, ErrInvalidColor) {
		t.Errorf("short color: got %v, want ErrInvalidColor", err)
	}
	if _, err := validateLabelInput(LabelInput{Name: "", Color: "000000"}); !errors.Is(err, ErrInvalidLabel) {
		t.Errorf("empty name: got %v, want ErrInvalidLabel", err)
	}
	if _, err := validateLabelInput(LabelInput{
		Name: "bug", Description: makeBody(maxDescriptionLen + 1), Color: "000000",
	}); !errors.Is(err, ErrBodyTooLong) {
		t.Errorf("long description: got %v, want ErrBodyTooLong", err)
	}
}

func TestValidateReviewInput(t *testing.T) {
	t.Parallel()
	if _, err := validateReviewInput(ReviewInput{Decision: "approved"}); err != nil {
		t.Errorf("approved: %v", err)
	}
	if _, err := validateReviewInput(ReviewInput{Decision: "changes_requested"}); err != nil {
		t.Errorf("changes_requested: %v", err)
	}
	if _, err := validateReviewInput(ReviewInput{Decision: "commented"}); err != nil {
		t.Errorf("commented: %v", err)
	}
	if _, err := validateReviewInput(ReviewInput{Decision: "rejected"}); !errors.Is(err, ErrInvalidDecision) {
		t.Errorf("rejected: got %v, want ErrInvalidDecision", err)
	}
}

func TestValidateBranchRuleInput(t *testing.T) {
	t.Parallel()
	if _, err := validateBranchRuleInput(BranchRuleInput{Pattern: "main", RequiredApprovals: 2}); err != nil {
		t.Errorf("valid rule: %v", err)
	}
	if _, err := validateBranchRuleInput(BranchRuleInput{Pattern: "", RequiredApprovals: 0}); !errors.Is(err,
		ErrInvalidPattern) {
		t.Errorf("empty pattern: got %v, want ErrInvalidPattern", err)
	}
	if _, err := validateBranchRuleInput(BranchRuleInput{Pattern: "main", RequiredApprovals: -1}); !errors.Is(err,
		ErrInvalidApprovals) {
		t.Errorf("negative approvals: got %v, want ErrInvalidApprovals", err)
	}
	if _, err := validateBranchRuleInput(BranchRuleInput{Pattern: "main", RequiredApprovals: 101}); !errors.Is(err,
		ErrInvalidApprovals) {
		t.Errorf("too many approvals: got %v, want ErrInvalidApprovals", err)
	}
	if _, err := validateBranchRuleInput(BranchRuleInput{Pattern: "main\nx", RequiredApprovals: 0}); !errors.Is(err,
		ErrInvalidPattern) {
		t.Errorf("newline pattern: got %v, want ErrInvalidPattern", err)
	}
}

func TestParsePage(t *testing.T) {
	t.Parallel()
	page, offset, err := parsePage(nil)
	if err != nil || offset != 0 || page.Limit != defaultPageLimit {
		t.Errorf("default page: %+v %v", page, err)
	}
	values := makeQueryValues("limit=5&cursor=10")
	page, offset, err = parsePage(values)
	if err != nil || offset != 10 || page.Limit != 5 {
		t.Errorf("explicit page: %+v %v", page, err)
	}
	values = makeQueryValues("limit=500")
	page, _, err = parsePage(values)
	if err != nil || page.Limit != maxPageLimit {
		t.Errorf("clamped limit: %+v %v", page, err)
	}
	values = makeQueryValues("limit=0")
	if _, _, err := parsePage(values); err == nil {
		t.Error("zero limit should error")
	}
	values = makeQueryValues("cursor=-1")
	if _, _, err := parsePage(values); err == nil {
		t.Error("negative cursor should error")
	}
}

func TestParseIfMatch(t *testing.T) {
	t.Parallel()
	if _, ok := parseIfMatch(""); ok {
		t.Error("empty header should not be ok")
	}
	ts := time.Now().UTC().Round(time.Microsecond)
	encoded := ts.Format(time.RFC3339Nano)
	if got, ok := parseIfMatch(`"` + encoded + `"`); !ok || !got.Equal(ts) {
		t.Errorf("quoted if-match: got %v ok=%v", got, ok)
	}
	if got, ok := parseIfMatch(encoded); !ok || !got.Equal(ts) {
		t.Errorf("unquoted if-match: got %v ok=%v", got, ok)
	}
	if _, ok := parseIfMatch("not-a-time"); ok {
		t.Error("garbage if-match should not be ok")
	}
}

func TestMalformedIfMatchIsRejected(t *testing.T) {
	t.Parallel()
	if _, err := buildIssueUpdateInput(
		issuePatchRequest{Title: ptrString("new")}, " ",
	); !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("malformed issue If-Match: got %v", err)
	}
	if _, err := buildMergeRequestUpdateInput(
		mergeRequestPatchRequest{Title: ptrString("new")}, "not-a-time",
	); !errors.Is(err, ErrInvalidPrecondition) {
		t.Fatalf("malformed merge request If-Match: got %v", err)
	}
}

func TestEncodeCursor(t *testing.T) {
	t.Parallel()
	if cursor := encodeCursor(0, 30, 10); cursor != "" {
		t.Errorf("partial page cursor: got %q", cursor)
	}
	if cursor := encodeCursor(0, 30, 30); cursor != "30" {
		t.Errorf("full page cursor: got %q", cursor)
	}
}

func TestParseInt64(t *testing.T) {
	t.Parallel()
	if n, ok := parseInt64("42"); !ok || n != 42 {
		t.Errorf("42: got %d %v", n, ok)
	}
	if _, ok := parseInt64("abc"); ok {
		t.Error("abc should fail")
	}
	if _, ok := parseInt64(""); ok {
		t.Error("empty should fail")
	}
	if _, ok := parseInt64("-1"); ok {
		t.Error("negative should fail")
	}
}

func makeTitle(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}

func makeBody(n int) string {
	if n < 0 {
		n = 0
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = 'x'
	}
	return string(out)
}
