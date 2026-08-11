package releases

import "testing"

const validTestRevision = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestValidateCreate(t *testing.T) {
	input, err := validateCreate(CreateInput{
		TagName: " v1.2.0 ", Title: " Release 1.2 ", Notes: " Notes ",
		SourceBranch: "main", Revision: validTestRevision, State: "published",
	})
	if err != nil {
		t.Fatalf("valid release failed: %v", err)
	}
	if input.TagName != "v1.2.0" || input.Title != "Release 1.2" || input.State != "published" {
		t.Fatalf("normalized release = %+v", input)
	}
	for _, invalidInput := range []CreateInput{
		{TagName: "", Title: "Release", SourceBranch: "main", Revision: validTestRevision},
		{TagName: "v1..2", Title: "Release", SourceBranch: "main", Revision: validTestRevision},
		{TagName: "v1", Title: "", SourceBranch: "main", Revision: validTestRevision},
		{TagName: "v1", Title: "Release", SourceBranch: "bad branch", Revision: validTestRevision},
		{TagName: "v1", Title: "Release", SourceBranch: "main", Revision: "bad"},
		{TagName: "v1", Title: "Release", SourceBranch: "main", Revision: validTestRevision, State: "open"},
	} {
		if _, err := validateCreate(invalidInput); err == nil {
			t.Fatalf("invalid release accepted: %+v", invalidInput)
		}
	}
}

func TestValidateUpdateAndAsset(t *testing.T) {
	title := " Updated "
	update, err := validateUpdate(UpdateInput{Title: &title, ExpectedVersion: 2})
	if err != nil || update.Title == nil || *update.Title != "Updated" {
		t.Fatalf("valid update = %+v, err %v", update, err)
	}
	if _, err := validateUpdate(UpdateInput{ExpectedVersion: 1}); err == nil {
		t.Fatal("empty update was accepted")
	}
	asset, err := validateAsset(AssetInput{
		Name: " Linux archive ", ExternalURL: "https://downloads.example/release.tar.gz", ExpectedVersion: 2,
	})
	if err != nil || asset.Name != "Linux archive" {
		t.Fatalf("valid asset = %+v, err %v", asset, err)
	}
	for _, value := range []string{"file:///tmp/release", "https://user:pass@example.test/a", "https://a b"} {
		if _, err := validateAsset(AssetInput{Name: "asset", ExternalURL: value, ExpectedVersion: 1}); err == nil {
			t.Fatalf("invalid asset URL %q was accepted", value)
		}
	}
}
