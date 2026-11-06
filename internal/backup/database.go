package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/shaomian/PrismCat/internal/storage"
)

func inspectDatabase(ctx context.Context, filename string) (map[string]int64, []string, string, error) {
	db, err := storage.OpenBackupDatabase(filename, true)
	if err != nil {
		return nil, nil, "", err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return nil, nil, "", fmt.Errorf("database integrity check failed: %s (%v)", integrity, err)
	}
	rows, err := db.QueryContext(ctx, "SELECT type,name,COALESCE(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil {
		return nil, nil, "", err
	}
	hash := sha256.New()
	tables := []string{}
	for rows.Next() {
		var kind, name, statement string
		if err = rows.Scan(&kind, &name, &statement); err != nil {
			break
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", kind, name, statement)
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	err = errorsWithRows(err, rows)
	if err != nil {
		return nil, nil, "", err
	}
	counts := map[string]int64{}
	for _, table := range tables {
		var count int64
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&count); err != nil {
			return nil, nil, "", err
		}
		counts[table] = count
	}
	if _, ok := counts["request_logs"]; !ok {
		return nil, nil, "", fmt.Errorf("not a PrismCat database")
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, nil, "", err
	}
	hasInvalidFK := rows.Next()
	err = errorsWithRows(nil, rows)
	if err != nil || hasInvalidFK {
		return nil, nil, "", fmt.Errorf("invalid foreign key references: %v", err)
	}
	rows, err = db.QueryContext(ctx, `SELECT request_body_ref FROM request_logs WHERE COALESCE(request_body_ref,'')!='' UNION SELECT response_body_ref FROM request_logs WHERE COALESCE(response_body_ref,'')!=''`)
	if err != nil {
		return nil, nil, "", err
	}
	refs := []string{}
	canonical := map[string]bool{}
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			break
		}
		var name string
		name, err = storage.BackupBlobPath(ref)
		if err != nil {
			break
		}
		if !canonical[name] {
			refs = append(refs, ref)
			canonical[name] = true
		}
		if len(refs) > maxFiles-3 {
			err = fmt.Errorf("too many body files")
			break
		}
	}
	err = errorsWithRows(err, rows)
	return counts, refs, hex.EncodeToString(hash.Sum(nil)), err
}

func errorsWithRows(err error, rows *sql.Rows) error {
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
