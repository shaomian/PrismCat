package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestRollbackAtEveryInstallationStepIsRestartable(t *testing.T) {
	for step := 0; step < 6; step++ {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			target := t.TempDir()
			id := uuid.NewString()
			stage := filepath.Join(target, ".prismcat-install-"+id)
			saved := filepath.Join(target, ".prismcat-rollback-"+id)
			_ = os.Mkdir(stage, 0700)
			_ = os.Mkdir(saved, 0700)
			journal := restoreJournal{ID: id}
			for _, name := range []string{"prismcat.db", "prismcat.db-wal", "prismcat.db-shm", "prismcat.db-journal", "blobs", "config.yaml"} {
				entry := restoreEntry{Name: name, HadOriginal: true, Install: name == "prismcat.db" || name == "blobs" || name == "config.yaml"}
				journal.Entries = append(journal.Entries, entry)
				if name == "blobs" {
					_ = os.Mkdir(filepath.Join(target, name), 0700)
					_ = os.WriteFile(filepath.Join(target, name, "old"), []byte("old"), 0600)
					_ = os.Mkdir(filepath.Join(stage, name), 0700)
					_ = os.WriteFile(filepath.Join(stage, name, "new"), []byte("new"), 0600)
				} else {
					_ = os.WriteFile(filepath.Join(target, name), []byte("old-"+name), 0600)
					if entry.Install {
						_ = os.WriteFile(filepath.Join(stage, name), []byte("new-"+name), 0600)
					}
				}
			}
			if err := writeJSON(filepath.Join(target, journalName), journal); err != nil {
				t.Fatal(err)
			}
			if err := install(context.Background(), target, stage, saved, journal, func(i int) error {
				if i == step {
					return errors.New("injected crash")
				}
				return nil
			}); err == nil {
				t.Fatal("fault not injected")
			}
			if !PendingRestore(filepath.Join(target, "config.yaml")) {
				t.Fatal("startup not blocked")
			}
			if err := Rollback(target, saved); err == nil {
				t.Fatal("explicit rollback replaced an unfinished restore journal")
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := rollbackRestore(target, saved, journal); err != nil {
					t.Fatal(err)
				}
				for _, entry := range journal.Entries {
					name := entry.Name
					if name == "blobs" {
						name = filepath.Join(name, "old")
					}
					data, err := os.ReadFile(filepath.Join(target, name))
					if err != nil || string(data) != "old" && string(data) != "old-"+entry.Name {
						t.Fatalf("step %d attempt %d: %s %q %v", step, attempt, entry.Name, data, err)
					}
				}
			}
		})
	}
}
