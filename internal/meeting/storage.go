package meeting

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Storage struct {
	Root string
}

func DefaultStorage() (Storage, error) {
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Storage{}, fmt.Errorf("resolve home directory: %w", err)
		}
		root = filepath.Join(home, ".local", "share")
	}
	return Storage{Root: filepath.Join(root, "meeting-record")}, nil
}

func SessionID(t time.Time) string {
	return t.Format("2006-01-02T15-04-05")
}

func (s Storage) Create(startedAt time.Time) (string, string, error) {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return "", "", fmt.Errorf("create recordings directory: %w", err)
	}
	base := SessionID(startedAt)
	for suffix := 1; suffix < 1000; suffix++ {
		id := base
		if suffix > 1 {
			id = fmt.Sprintf("%s-%d", base, suffix)
		}
		directory := filepath.Join(s.Root, id)
		err := os.Mkdir(directory, 0o700)
		if err == nil {
			return id, directory, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf("create session directory: %w", err)
		}
	}
	return "", "", fmt.Errorf("could not allocate a unique session directory")
}

func (s Storage) Write(directory string, metadata Metadata) error {
	data, err := Marshal(metadata)
	if err != nil {
		return fmt.Errorf("serialize meeting metadata: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(directory, "meeting.json"), data, 0o600)
}

func (s Storage) Load(id string) (Metadata, string, error) {
	directory, err := s.resolve(id)
	if err != nil {
		return Metadata{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(directory, "meeting.json"))
	if err != nil {
		return Metadata{}, "", fmt.Errorf("read session %q metadata: %w", id, err)
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, "", fmt.Errorf("parse session %q metadata: %w", id, err)
	}
	if metadata.ID == "" {
		metadata.ID = id
	}
	return metadata, directory, nil
}

type SessionSummary struct {
	ID              string       `json:"id"`
	StartedAt       time.Time    `json:"startedAt"`
	EndedAt         *time.Time   `json:"endedAt,omitempty"`
	DurationSeconds int64        `json:"durationSeconds"`
	Status          string       `json:"status"`
	Failure         string       `json:"failure,omitempty"`
	Directory       string       `json:"directory"`
	LocalFile       string       `json:"localFile"`
	RemoteFile      string       `json:"remoteFile"`
	Microphone      audioSummary `json:"microphone"`
	Output          audioSummary `json:"output"`
}

type audioSummary struct {
	Description string `json:"description"`
	NodeName    string `json:"nodeName"`
}

type ListResult struct {
	Sessions []SessionSummary `json:"sessions"`
	Warnings []string         `json:"warnings,omitempty"`
}

func (s Storage) List() (ListResult, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return ListResult{Sessions: []SessionSummary{}}, nil
	}
	if err != nil {
		return ListResult{}, fmt.Errorf("read recordings directory: %w", err)
	}
	result := ListResult{Sessions: []SessionSummary{}}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, directory, err := s.Load(entry.Name())
		if err != nil {
			result.Warnings = append(result.Warnings, err.Error())
			continue
		}
		result.Sessions = append(result.Sessions, SessionSummary{
			ID: metadata.ID, StartedAt: metadata.StartedAt, EndedAt: metadata.EndedAt,
			DurationSeconds: metadata.DurationSeconds, Status: metadata.Status,
			Failure: metadata.Failure, Directory: directory,
			LocalFile:  filepath.Join(directory, metadata.Local.File),
			RemoteFile: filepath.Join(directory, metadata.Remote.File),
			Microphone: audioSummary{Description: metadata.Local.Description, NodeName: metadata.Local.NodeName},
			Output:     audioSummary{Description: metadata.Remote.Description, NodeName: metadata.Remote.NodeName},
		})
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.After(result.Sessions[j].StartedAt)
	})
	return result, nil
}

func (s Storage) Delete(id string) error {
	directory, err := s.resolve(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return nil
}

func (s Storage) resolve(id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	directory := filepath.Join(s.Root, id)
	info, err := os.Lstat(directory)
	if err != nil {
		return "", fmt.Errorf("find session %q: %w", id, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("session %q is not a regular directory", id)
	}
	return directory, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
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
	return os.Rename(temporaryName, path)
}
