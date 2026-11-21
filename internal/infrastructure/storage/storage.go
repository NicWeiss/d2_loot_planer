// Package storage keeps files under DATA_DIR: the market snapshot and browser sessions.
package storage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"loot-planner/internal/application"
	"loot-planner/internal/domain/market"
)

// writeAtomic writes through a temp file, so a crash mid-write leaves the previous file intact.
func writeAtomic(path string, mode os.FileMode, write func(*bufio.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if err := write(w); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Snapshots: DATA_DIR/market/{wts,wtb,currency}.json. Listings are streamed one by one and only the fields
// the app reads are kept, so a 100 MB file never sits in memory whole.
type Snapshots struct{ Dir string }

func NewSnapshots(dataDir string) *Snapshots { return &Snapshots{filepath.Join(dataDir, "market")} }

func readAds(path string) ([]market.Ad, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil, fmt.Errorf("%s: want a JSON array", path)
	}
	var ads []market.Ad
	for dec.More() {
		var a market.Ad
		if err := dec.Decode(&a); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ads = append(ads, a)
	}
	return ads, nil
}

func (s *Snapshots) Load() (*application.RawSnapshot, error) {
	snap := &application.RawSnapshot{}
	var err error
	if snap.WTS, err = readAds(filepath.Join(s.Dir, "wts.json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if snap.WTB, err = readAds(filepath.Join(s.Dir, "wtb.json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "currency.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return snap, json.Unmarshal(b, &snap.Currency)
}

func writeAds(w *bufio.Writer, ads []market.Ad) error {
	w.WriteByte('[')
	for i := range ads {
		if i > 0 {
			w.WriteByte(',')
		}
		b, err := json.Marshal(&ads[i])
		if err != nil {
			return err
		}
		w.Write(b)
	}
	w.WriteByte(']')
	return nil
}

func (s *Snapshots) Save(snap *application.RawSnapshot) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.Dir, "wts.json"), 0o644, func(w *bufio.Writer) error { return writeAds(w, snap.WTS) }); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.Dir, "wtb.json"), 0o644, func(w *bufio.Writer) error { return writeAds(w, snap.WTB) }); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, "currency.json"), 0o644, func(w *bufio.Writer) error {
		return json.NewEncoder(w).Encode(snap.Currency)
	})
}

// Sessions: DATA_DIR/sessions/<hash of session id>.json, readable by the service user only.
// Files are named by a hash so a directory listing does not reveal the session cookies.
type Sessions struct{ Dir string }

func NewSessions(dataDir string) *Sessions { return &Sessions{filepath.Join(dataDir, "sessions")} }

func (s *Sessions) path(id string) string {
	h := sha256.Sum256([]byte(id))
	return filepath.Join(s.Dir, hex.EncodeToString(h[:])[:40]+".json")
}

func (s *Sessions) Get(id string) *application.Session {
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil
	}
	var sess application.Session
	if json.Unmarshal(b, &sess) != nil {
		return nil
	}
	sess.ID = id
	return &sess
}

func (s *Sessions) Save(sess *application.Session) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(s.Dir, 0o700)
	sess.Touched = float64(time.Now().UnixNano()) / 1e9
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return writeAtomic(s.path(sess.ID), 0o600, func(w *bufio.Writer) error { _, err := w.Write(b); return err })
}

func (s *Sessions) Delete(id string) { _ = os.Remove(s.path(id)) }

func (s *Sessions) Purge(olderThan float64) int {
	entries, _ := os.ReadDir(s.Dir)
	n := 0
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && float64(info.ModTime().Unix()) < olderThan {
			if os.Remove(filepath.Join(s.Dir, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}
