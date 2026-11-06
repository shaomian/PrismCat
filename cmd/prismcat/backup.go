package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/backup"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
	"github.com/shaomian/PrismCat/internal/storage"
)

func runBackup(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: prismcat backup create|inspect|restore|rollback [options]")
	}
	flags := flag.NewFlagSet("backup "+args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	configPath := flags.String("config", "data/config.yaml", "source config (create only)")
	archive := flags.String("file", "", "backup ZIP file")
	target := flags.String("target", "", "destination data directory (restore/rollback)")
	dryRun := flags.Bool("dry-run", false, "validate and preview without installing data")
	replace := flags.Bool("replace", false, "replace existing managed data, preserving a rollback copy")
	keepServer := flags.String("keep-server-config", "", "retain server settings from this config file")
	from := flags.String("from", "", "completed restore rollback directory (rollback only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	ctx := context.Background()
	switch args[0] {
	case "create":
		if *archive == "" {
			return errors.New("--file is required")
		}
		configLock, err := instancelock.Acquire(*configPath)
		if err != nil {
			return err
		}
		defer configLock.Close()
		if backup.PendingRestore(*configPath) {
			return errors.New("unfinished restore: run backup rollback first")
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		dataLock, err := instancelock.Acquire(cfg.Storage.Database, cfg.Storage.BlobDir)
		if err != nil {
			return err
		}
		defer dataLock.Close()
		blobs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
		if err != nil {
			return err
		}
		manifest, err := backup.Create(ctx, backup.Source{Config: cfg, Blobs: blobs, Snapshot: func(ctx context.Context, name string) error {
			return storage.SnapshotFile(ctx, cfg.Storage.Database, name)
		}}, *archive, uuid.NewString(), "offline", nil)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(manifest)
	case "inspect":
		if *archive == "" {
			return errors.New("--file is required")
		}
		validated, err := backup.Validate(ctx, *archive, "")
		if err != nil {
			return err
		}
		defer validated.Close()
		return json.NewEncoder(output).Encode(validated.Manifest)
	case "restore":
		if *archive == "" || *target == "" {
			return errors.New("--file and --target are required")
		}
		result, err := backup.Restore(ctx, *archive, backup.RestoreOptions{Target: *target, Replace: *replace, DryRun: *dryRun, KeepServerConfig: *keepServer})
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	case "rollback":
		if *target == "" {
			return errors.New("--target is required")
		}
		if err := backup.Rollback(*target, *from); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "Rollback complete. Start with -config", filepath.Join(*target, "config.yaml"))
		return err
	default:
		return fmt.Errorf("unknown backup command %q", args[0])
	}
}
