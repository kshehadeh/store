package onepassword

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/kshehadeh/store/internal/provider"
)

type call struct {
	args  []string
	stdin string
}

type recorder struct {
	calls []call
	out   map[string]string
}

func (r *recorder) Run(_ context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	c := call{args: args}
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		c.stdin = string(b)
	}
	r.calls = append(r.calls, c)
	return []byte(r.out[strings.Join(args[:2], " ")]), nil
}

func TestCreateSendsPayloadOnStdin(t *testing.T) {
	r := &recorder{out: map[string]string{"document create": `{"uuid":"abc"}`}}
	a, err := New(r, map[string]string{"vault_id": "V1", "account": "ACCT"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.CreateDocument(context.Background(), provider.Document{
		Title: "root-env", FileName: ".env", Tags: []string{"store/a", "store/b"}, Content: strings.NewReader("SECRET=1"),
	})
	if err != nil || id != "abc" {
		t.Fatalf("CreateDocument = %q, %v", id, err)
	}
	c := r.calls[0]
	want := []string{"document", "create", "-", "--vault", "V1", "--title", "root-env", "--file-name", ".env", "--tags", "store/a,store/b", "--format", "json", "--account", "ACCT"}
	if !slices.Equal(c.args, want) {
		t.Errorf("args = %q", c.args)
	}
	if c.stdin != "SECRET=1" {
		t.Errorf("stdin = %q", c.stdin)
	}
}

func TestCreateSetsDescriptionAsCustomTextField(t *testing.T) {
	r := &recorder{out: map[string]string{"document create": `{"uuid":"abc"}`}}
	a, err := New(r, map[string]string{"vault_id": "V1"})
	if err != nil {
		t.Fatal(err)
	}
	description := "Local API settings"
	id, err := a.CreateDocument(context.Background(), provider.Document{
		Title: "root-env", FileName: ".env", Description: &description, Content: strings.NewReader("SECRET=1"),
	})
	if err != nil || id != "abc" {
		t.Fatalf("CreateDocument = %q, %v", id, err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("got %d op calls; want create plus description edit", len(r.calls))
	}
	want := []string{"item", "edit", "abc", "Description[text]=Local API settings", "--vault", "V1"}
	if !slices.Equal(r.calls[1].args, want) {
		t.Errorf("description args = %q; want %q", r.calls[1].args, want)
	}
}

func TestUpdateDocumentSetsDescriptionWhenProvided(t *testing.T) {
	r := &recorder{}
	a, err := New(r, map[string]string{"vault_id": "V1"})
	if err != nil {
		t.Fatal(err)
	}
	description := "Updated description"
	err = a.UpdateDocument(context.Background(), "abc", provider.Document{
		Title: "root-env", FileName: ".env", Tags: []string{"store/managed"},
		Description: &description, Content: strings.NewReader("SECRET=2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("got %d op calls; want document edit plus description edit", len(r.calls))
	}
	want := []string{"item", "edit", "abc", "Description[text]=Updated description", "--vault", "V1"}
	if !slices.Equal(r.calls[1].args, want) {
		t.Errorf("description args = %q; want %q", r.calls[1].args, want)
	}
}

func TestListMapsCategories(t *testing.T) {
	r := &recorder{out: map[string]string{"item list": `[
		{"id":"1","title":"a","category":"DOCUMENT","tags":["store/x"],"updated_at":"2026-01-02T03:04:05.123Z"},
		{"id":"2","title":"b","category":"SECURE_NOTE"},
		{"id":"3","title":"c","category":"API_CREDENTIAL"}]`}}
	a, _ := New(r, map[string]string{"vault_id": "V1"})
	items, err := a.ListItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Kind != provider.KindDocument || items[0].Updated.IsZero() || items[1].Kind != provider.KindNote || items[2].Category != "API Credential" {
		t.Errorf("items = %+v", items)
	}
	if slices.Contains(r.calls[0].args, "--account") {
		t.Error("--account passed without an account")
	}
}

func TestRenameItem(t *testing.T) {
	r := &recorder{}
	a, err := New(r, map[string]string{"vault_id": "V1", "account": "ACCT"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RenameItem(context.Background(), "item-id", "new title"); err != nil {
		t.Fatal(err)
	}
	want := []string{"item", "edit", "item-id", "--vault", "V1", "--title", "new title", "--account", "ACCT"}
	if !slices.Equal(r.calls[0].args, want) {
		t.Fatalf("RenameItem args = %q; want %q", r.calls[0].args, want)
	}
}

func TestGetItemReadsDescriptionField(t *testing.T) {
	r := &recorder{out: map[string]string{"item get": `{
		"id":"abc","title":"settings","category":"DOCUMENT",
		"tags":["store/meta/managed/v1"],
		"fields":[{"id":"field1","type":"STRING","label":"Description","value":"Local settings"}]
	}`}}
	a, err := New(r, map[string]string{"vault_id": "V1"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := a.GetItem(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Description != "Local settings" {
		t.Fatalf("Description = %q; want Local settings", detail.Description)
	}
	if slices.Contains(r.calls[0].args, "--reveal") {
		t.Fatal("GetItem revealed concealed fields")
	}
}

func TestUpdateItemMetadata(t *testing.T) {
	r := &recorder{}
	a, err := New(r, map[string]string{"vault_id": "V1", "account": "ACCT"})
	if err != nil {
		t.Fatal(err)
	}
	description := "Updated settings"
	itemTags := []string{"store/meta/managed/v1", "store/environment/dev"}
	if err := a.UpdateItemMetadata(context.Background(), "abc", &description, itemTags); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"item", "edit", "abc", "Description[text]=Updated settings",
		"--vault", "V1", "--tags", "store/meta/managed/v1,store/environment/dev", "--account", "ACCT",
	}
	if !slices.Equal(r.calls[0].args, want) {
		t.Fatalf("UpdateItemMetadata args = %q; want %q", r.calls[0].args, want)
	}
}

func TestUpdateItemMetadataCanPreserveDescription(t *testing.T) {
	r := &recorder{}
	a, _ := New(r, map[string]string{"vault_id": "V1"})
	if err := a.UpdateItemMetadata(context.Background(), "abc", nil, []string{"store/meta/managed/v1"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"item", "edit", "abc", "--vault", "V1", "--tags", "store/meta/managed/v1"}
	if !slices.Equal(r.calls[0].args, want) {
		t.Fatalf("UpdateItemMetadata args = %q; want %q", r.calls[0].args, want)
	}
}

func TestCleanMessage(t *testing.T) {
	got := cleanMessage("[ERROR] 2026/01/02 03:04:05 account is not signed in\n")
	if got != "account is not signed in" {
		t.Errorf("cleanMessage = %q", got)
	}
}
