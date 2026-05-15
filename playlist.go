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
	order        []int
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
	return &Playlist{
		tracks:       make([]*Track, 0),
		currentIndex: -1,
		yt:           yt,
		cache:        c,
	}
}

func (pl *Playlist) load(playlistURL string) error {
	pid := extractPlaylistID(playlistURL)
	if pid == "" {
		return fmt.Errorf("invalid playlist URL")
	}
	pl.mu.Lock()
	pl.playlistID = pid
	pl.tracks = pl.tracks[:0]
	pl.currentIndex = -1
	pl.mu.Unlock()

	if entry, ok := pl.cache.getPlaylist(pid); ok {
		log.Printf("Playlist loaded from cache: %d tracks", len(entry.Tracks))
		pl.mu.Lock()
		for _, t := range entry.Tracks {
			if !t.Embeddable {
				continue
			}
			pl.tracks = append(pl.tracks, &Track{
				VideoID:     t.VideoID,
				Title:       t.Title,
				DurationSec: t.DurationSec,
				Views:       t.Views,
				AddedAt:     time.Now(),
				AddedBy:     "Playlist",
			})
		}
		pl.buildOrderLocked()
		pl.mu.Unlock()
		return nil
	}
	return pl.fetchAndCache(pid)
}

func (pl *Playlist) reload(playlistURL string) error {
	pid := extractPlaylistID(playlistURL)
	if pid == "" {
		return fmt.Errorf("invalid playlist URL")
	}
	pl.cache.deletePlaylist(pid)
	return pl.load(playlistURL)
}

func (pl *Playlist) fetchAndCache(pid string) error {
	vids, err := pl.fetchAllVideoIDs(pid)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var cTracks []PlaylistTrack
	ok, fail := 0, 0
	for _, vid := range vids {
		info, err := pl.yt.getVideoInfoWithClient(vid, client)
		if err != nil || !info.Embeddable {
			fail++
			continue
		}
		pl.mu.Lock()
		pl.tracks = append(pl.tracks, &Track{
			VideoID:     vid,
			Title:       info.Title,
			DurationSec: info.Duration,
			Views:       info.Views,
			AddedAt:     time.Now(),
			AddedBy:     "Playlist",
		})
		pl.mu.Unlock()
		cTracks = append(cTracks, PlaylistTrack{
			VideoID:     vid,
			Title:       info.Title,
			DurationSec: info.Duration,
			Views:       info.Views,
			Embeddable:  true,
			CategoryId:  "10",
		})
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("no valid tracks found in playlist")
	}
	log.Printf("Loaded playlist: %d tracks (%d skipped)", ok, fail)
	pl.cache.setPlaylist(pid, PlaylistEntry{Tracks: cTracks})
	pl.mu.Lock()
	pl.buildOrderLocked()
	pl.mu.Unlock()
	return nil
}

func (pl *Playlist) fetchAllVideoIDs(pid string) ([]string, error) {
	if pl.yt.apiKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var vids []string
	pageToken := ""
	for {
		u := fmt.Sprintf(
			"https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&playlistId=%s&maxResults=50&key=%s",
			pid, pl.yt.apiKey,
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

// getNext returns the next track in sequence and advances currentIndex.
// Called only when the main queue is exhausted.
func (pl *Playlist) getNext() *Track {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if !pl.isEnabled || len(pl.tracks) == 0 {
		return nil
	}
	next := pl.currentIndex + 1
	if next >= len(pl.tracks) {
		next = 0
		if pl.isShuffled {
			pl.buildOrderLocked()
		}
	}
	pl.currentIndex = next
	return pl.trackAtLocked(pl.currentIndex)
}

// jumpTo sets currentIndex to the given track (by original slice index) and returns it.
func (pl *Playlist) jumpTo(trackIdx int) *Track {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if trackIdx < 0 || trackIdx >= len(pl.tracks) {
		return nil
	}
	if pl.isShuffled {
		for i, v := range pl.order {
			if v == trackIdx {
				pl.currentIndex = i
				break
			}
		}
	} else {
		pl.currentIndex = trackIdx
	}
	return pl.trackAtLocked(pl.currentIndex)
}

// activeTrackIndex returns the index of the currently playing track
// in the original (display) slice, for correct UI highlighting.
func (pl *Playlist) activeTrackIndex() int {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.activeTrackIndexLocked()
}

func (pl *Playlist) activeTrackIndexLocked() int {
	if pl.currentIndex < 0 || len(pl.tracks) == 0 {
		return -1
	}
	if pl.isShuffled && pl.currentIndex < len(pl.order) {
		return pl.order[pl.currentIndex]
	}
	return pl.currentIndex
}

func (pl *Playlist) trackAtLocked(pos int) *Track {
	if pos < 0 || len(pl.tracks) == 0 {
		return nil
	}
	idx := pos
	if pl.isShuffled && pos < len(pl.order) {
		idx = pl.order[pos]
	}
	if idx < 0 || idx >= len(pl.tracks) {
		return nil
	}
	src := pl.tracks[idx]
	return &Track{
		VideoID:     src.VideoID,
		Title:       src.Title,
		DurationSec: src.DurationSec,
		Views:       src.Views,
		AddedAt:     time.Now(),
		AddedBy:     "Playlist",
	}
}

func (pl *Playlist) toggleShuffle() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.isShuffled = !pl.isShuffled
	pl.currentIndex = -1
	if pl.isShuffled {
		pl.buildOrderLocked()
	}
	log.Printf("Playlist shuffle: %v", pl.isShuffled)
}

// buildOrderLocked builds a shuffled index mapping. order[pos] = actual track index.
func (pl *Playlist) buildOrderLocked() {
	n := len(pl.tracks)
	pl.order = make([]int, n)
	for i := range pl.order {
		pl.order[i] = i
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(n, func(i, j int) { pl.order[i], pl.order[j] = pl.order[j], pl.order[i] })
}

func (pl *Playlist) enable() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.isEnabled = true
}

func (pl *Playlist) disable() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.isEnabled = false
}

func (pl *Playlist) getTracks() []*Track {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.tracks
}

func (pl *Playlist) status() map[string]any {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return map[string]any{
		"enabled":       pl.isEnabled,
		"shuffled":      pl.isShuffled,
		"playlist_id":   pl.playlistID,
		"total_tracks":  len(pl.tracks),
		"current_index": pl.activeTrackIndexLocked(),
		"loaded":        len(pl.tracks) > 0,
	}
}

func (pl *Playlist) currentIndexVal() int { return pl.activeTrackIndex() }
func (pl *Playlist) isEnabledVal() bool   { pl.mu.RLock(); defer pl.mu.RUnlock(); return pl.isEnabled }
func (pl *Playlist) isShuffledVal() bool  { pl.mu.RLock(); defer pl.mu.RUnlock(); return pl.isShuffled }
func (pl *Playlist) getPlaylistID() string {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.playlistID
}
func (pl *Playlist) lenVal() int  { pl.mu.RLock(); defer pl.mu.RUnlock(); return len(pl.tracks) }
func (pl *Playlist) loaded() bool { pl.mu.RLock(); defer pl.mu.RUnlock(); return len(pl.tracks) > 0 }
