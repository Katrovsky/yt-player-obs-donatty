package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type PlaylistManager struct {
	playlistID   string
	tracks       []*Track
	shuffleMap   map[int]int
	currentIndex int
	isShuffled   bool
	isEnabled    bool
	wasPlaying   bool
	mu           sync.RWMutex
}

type PlaylistAPIResponse struct {
	Items []struct {
		Snippet struct {
			ResourceID struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
		} `json:"snippet"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

func NewPlaylistManager(pu string) *PlaylistManager {
	pm := &PlaylistManager{
		tracks:     make([]*Track, 0),
		shuffleMap: make(map[int]int),
		isShuffled: false,
		isEnabled:  false,
		wasPlaying: false,
	}
	if pu != "" && pm.LoadPlaylist(pu) != nil {
		log.Printf("Failed to load playlist")
		return nil
	}
	return pm
}

func (pm *PlaylistManager) LoadPlaylist(pu string) error {
	pid := ExtractPlaylistID(pu)
	if pid == "" {
		return fmt.Errorf("invalid playlist URL")
	}
	pm.mu.Lock()
	pm.playlistID = pid
	pm.tracks = make([]*Track, 0)
	pm.currentIndex = 0
	pm.mu.Unlock()
	vids, err := pm.fetchAllVideoIDs(pid)
	if err != nil {
		return err
	}
	sc, fc := 0, 0
	for _, vid := range vids {
		vi, err := GetYouTubeVideoInfo(vid)
		if err != nil {
			fc++
			continue
		}
		t := &Track{VideoID: vid, Title: vi.Title, DurationSec: vi.Duration, Views: vi.Views, AddedAt: time.Now(), AddedBy: "Playlist", IsPaid: false}
		pm.mu.Lock()
		pm.tracks = append(pm.tracks, t)
		pm.mu.Unlock()
		sc++
	}
	if sc == 0 {
		return fmt.Errorf("no valid tracks found in playlist")
	}
	log.Printf("Loaded playlist: %d tracks (%d skipped)", sc, fc)
	pm.createShuffleMap()
	return nil
}

func (pm *PlaylistManager) fetchAllVideoIDs(pid string) ([]string, error) {
	if conf.YouTubeAPIKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	var vids []string
	npt := ""
	client := &http.Client{Timeout: 10 * time.Second}
	for {
		url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&playlistId=%s&maxResults=50&key=%s", pid, conf.YouTubeAPIKey)
		if npt != "" {
			url += "&pageToken=" + npt
		}
		ar, err := pm.fetchPlaylistPage(client, url)
		if err != nil {
			return nil, err
		}
		for _, item := range ar.Items {
			if vid := item.Snippet.ResourceID.VideoID; vid != "" {
				vids = append(vids, vid)
			}
		}
		if ar.NextPageToken == "" {
			break
		}
		npt = ar.NextPageToken
	}
	return vids, nil
}

func (pm *PlaylistManager) fetchPlaylistPage(client *http.Client, url string) (*PlaylistAPIResponse, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube API returned status: %d", resp.StatusCode)
	}
	var ar PlaylistAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	return &ar, nil
}

func ExtractPlaylistID(url string) string {
	if len(url) == 34 && url[:2] == "PL" {
		return url
	}
	start := 0
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '=' {
			start = i + 1
			break
		}
	}
	if start > 0 && len(url) >= start+34 {
		pid := url[start : start+34]
		if pid[:2] == "PL" {
			return pid
		}
	}
	return ""
}

func (pm *PlaylistManager) createShuffleMap() {
	pm.shuffleMap = make(map[int]int)
	indices := make([]int, len(pm.tracks))
	for i := range indices {
		indices[i] = i
	}
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})
	for shuffled, original := range indices {
		pm.shuffleMap[original] = shuffled
	}
}

func (pm *PlaylistManager) GetNext() *Track {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if !pm.isEnabled || len(pm.tracks) == 0 {
		return nil
	}

	actualIndex := pm.currentIndex
	if pm.isShuffled {
		if shuffledPos, ok := pm.shuffleMap[pm.currentIndex]; ok {
			actualIndex = shuffledPos
		}
	}

	if actualIndex >= len(pm.tracks) {
		actualIndex = 0
	}

	ot := pm.tracks[actualIndex]
	return &Track{VideoID: ot.VideoID, Title: ot.Title, DurationSec: ot.DurationSec, Views: ot.Views, AddedAt: time.Now(), AddedBy: "Playlist", IsPaid: false}
}

func (pm *PlaylistManager) AdvanceToNext() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.currentIndex++

	if pm.currentIndex >= len(pm.tracks) {
		pm.currentIndex = 0
		if pm.isShuffled {
			pm.createShuffleMap()
		}
	}
}

func (pm *PlaylistManager) GoToPrevious() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.currentIndex--

	if pm.currentIndex < 0 {
		pm.currentIndex = len(pm.tracks) - 1
	}
}

func (pm *PlaylistManager) JumpToIndex(i int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if i < 0 || i >= len(pm.tracks) {
		return fmt.Errorf("index out of range")
	}
	pm.currentIndex = i
	return nil
}

func (pm *PlaylistManager) Shuffle() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.isShuffled = !pm.isShuffled
	if pm.isShuffled {
		pm.createShuffleMap()
	}
	log.Printf("Playlist shuffle %s", map[bool]string{true: "enabled", false: "disabled"}[pm.isShuffled])

	mu.Lock()
	dirty = true
	bc <- currentState()
	mu.Unlock()
}

func (pm *PlaylistManager) Enable() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.isEnabled = true
	pm.wasPlaying = true
	log.Println("Playlist enabled")
}

func (pm *PlaylistManager) Disable() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.isEnabled = false
	pm.wasPlaying = false
	log.Println("Playlist disabled")
}

func (pm *PlaylistManager) SetInterrupted(i bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if i {
		pm.wasPlaying = pm.isEnabled
	}
}

func (pm *PlaylistManager) WasPlaying() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.wasPlaying && pm.isEnabled
}

func (pm *PlaylistManager) GetStatus() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return map[string]interface{}{
		"enabled":       pm.isEnabled,
		"shuffled":      pm.isShuffled,
		"playlist_id":   pm.playlistID,
		"total_tracks":  len(pm.tracks),
		"current_index": pm.currentIndex,
		"was_playing":   pm.wasPlaying,
		"loaded":        len(pm.tracks) > 0,
	}
}

func (pm *PlaylistManager) GetTracks() []*Track {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.tracks
}

func handlePlaylistSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	pu := r.URL.Query().Get("url")
	if pu == "" {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Missing playlist URL"})
		return
	}
	newPm := NewPlaylistManager(pu)
	if newPm == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Failed to load playlist"})
		return
	}
	mu.Lock()
	pm = newPm
	dirty = true
	mu.Unlock()

	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playlist loaded successfully", Data: newPm.GetStatus()})
}

func handlePlaylistEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No playlist loaded"})
		return
	}
	p.Enable()
	mu.Lock()
	if state == "stopped" && cur == nil {
		cur = p.GetNext()
		if cur != nil {
			state = "playing"
			dirty = true
		}
	}
	mu.Unlock()

	if cur != nil {
		bc <- currentState()
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playlist enabled", Data: p.GetStatus()})
}

func handlePlaylistDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No playlist loaded"})
		return
	}
	p.Disable()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playlist disabled", Data: p.GetStatus()})
}

func handlePlaylistStatus(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"enabled": false, "loaded": false}})
		return
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: p.GetStatus()})
}

func handlePlaylistReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No playlist loaded"})
		return
	}
	p.mu.RLock()
	pu := "https://www.youtube.com/playlist?list=" + p.playlistID
	p.mu.RUnlock()
	if err := p.LoadPlaylist(pu); err != nil {
		respondJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to reload playlist: " + err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playlist reloaded successfully", Data: p.GetStatus()})
}

func handlePlaylistTracks(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"tracks": []interface{}{}}})
		return
	}
	ts := p.GetTracks()
	p.mu.RLock()
	ci := p.currentIndex
	p.mu.RUnlock()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"tracks": ts, "current_index": ci, "total": len(ts)}})
}

func handlePlaylistJump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid index parameter"})
		return
	}
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No playlist loaded"})
		return
	}
	if err := p.JumpToIndex(idx); err != nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	mu.Lock()
	if cur != nil && cur.AddedBy == "Playlist" {
		hist = append(hist, cur)
		if len(hist) > 100 {
			hist = hist[1:]
		}
	}
	cur = p.GetNext()
	if cur != nil {
		state = "playing"
		dirty = true
	}
	mu.Unlock()

	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Jumped to track"})
}

func handlePlaylistShuffle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.RLock()
	p := pm
	mu.RUnlock()
	if p == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No playlist loaded"})
		return
	}
	p.Shuffle()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playlist shuffle toggled", Data: p.GetStatus()})
}
