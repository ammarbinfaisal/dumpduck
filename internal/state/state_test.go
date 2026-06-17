package state

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmptyState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.json")
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load missing state: %v", err)
	}

	if !reflect.DeepEqual(got, State{}) {
		t.Fatalf("expected empty state, got %#v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")

	lastUpload := time.Unix(1_700_000_000, 0).UTC()
	windowStart := time.Unix(1_700_000_900, 0).UTC()
	want := State{
		LastSuccessfulUploadTime: &lastUpload,
		UploadedFiles: []UploadedFileRecord{
			{
				Path:       "/tmp/capture-1.pcap",
				RemotePath: "gdrive:dumpduck/capture-1.pcap",
				UploadedAt: lastUpload,
			},
		},
		CurrentWindowStartTime: &windowStart,
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("save state: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("state mismatch after round trip:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestRecordUploadedFileReplacesExistingPath(t *testing.T) {
	t.Parallel()

	uploadedAt := time.Unix(1_700_000_000, 0).UTC()
	updatedAt := uploadedAt.Add(5 * time.Minute)

	st := State{
		UploadedFiles: []UploadedFileRecord{
			{
				Path:       "/tmp/capture-1.pcap",
				RemotePath: "remote:captures/capture-1.pcap",
				UploadedAt: uploadedAt,
			},
		},
	}

	st.RecordUploadedFile(UploadedFileRecord{
		Path:       "/tmp/../tmp/capture-1.pcap",
		RemotePath: "remote:captures-renamed/capture-1.pcap",
		UploadedAt: updatedAt,
	})

	if len(st.UploadedFiles) != 1 {
		t.Fatalf("expected one uploaded file, got %#v", st.UploadedFiles)
	}
	if st.UploadedFiles[0].RemotePath != "remote:captures-renamed/capture-1.pcap" {
		t.Fatalf("expected updated remote path, got %#v", st.UploadedFiles[0])
	}
}
