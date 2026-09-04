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
	if result.FileUploadID != "upload-id" || result.BlockID != "3d12d5f2-8d7d-803f-a8ec-e1c7b133b4f5" || result.Status != "processing" {
		t.Fatalf("unexpected Notion result: %#v", result)
	}
	if result.DestinationID != "team" || result.DestinationName != "Team meetings" {
		t.Fatalf("destination was not retained: %#v", result)
	}
	if result.URL != "https://app.notion.com/p/3d12d5f28d7d8067ad48c686bec6fb0a#3d12d5f28d7d803fa8ece1c7b133b4f5" {
		t.Fatalf("unexpected Notion URL: %q", result.URL)
	}
	if len(runner.calls) != 2 || runner.calls[0].name != "ntn" || runner.calls[1].name != "ntn" {
		t.Fatalf("unexpected calls: %#v", runner.calls)
	}
	if runner.calls[0].stdin != "audio" || !containsSequence(runner.calls[0].args, []string{"files", "create", "--json"}) {
		t.Fatalf("unexpected upload call: %#v", runner.calls[0])
	}
	dataIndex := -1
	for index, argument := range runner.calls[1].args {
		if argument == "-d" && index+1 < len(runner.calls[1].args) {
			dataIndex = index + 1
			break
		}
	}
	if dataIndex == -1 {
		t.Fatalf("meeting note call has no JSON body: %#v", runner.calls[1])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(runner.calls[1].args[dataIndex]), &body); err != nil {
		t.Fatal(err)
	}
	parent := body["parent"].(map[string]any)
	if parent["page_id"] != "3d12d5f2-8d7d-8067-ad48-c686bec6fb0a" {
		t.Fatalf("unexpected meeting note parent: %#v", body)
	}
}

func TestUploadToNotionRequiresParentPage(t *testing.T) {
	metadata := meeting.Metadata{Merged: &meeting.AudioFile{File: "meeting.m4a"}}
	if _, err := UploadToNotion(context.Background(), &notionRunner{}, t.TempDir(), metadata, NotionOptions{}); err == nil {
		t.Fatal("expected missing parent page to fail")
	}
}
