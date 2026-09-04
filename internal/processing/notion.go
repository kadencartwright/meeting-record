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

type notionPageResponse struct {
	ID  string `json:"id"`
	URL string `json:"url"`
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
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Meeting " + metadata.StartedAt.Format("Jan 2, 2006 3:04 PM")
	}
	return UploadAudioToNotion(ctx, runner, audioPath, metadata.ID+filepath.Ext(metadata.Merged.File), title, options)
}

func UploadAudioToNotion(ctx context.Context, runner Runner, audioPath, uploadFilename, defaultTitle string, options NotionOptions) (meeting.NotionExport, error) {
	if strings.TrimSpace(options.ParentPageID) == "" {
		return meeting.NotionExport{}, fmt.Errorf("Notion parent page is not configured")
	}
	audioFile, err := os.Open(audioPath)
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("open merged meeting audio: %w", err)
	}
	uploadOutput, err := runner.Run(ctx, "ntn", []string{
		"files", "create", "--json",
		"--filename", uploadFilename,
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
		title = defaultTitle
	}
	language := strings.TrimSpace(options.Language)
	if language == "" {
		language = "auto"
	}
	pageBody, err := json.Marshal(map[string]any{
		"parent": map[string]string{
			"type":    "page_id",
			"page_id": strings.TrimSpace(options.ParentPageID),
		},
		"properties": map[string]any{
			"title": map[string]any{
				"type": "title",
				"title": []any{map[string]any{
					"type": "text",
					"text": map[string]any{"content": title},
				}},
			},
		},
	})
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("serialize Notion child page request: %w", err)
	}
	pageOutput, err := runner.Run(ctx, "ntn", []string{
		"api", "/v1/pages", "-X", "POST", "-d", string(pageBody),
	}, nil)
	if err != nil {
		return meeting.NotionExport{}, fmt.Errorf("create Notion meeting page: %w", err)
	}
	var page notionPageResponse
	if err := json.Unmarshal(pageOutput, &page); err != nil {
		return meeting.NotionExport{}, fmt.Errorf("parse Notion child page response: %w", err)
	}
	if page.ID == "" {
		return meeting.NotionExport{}, fmt.Errorf("Notion child page response contained no page id")
	}

	body, err := json.Marshal(map[string]any{
		"source": map[string]string{
			"type":           "file_upload",
			"file_upload_id": upload.ID,
		},
		"parent": map[string]string{
			"type":    "page_id",
			"page_id": page.ID,
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
		archiveNotionPage(ctx, runner, page.ID)
		return meeting.NotionExport{}, fmt.Errorf("create Notion meeting note: %w", err)
	}
	var note meetingNoteResponse
	if err := json.Unmarshal(noteOutput, &note); err != nil {
		archiveNotionPage(ctx, runner, page.ID)
		return meeting.NotionExport{}, fmt.Errorf("parse Notion meeting note response: %w", err)
	}
	if note.ID == "" {
		archiveNotionPage(ctx, runner, page.ID)
		return meeting.NotionExport{}, fmt.Errorf("Notion meeting note response contained no block id")
	}
	notionURL := strings.TrimSpace(page.URL)
	if notionURL == "" {
		notionURL, _ = meeting.NotionPageURL(page.ID)
	}
	return meeting.NotionExport{
		FileUploadID:    upload.ID,
		PageID:          page.ID,
		BlockID:         note.ID,
		UploadedAt:      time.Now(),
		Status:          "processing",
		DestinationID:   options.DestinationID,
		DestinationName: options.DestinationName,
		URL:             notionURL,
	}, nil
}

func archiveNotionPage(ctx context.Context, runner Runner, pageID string) {
	body, _ := json.Marshal(map[string]bool{"in_trash": true})
	_, _ = runner.Run(ctx, "ntn", []string{
		"api", "/v1/pages/" + pageID, "-X", "PATCH", "-d", string(body),
	}, nil)
}
