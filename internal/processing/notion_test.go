package processing

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type notionCall struct {
	name  string
	args  []string
	stdin string
}

type notionRunner struct {
	calls []notionCall
}

func (runner *notionRunner) Run(_ context.Context, name string, args []string, stdin io.Reader) ([]byte, error) {
	var input []byte
	if stdin != nil {
		input, _ = io.ReadAll(stdin)
	}
	runner.calls = append(runner.calls, notionCall{name: name, args: append([]string(nil), args...), stdin: string(input)})
	if len(runner.calls) == 1 {
		return []byte(`{"id":"upload-id","status":"uploaded"}`), nil
	}
	if len(runner.calls) == 2 {
		return []byte(`{"object":"page","id":"3d12d5f2-8d7d-8000-a111-111111111111","url":"https://app.notion.com/p/Meeting-3d12d5f28d7d8000a111111111111111"}`), nil
	}
	return []byte(`{"object":"block","id":"3d12d5f2-8d7d-803f-a8ec-e1c7b133b4f5"}`), nil
}

func TestUploadToNotionUsesCLIAndCreatesMeetingNote(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "meeting.m4a"), []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := meeting.Metadata{
		ID:        "2026-09-04T09-42-13",
		StartedAt: time.Date(2026, 9, 4, 9, 42, 13, 0, time.UTC),
		Merged:    &meeting.AudioFile{File: "meeting.m4a", Channels: 2},
	}
	runner := &notionRunner{}
	result, err := UploadToNotion(context.Background(), runner, directory, metadata, NotionOptions{
		ParentPageID: "3d12d5f2-8d7d-8067-ad48-c686bec6fb0a", DestinationID: "team", DestinationName: "Team meetings", KickoffSummary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileUploadID != "upload-id" || result.PageID != "3d12d5f2-8d7d-8000-a111-111111111111" || result.BlockID != "3d12d5f2-8d7d-803f-a8ec-e1c7b133b4f5" || result.Status != "processing" {
		t.Fatalf("unexpected Notion result: %#v", result)
	}
	if result.DestinationID != "team" || result.DestinationName != "Team meetings" {
		t.Fatalf("destination was not retained: %#v", result)
	}
	if result.URL != "https://app.notion.com/p/Meeting-3d12d5f28d7d8000a111111111111111" {
		t.Fatalf("unexpected Notion URL: %q", result.URL)
	}
	if len(runner.calls) != 3 || runner.calls[0].name != "ntn" || runner.calls[1].name != "ntn" || runner.calls[2].name != "ntn" {
		t.Fatalf("unexpected calls: %#v", runner.calls)
	}
	if runner.calls[0].stdin != "audio" || !containsSequence(runner.calls[0].args, []string{"files", "create", "--json"}) {
		t.Fatalf("unexpected upload call: %#v", runner.calls[0])
	}
	pageDataIndex := dataArgumentIndex(runner.calls[1].args)
	if pageDataIndex == -1 {
		t.Fatalf("child page call has no JSON body: %#v", runner.calls[1])
	}
	var pageBody map[string]any
	if err := json.Unmarshal([]byte(runner.calls[1].args[pageDataIndex]), &pageBody); err != nil {
		t.Fatal(err)
	}
	pageParent := pageBody["parent"].(map[string]any)
	if pageParent["page_id"] != "3d12d5f2-8d7d-8067-ad48-c686bec6fb0a" {
		t.Fatalf("unexpected child page parent: %#v", pageBody)
	}
	properties := pageBody["properties"].(map[string]any)
	if _, ok := properties["title"]; !ok {
		t.Fatalf("child page has no title: %#v", pageBody)
	}

	dataIndex := dataArgumentIndex(runner.calls[2].args)
	if dataIndex == -1 {
		t.Fatalf("meeting note call has no JSON body: %#v", runner.calls[2])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(runner.calls[2].args[dataIndex]), &body); err != nil {
		t.Fatal(err)
	}
	parent := body["parent"].(map[string]any)
	if parent["page_id"] != "3d12d5f2-8d7d-8000-a111-111111111111" {
		t.Fatalf("meeting note was not nested in child page: %#v", body)
	}
}

func dataArgumentIndex(arguments []string) int {
	for index, argument := range arguments {
		if argument == "-d" && index+1 < len(arguments) {
			return index + 1
		}
	}
	return -1
}

func TestUploadToNotionRequiresParentPage(t *testing.T) {
	metadata := meeting.Metadata{Merged: &meeting.AudioFile{File: "meeting.m4a"}}
	if _, err := UploadToNotion(context.Background(), &notionRunner{}, t.TempDir(), metadata, NotionOptions{}); err == nil {
		t.Fatal("expected missing parent page to fail")
	}
}
