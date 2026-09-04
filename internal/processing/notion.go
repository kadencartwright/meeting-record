package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type NotionOptions struct {
	ParentPageID    string
	DestinationID   string
	DestinationName string
	Title           string
	Language        string
	KickoffSummary  bool
}

type fileUploadResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type meetingNoteResponse struct {
	ID string `json:"id"`
}

func UploadToNotion(ctx context.Context, runner Runner, directory string, metadata meeting.Metadata, options NotionOptions) (meeting.NotionExport, error) {
	if metadata.Notion != nil && metadata.Notion.BlockID != "" {
		return meeting.NotionExport{}, fmt.Errorf("session is already uploaded to Notion as block %s", metadata.Notion.BlockID)
	}
	if strings.TrimSpace(options.ParentPageID) == "" {
		return meeting.NotionExport{}, fmt.Errorf("Notion parent page is not configured; set MEETING_RECORD_NOTION_PARENT_PAGE_ID or pass --parent-page")
	}
	if metadata.Merged == nil || metadata.Merged.File == "" {
		return meeting.NotionExport{}, fmt.Errorf("session has no merged meeting audio")
	}
	audioPath, err := trackPath(directory, metadata.Merged.File)
	if err != nil {
		return meeting.NotionExport{}, err
	}
	audioFile, err := os.Open(audioPath)
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("open merged meeting audio: %w", err)
	}
	uploadOutput, err := runner.Run(ctx, "ntn", []string{
		"files", "create", "--json",
		"--filename", metadata.ID + filepath.Ext(metadata.Merged.File),
		"--content-type", "audio/mp4",
	}, audioFile)
	closeErr := audioFile.Close()
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("upload meeting audio to Notion: %w", err)
	}
	if closeErr != nil {
		return meeting.NotionExport{}, closeErr
	}
	var upload fileUploadResponse
	if err := json.Unmarshal(uploadOutput, &upload); err != nil {
		return meeting.NotionExport{}, fmt.Errorf("parse Notion file upload response: %w", err)
	}
	if upload.ID == "" || upload.Status != "uploaded" {
		return meeting.NotionExport{}, fmt.Errorf("Notion file upload did not complete (status %q)", upload.Status)
	}

	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Meeting " + metadata.StartedAt.Format("Jan 2, 2006 3:04 PM")
	}
	language := strings.TrimSpace(options.Language)
	if language == "" {
		language = "auto"
	}
	body, err := json.Marshal(map[string]any{
		"source": map[string]string{
			"type":           "file_upload",
			"file_upload_id": upload.ID,
		},
		"parent": map[string]string{
			"type":    "page_id",
			"page_id": strings.TrimSpace(options.ParentPageID),
		},
		"title":    title,
		"language": language,
		"options": map[string]bool{
			"kickoff_summary": options.KickoffSummary,
		},
	})
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("serialize Notion meeting note request: %w", err)
	}
	noteOutput, err := runner.Run(ctx, "ntn", []string{
		"api", "/v1/blocks/meeting_notes", "-X", "POST", "-d", string(body),
	}, nil)
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("create Notion meeting note: %w", err)
	}
	var note meetingNoteResponse
	if err := json.Unmarshal(noteOutput, &note); err != nil {
		return meeting.NotionExport{}, fmt.Errorf("parse Notion meeting note response: %w", err)
	}
	if note.ID == "" {
		return meeting.NotionExport{}, fmt.Errorf("Notion meeting note response contained no block id")
	}
	notionURL, _ := meeting.NotionBlockURL(options.ParentPageID, note.ID)
	return meeting.NotionExport{
		FileUploadID:    upload.ID,
		BlockID:         note.ID,
		UploadedAt:      time.Now(),
		Status:          "processing",
		DestinationID:   options.DestinationID,
		DestinationName: options.DestinationName,
		URL:             notionURL,
	}, nil
}
