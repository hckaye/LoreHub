package merge

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type recordingPushAuthorizer struct {
	input loreclient.PushAuthorization
	count int
	err   error
}

func (authorizer *recordingPushAuthorizer) AuthorizeLoreMergePush(
	_ context.Context,
	input loreclient.PushAuthorization,
) error {
	authorizer.input = input
	authorizer.count++
	return authorizer.err
}

func TestFixedPushAuthorizerPinsRequestActorRepositoryAndOperation(t *testing.T) {
	recorder := &recordingPushAuthorizer{}
	api := &API{pushAuth: recorder}
	actor := platform.User{ID: "actor-1"}
	repository := collab.Repository{ID: "repository-1"}
	authorizer := api.fixedPushAuthorizer(actor, repository, "operation-1")
	input := loreclient.PushAuthorization{
		ActorUserID:            "wrong-actor",
		RepositoryID:           "wrong-repository",
		OperationID:            "wrong-operation",
		RepositoryPartition:    "partition-1",
		TargetBranchID:         "branch-1",
		TargetBranchName:       "main",
		ExpectedTargetRevision: "target-1",
		ProposedRevision:       "merged-1",
		SourceRevision:         "source-1",
		ParentRevisions:        []string{"source-1", "target-1"},
	}
	if err := authorizer.AuthorizeLoreMergePush(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if recorder.count != 1 {
		t.Fatalf("authorization callback count = %d, want 1", recorder.count)
	}
	if recorder.input.ActorUserID != actor.ID || recorder.input.RepositoryID != repository.ID ||
		recorder.input.OperationID != "operation-1" || recorder.input.RepositoryPartition != "partition-1" ||
		recorder.input.TargetBranchID != "branch-1" || recorder.input.TargetBranchName != "main" ||
		recorder.input.ExpectedTargetRevision != "target-1" || recorder.input.ProposedRevision != "merged-1" ||
		recorder.input.SourceRevision != "source-1" || len(recorder.input.ParentRevisions) != 2 ||
		recorder.input.ParentRevisions[0] != "source-1" || recorder.input.ParentRevisions[1] != "target-1" {
		t.Fatalf("authorization tuple was not preserved and pinned: %+v", recorder.input)
	}
}

func TestFixedPushAuthorizerRequiresConfiguredDependency(t *testing.T) {
	api := &API{}
	actor := platform.User{ID: "actor-1"}
	repository := collab.Repository{ID: "repo-1"}
	if authorizer := api.fixedPushAuthorizer(actor, repository, "op-1"); authorizer != nil {
		t.Fatal("nil production authorizer dependency was accepted")
	}
}

func TestPushAuthorizationDoubleRejectsWrongTuple(t *testing.T) {
	want := loreclient.PushAuthorization{
		ActorUserID: "actor-1", RepositoryID: "repo-1", RepositoryPartition: "partition-1",
		OperationID: "op-1", TargetBranchID: "branch-1", TargetBranchName: "main",
		ExpectedTargetRevision: "target-1", ProposedRevision: "merged-1", SourceRevision: "source-1",
		ParentRevisions: []string{"source-1", "target-1"},
	}
	for name, input := range map[string]loreclient.PushAuthorization{
		"actor": func() loreclient.PushAuthorization { value := want; value.ActorUserID = "wrong"; return value }(),
		"base": func() loreclient.PushAuthorization {
			value := want
			value.ExpectedTargetRevision = "wrong"
			return value
		}(),
		"branch":   func() loreclient.PushAuthorization { value := want; value.TargetBranchID = "wrong"; return value }(),
		"proposed": func() loreclient.PushAuthorization { value := want; value.ProposedRevision = "wrong"; return value }(),
	} {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			called := false
			authorizer := loreclient.PushAuthorizerFunc(func(_ context.Context, value loreclient.PushAuthorization) error {
				called = true
				if !reflect.DeepEqual(value, want) {
					return errors.New("tuple mismatch")
				}
				return nil
			})
			if err := authorizer.AuthorizeLoreMergePush(context.Background(), input); err == nil {
				t.Fatal("wrong authorization tuple was accepted")
			}
			if !called {
				t.Fatal("authorization double was not called")
			}
		})
	}
}
