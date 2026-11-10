package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func writeJSON(w io.Writer, path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if path == "" || path == "-" {
		_, err = w.Write(body)
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func readJSON(path string, dest any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
