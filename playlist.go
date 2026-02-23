package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Playlist struct {
	mu           sync.RWMutex
	playlistID   string
	tracks       []*Track
	shuffleMap   map[int]int
	currentIndex int
	isShuffled   bool
	isEnabled    bool
	yt           *YouTubeClient
	cache        *Cache
}

type playlistAPIResponse struct {
	Items []struct {
		Snippet struct {
			ResourceID struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
		} `json:"snippet"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

func newPlaylist(yt *YouTubeClient, c *Cache) *Playlist {
	return &Playlist{tracks: make([]*Track, 0), shuffleMap: make(map[int]int), yt: yt, cache: c}
}

func (m *Playlist) load(playlistURL string) error {
	pid := extractPlaylistID(playlistURL)
	if pid == "" {
		return fmt.Errorf("invalid playlist URL")
	}
	m.mu.Lock()
	m.playlistID = pid
	m.tracks = m.tracks[:0]
	m.currentIndex = 0
	m.mu.Unlock()

	if entry, ok := m.cache.getPlaylist(pid); ok {
		log.Printf("Playlist loaded from cache: %d tracks", len(entry.Tracks))
		m.mu.Lock()
		for _, t := range entry.Tracks {
			if !t.Embeddable {
				continue
			}
			m.tracks = append(m.tracks, &Track{
				VideoID: t.VideoID, Title: t.Title,
				DurationSec: t.DurationSec, Views: t.Views,
				AddedAt: time.Now(), AddedBy: "Playlist",
			})
		}
		m.reshuffleLocked()
		m.mu.Unlock()
		return nil
	}
	return m.fetchAndCache(pid)
}

func (m *Playlist) reload(playlistURL string) error {
	pid := extractPlaylistID(playlistURL)
	if pid == "" {
		return fmt.Errorf("invalid playlist URL")
	}
	m.cache.deletePlaylist(pid)
	return m.load(playlistURL)
}

func (m *Playlist) fetchAndCache(pid string) error {
	vids, err := m.fetchAllVideoIDs(pid)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var cTracks []PlaylistTrack
	ok, fail := 0, 0
	for _, vid := range vids {
		info, err := m.yt.getVideoInfoWithClient(vid, client)
		if err != nil || !info.Embeddable {
			fail++
			continue
		}
		m.mu.Lock()
		m.tracks = append(m.tracks, &Track{
			VideoID: vid, Title: info.Title,
			DurationSec: info.Duration, Views: info.Views,
			AddedAt: time.Now(), AddedBy: "Playlist",
		})
		m.mu.Unlock()
		cTracks = append(cTracks, PlaylistTrack{
			VideoID: vid, Title: info.Title,
			DurationSec: info.Duration, Views: info.Views, Embeddable: true,
		})
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("no valid tracks found in playlist")
	}
	log.Printf("Loaded playlist: %d tracks (%d skipped)", ok, fail)
	m.cache.setPlaylist(pid, PlaylistEntry{Tracks: cTracks})
	m.mu.Lock()
	m.reshuffleLocked()
	m.mu.Unlock()
	return nil
}

func (m *Playlist) fetchAllVideoIDs(pid string) ([]string, error) {
	var vids []string
	pageToken := ""
	client := &http.Client{Timeout: 20 * time.Second}
	if m.yt.apiKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	for {
		u := fmt.Sprintf(
			"https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&playlistId=%s&maxResults=50&key=%s",
			pid, m.yt.apiKey,
		)
		if pageToken != "" {
			u += "&pageToken=" + pageToken
		}
		page, err := fetchPlaylistPage(client, u)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if vid := item.Snippet.ResourceID.VideoID; vid != "" {
				vids = append(vids, vid)
			}
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return vids, nil
}

func fetchPlaylistPage(client *http.Client, u string) (*playlistAPIResponse, error) {
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube API returned status: %d", resp.StatusCode)
	}
	var ar playlistAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	return &ar, nil
}

func extractPlaylistID(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if pid := u.Query().Get("list"); len(pid) >= 2 && pid[:2] == "PL" {
			return pid
		}
	}
	if len(rawURL) >= 34 && rawURL[:2] == "PL" {
		return rawURL[:34]
	}
	return ""
}

func (m *Playlist) getNext() *Track {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isEnabled || len(m.tracks) == 0 {
		return nil
	}
	idx := m.currentIndex
	if m.isShuffled {
		if s, ok := m.shuffleMap[idx]; ok {
			idx = s
		}
	}
	if idx >= len(m.tracks) {
		idx = 0
	}
	src := m.tracks[idx]
	return &Track{VideoID: src.VideoID, Title: src.Title, DurationSec: src.DurationSec, Views: src.Views, AddedAt: time.Now(), AddedBy: "Playlist"}
}

func (m *Playlist) advanceToNext() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentIndex++
	if m.currentIndex >= len(m.tracks) {
		m.currentIndex = 0
		if m.isShuffled {
			m.reshuffleLocked()
		}
	}
}

func (m *Playlist) getAt(i int) *Track {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if i < 0 || i >= len(m.tracks) {
		return nil
	}
	src := m.tracks[i]
	return &Track{VideoID: src.VideoID, Title: src.Title, DurationSec: src.DurationSec, Views: src.Views, AddedAt: time.Now(), AddedBy: "Playlist"}
}

func (m *Playlist) goToPrevious() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentIndex--
	if m.currentIndex < 0 {
		m.currentIndex = len(m.tracks) - 1
	}
}

func (m *Playlist) jumpToIndex(i int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.tracks) {
		return fmt.Errorf("index out of range")
	}
	m.currentIndex = i
	return nil
}

func (m *Playlist) toggleShuffle() {
	m.mu.Lock()
	m.isShuffled = !m.isShuffled
	if m.isShuffled {
		m.reshuffleLocked()
	}
	log.Printf("Playlist shuffle %v", m.isShuffled)
	m.mu.Unlock()
}

func (m *Playlist) reshuffleLocked() {
	m.shuffleMap = make(map[int]int, len(m.tracks))
	indices := make([]int, len(m.tracks))
	for i := range indices {
		indices[i] = i
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
	for shuffled, original := range indices {
		m.shuffleMap[original] = shuffled
	}
}

func (m *Playlist) enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isEnabled = true
}

func (m *Playlist) disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isEnabled = false
}

func (m *Playlist) isEnabledVal() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isEnabled
}

func (m *Playlist) getTracks() []*Track {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracks
}

func (m *Playlist) currentIndexVal() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentIndex
}

func (m *Playlist) status() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"enabled":       m.isEnabled,
		"shuffled":      m.isShuffled,
		"playlist_id":   m.playlistID,
		"total_tracks":  len(m.tracks),
		"current_index": m.currentIndex,
		"loaded":        len(m.tracks) > 0,
	}
}

func (m *Playlist) isShuffledVal() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isShuffled
}

func (m *Playlist) getPlaylistID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.playlistID
}

func (m *Playlist) loaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tracks) > 0
}

func (m *Playlist) lenVal() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tracks)
}
