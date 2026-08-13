package nex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CommonDataStore is deliberately small so a local journal, PostgreSQL, or a
// remote service can back Ranking without changing its NEX handler.
type CommonDataStore interface {
	PutCommonData(pid uint64, data []byte, observedAt time.Time) error
}

type CommonDataRecord struct {
	PID        uint64    `json:"pid"`
	Data       []byte    `json:"data"`
	ObservedAt time.Time `json:"observedAt"`
}

// JSONLCommonDataStore is a durable append-only development store. Appending
// avoids partial in-place rewrites; a production PostgreSQL adapter can later
// replace it through CommonDataStore.
type JSONLCommonDataStore struct {
	mu     sync.Mutex
	path   string
	latest map[uint64]CommonDataRecord
}

func NewJSONLCommonDataStore(path string) (*JSONLCommonDataStore, error) {
	if path == "" {
		return nil, errors.New("ranking store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &JSONLCommonDataStore{path: path, latest: make(map[uint64]CommonDataRecord)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *JSONLCommonDataStore) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))
	for {
		var record CommonDataRecord
		if err := dec.Decode(&record); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		s.latest[record.PID] = record
	}
}

func (s *JSONLCommonDataStore) PutCommonData(pid uint64, data []byte, observedAt time.Time) error {
	if pid == 0 {
		return errors.New("ranking store PID is zero")
	}
	record := CommonDataRecord{
		PID:        pid,
		Data:       append([]byte(nil), data...),
		ObservedAt: observedAt.UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(record)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	s.latest[pid] = record
	return nil
}

func (s *JSONLCommonDataStore) Latest(pid uint64) (CommonDataRecord, bool) {
	s.mu.Lock()
	record, ok := s.latest[pid]
	record.Data = append([]byte(nil), record.Data...)
	s.mu.Unlock()
	return record, ok
}

func (s *JSONLCommonDataStore) Count() int {
	s.mu.Lock()
	n := len(s.latest)
	s.mu.Unlock()
	return n
}
