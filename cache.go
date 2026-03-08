package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	maxVideos    = 50000
	maxPlaylists = 500
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
	CategoryId string
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
	CategoryId  string
}

type Cache struct {
	db *bolt.DB
}

func openCache(path string, _ time.Duration) (*Cache, error) {
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
	return &Cache{db: db}, nil
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
			return nil
		}
		return gobDecode(b, &e)
	})
	if e.Title == "" {
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
		bkt := tx.Bucket(bucketVideos)
		count := bkt.Stats().KeyN
		if count >= maxVideos {
			if err := evictOldestFromBucket(bkt, count-maxVideos+1); err != nil {
				log.Printf("Video cache eviction error: %v", err)
			}
		}
		return bkt.Put([]byte(id), data)
	})
}

func (c *Cache) getPlaylist(id string) (PlaylistEntry, bool) {
	var e PlaylistEntry
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlaylist).Get([]byte(id))
		if b == nil {
			return nil
		}
		return gobDecode(b, &e)
	})
	if len(e.Tracks) == 0 {
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
		bkt := tx.Bucket(bucketPlaylist)
		count := bkt.Stats().KeyN
		if count >= maxPlaylists {
			if err := evictOldestFromBucket(bkt, count-maxPlaylists+1); err != nil {
				log.Printf("Playlist cache eviction error: %v", err)
			}
		}
		return bkt.Put([]byte(id), data)
	})
}

func (c *Cache) deletePlaylist(id string) {
	_ = c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlaylist).Delete([]byte(id))
	})
}

func evictOldestFromBucket(bkt *bolt.Bucket, n int) error {
	type kv struct {
		key      []byte
		cachedAt time.Time
	}
	var entries []kv
	_ = bkt.ForEach(func(k, v []byte) error {
		var cachedAt time.Time
		var ve VideoEntry
		if err := gobDecode(v, &ve); err == nil && !ve.CachedAt.IsZero() {
			cachedAt = ve.CachedAt
		} else {
			var pe PlaylistEntry
			if err := gobDecode(v, &pe); err == nil && !pe.CachedAt.IsZero() {
				cachedAt = pe.CachedAt
			}
		}
		entries = append(entries, kv{key: append([]byte{}, k...), cachedAt: cachedAt})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].cachedAt.Before(entries[j].cachedAt)
	})
	if n > len(entries) {
		n = len(entries)
	}
	for i := 0; i < n; i++ {
		if err := bkt.Delete(entries[i].key); err != nil {
			return err
		}
	}
	return nil
}
