package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketPlaylist = []byte("playlist")
	bucketVideos   = []byte("videos")
)

type VideoEntry struct {
	Title      string
	Duration   int
	Views      int
	Embeddable bool
	CachedAt   time.Time
}

type PlaylistEntry struct {
	Tracks   []PlaylistTrack
	CachedAt time.Time
}

type PlaylistTrack struct {
	VideoID     string
	Title       string
	DurationSec int
	Views       int
	Embeddable  bool
}

type Cache struct {
	db  *bolt.DB
	ttl time.Duration
}

func openCache(path string, ttl time.Duration) (*Cache, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketPlaylist); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketVideos)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db, ttl: ttl}, nil
}

func (c *Cache) close() { c.db.Close() }

func gobEncode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gobDecode(data []byte, v any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}

func (c *Cache) getVideo(id string) (VideoEntry, bool) {
	var e VideoEntry
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketVideos).Get([]byte(id))
		if b == nil {
			return fmt.Errorf("miss")
		}
		return gobDecode(b, &e)
	})
	if e.Title == "" {
		return VideoEntry{}, false
	}
	if c.ttl > 0 && time.Since(e.CachedAt) > c.ttl {
		return VideoEntry{}, false
	}
	return e, true
}

func (c *Cache) setVideo(id string, e VideoEntry) {
	e.CachedAt = time.Now()
	data, err := gobEncode(e)
	if err != nil {
		return
	}
	_ = c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketVideos).Put([]byte(id), data)
	})
}

func (c *Cache) getPlaylist(id string) (PlaylistEntry, bool) {
	var e PlaylistEntry
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlaylist).Get([]byte(id))
		if b == nil {
			return fmt.Errorf("miss")
		}
		return gobDecode(b, &e)
	})
	if len(e.Tracks) == 0 {
		return PlaylistEntry{}, false
	}
	if c.ttl > 0 && time.Since(e.CachedAt) > c.ttl {
		return PlaylistEntry{}, false
	}
	return e, true
}

func (c *Cache) setPlaylist(id string, e PlaylistEntry) {
	e.CachedAt = time.Now()
	data, err := gobEncode(e)
	if err != nil {
		return
	}
	_ = c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlaylist).Put([]byte(id), data)
	})
}

func (c *Cache) deletePlaylist(id string) {
	_ = c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlaylist).Delete([]byte(id))
	})
}
