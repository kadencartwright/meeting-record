package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kadencartwright/meeting-record/internal/meeting"
)

type Upload struct {
	ID          string               `json:"id"`
	Fingerprint string               `json:"fingerprint"`
	SourceName  string               `json:"sourceName"`
	RecordedAt  time.Time            `json:"recordedAt"`
	Notion      meeting.NotionExport `json:"notion"`
}

type Registry struct {
	Version int               `json:"version"`
	Uploads map[string]Upload `json:"uploads"`
}

func LoadRegistry(storage meeting.Storage) (Registry, error) {
	path := filepath.Join(storage.Root, "external-uploads.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{Version: 1, Uploads: map[string]Upload{}}, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("read external recording registry: %w", err)
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("parse external recording registry: %w", err)
	}
	if registry.Uploads == nil {
		registry.Uploads = map[string]Upload{}
	}
	return registry, nil
}

func (registry Registry) Save(storage meeting.Storage) error {
	if err := os.MkdirAll(storage.Root, 0o700); err != nil {
		return fmt.Errorf("create recorder storage: %w", err)
	}
	registry.Version = 1
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize external recording registry: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(storage.Root, ".external-uploads-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(storage.Root, "external-uploads.json"))
}

func (registry Registry) Lookup(file File) (Upload, bool) {
	upload, found := registry.Uploads[fingerprintKey(file.Fingerprint)]
	return upload, found && upload.Fingerprint == file.Fingerprint
}

func (registry Registry) LookupID(id string) (Upload, bool) {
	for _, upload := range registry.Uploads {
		if upload.ID == id {
			return upload, true
		}
	}
	return Upload{}, false
}

func (registry *Registry) Put(file File, notion meeting.NotionExport) {
	if registry.Uploads == nil {
		registry.Uploads = map[string]Upload{}
	}
	for key, upload := range registry.Uploads {
		if upload.ID == file.ID {
			delete(registry.Uploads, key)
		}
	}
	registry.Uploads[fingerprintKey(file.Fingerprint)] = Upload{
		ID: file.ID, Fingerprint: file.Fingerprint, SourceName: file.Name,
		RecordedAt: file.RecordedAt, Notion: notion,
	}
}

func Decorate(inventory *Inventory, registry Registry) {
	for recorderIndex := range inventory.Recorders {
		for fileIndex := range inventory.Recorders[recorderIndex].Files {
			file := &inventory.Recorders[recorderIndex].Files[fileIndex]
			if upload, found := registry.Lookup(*file); found {
				file.Notion = &upload.Notion
			}
		}
	}
}

func fingerprintKey(fingerprint string) string {
	sum := sha256.Sum256([]byte(fingerprint))
	return hex.EncodeToString(sum[:])
}
