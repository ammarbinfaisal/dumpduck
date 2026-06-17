package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type State struct {
	LastSuccessfulUploadTime *time.Time           `json:"last_successful_upload_time,omitempty"`
	UploadedFiles            []UploadedFileRecord `json:"uploaded_files,omitempty"`
	CurrentWindowStartTime   *time.Time           `json:"current_window_start_time,omitempty"`
}

type UploadedFileRecord struct {
	Path       string    `json:"path"`
	RemotePath string    `json:"remote_path,omitempty"`
	UploadedAt time.Time `json:"uploaded_at"`
}

func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read state %q: %w", path, err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	return st, nil
}

func Save(path string, st State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory for %q: %w", path, err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state %q: %w", path, err)
	}
	return nil
}
