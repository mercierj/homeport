package authz

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
)

type FileAuditLog struct {
	mu   sync.Mutex
	path string
}

func NewFileAuditLog(path string) *FileAuditLog {
	return &FileAuditLog{path: path}
}

func (l *FileAuditLog) Record(decision Decision) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	if err := json.NewEncoder(file).Encode(decision); err != nil {
		return err
	}
	return file.Sync()
}

func (l *FileAuditLog) Decisions() ([]Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var decisions []Decision
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var decision Decision
		if err := json.Unmarshal([]byte(line), &decision); err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return decisions, nil
}
