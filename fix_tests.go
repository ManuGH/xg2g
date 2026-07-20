package main

import (
	"bytes"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func main() {
	err := filepath.WalkDir("backend/internal/config", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".go" && stringsHasSuffix(path, "_test.go") {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			newContent := bytes.ReplaceAll(content, []byte(`t.Setenv("XG2G_E2_HOST"`), []byte("t.Setenv(\"XG2G_RECORDINGS_TARGET_SIGNING_KEY\", \"abcdefghijklmnopqrstuvwxyz0123456789ABCDE1\")\n\t\tt.Setenv(\"XG2G_E2_HOST\""))
			if !bytes.Equal(content, newContent) {
				os.WriteFile(path, newContent, 0644)
				log.Printf("Updated %s", path)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
