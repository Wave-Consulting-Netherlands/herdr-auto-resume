package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	captureMaxFileBytes = 256 * 1024
	captureMaxDirBytes  = 4 * 1024 * 1024
)

func (m *Manager) capturePaneContent(jobID, content string, now time.Time) {
	dir := filepath.Join(filepath.Dir(m.store.Path()), "captures")
	if err := os.MkdirAll(dir, 0700); err != nil {
		m.logf("job=%s pane capture mkdir failed: %v", jobID, err)
		return
	}
	// Deliberately no os.Chmod on dir: MkdirAll already creates it 0700, and
	// chmod'ing a pre-existing directory the tool does not own fails outright.
	// That is BACKLOG 19, removed from the store on 2026-08-05; here it would
	// silently disable capture on exactly the shared /tmp state dirs where a
	// rare live event is most likely to need capturing.
	data := []byte(content)
	if len(data) > captureMaxFileBytes {
		data = data[:captureMaxFileBytes]
	}
	base := fmt.Sprintf("%s-%s", safeCaptureJobID(jobID), now.UTC().Format("20060102T150405.000000000Z"))
	path, err := writeCaptureFile(dir, base, data)
	if err != nil {
		m.logf("job=%s pane capture write failed: %v", jobID, err)
		return
	}
	if err := os.Chmod(path, 0600); err != nil {
		m.logf("job=%s pane capture chmod file failed: %v", jobID, err)
		return
	}
	if err := boundCaptures(dir); err != nil {
		m.logf("job=%s pane capture bound failed: %v", jobID, err)
	}
}

func writeCaptureFile(dir, base string, data []byte) (string, error) {
	for suffix := 0; suffix < 100; suffix++ {
		name := base + ".txt"
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d.txt", base, suffix)
		}
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("too many captures at timestamp %s", base)
}

func safeCaptureJobID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "job"
	}
	return b.String()
}

type captureEntry struct {
	path    string
	size    int64
	modTime time.Time
}

func boundCaptures(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := make([]captureEntry, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, captureEntry{path: filepath.Join(dir, entry.Name()), size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path < files[j].path
		}
		return files[i].modTime.Before(files[j].modTime)
	})
	for _, file := range files {
		if total <= captureMaxDirBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return err
		}
		total -= file.size
	}
	return nil
}
