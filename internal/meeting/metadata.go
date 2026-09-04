package meeting

import (
	"encoding/json"
	"time"

	"github.com/kadencartwright/meeting-record/internal/audio"
)

const MetadataVersion = 4

type Track struct {
	File        string `json:"file"`
	NodeName    string `json:"nodeName"`
	Description string `json:"description"`
	Channels    int    `json:"channels"`
}

type AudioFile struct {
	File     string `json:"file"`
	Channels int    `json:"channels"`
}

type NotionExport struct {
	FileUploadID    string    `json:"fileUploadId"`
	BlockID         string    `json:"blockId"`
	UploadedAt      time.Time `json:"uploadedAt"`
	Status          string    `json:"status"`
	DestinationID   string    `json:"destinationId,omitempty"`
	DestinationName string    `json:"destinationName,omitempty"`
	URL             string    `json:"url,omitempty"`
}

type Metadata struct {
	Version         int           `json:"version"`
	ID              string        `json:"id"`
	StartedAt       time.Time     `json:"startedAt"`
	EndedAt         *time.Time    `json:"endedAt,omitempty"`
	DurationSeconds int64         `json:"durationSeconds"`
	SampleRate      int           `json:"sampleRate"`
	Status          string        `json:"status"`
	Failure         string        `json:"failure,omitempty"`
	Local           Track         `json:"local"`
	Remote          Track         `json:"remote"`
	Merged          *AudioFile    `json:"merged,omitempty"`
	MergeFailure    string        `json:"mergeFailure,omitempty"`
	Notion          *NotionExport `json:"notion,omitempty"`
}

func NewMetadata(id string, startedAt time.Time, devices audio.Devices) Metadata {
	return Metadata{
		Version:    MetadataVersion,
		ID:         id,
		StartedAt:  startedAt,
		SampleRate: 48000,
		Status:     "recording",
		Local: Track{
			File: "local.flac", NodeName: devices.Microphone.NodeName,
			Description: devices.Microphone.Description, Channels: 1,
		},
		Remote: Track{
			File: "remote.flac", NodeName: devices.Output.NodeName,
			Description: devices.Output.Description, Channels: 2,
		},
	}
}

func (m *Metadata) Finish(endedAt time.Time, failure string) {
	m.EndedAt = &endedAt
	m.DurationSeconds = max(0, int64(endedAt.Sub(m.StartedAt).Seconds()))
	if failure == "" {
		m.Status = "complete"
	} else {
		m.Status = "failed"
		m.Failure = failure
	}
}

func Marshal(metadata Metadata) ([]byte, error) {
	return json.MarshalIndent(metadata, "", "  ")
}
