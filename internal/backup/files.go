package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeJSON(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".prismcat-json-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = replaceFile(file.Name(), filename); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(filename))
}

func requireSpace(dir string, bytes int64) error {
	available, err := availableSpace(dir)
	if err != nil {
		return err
	}
	if bytes < 0 || uint64(bytes)+(16<<20) > available {
		return fmt.Errorf("insufficient disk space (need %d bytes plus reserve)", bytes)
	}
	return nil
}
