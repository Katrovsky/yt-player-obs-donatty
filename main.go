package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

//go:embed dashboard.html overlay.html dock.html
var staticFiles embed.FS

var (
	dm      *DonationMonitor
	pm      *PlaylistManager
	conf    Config
	q       = &PriorityQueue{}
	hist    []*Track
	cur     *Track
	state   = "stopped"
	clients = make(map[*websocket.Conn]bool)
	mu      sync.RWMutex
	bc      = make(chan PlayerState, 100)
	up      = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	cache   PlayerState
	dirty   = true
	ytCache = make(map[string]*YouTubeVideoInfo)
	ytMu    sync.RWMutex
)

type Config struct {
	Port                  int    `json:"port"`
	MaxDurationMinutes    int    `json:"max_duration_minutes"`
	MinViews              int    `json:"min_views"`
	RepeatLimit           int    `json:"repeat_limit"`
	CleanupAfterHours     int    `json:"cleanup_after_hours"`
	MaxQueueSize          int    `json:"max_queue_size"`
	DonationWidgetURL     string `json:"donation_widget_url"`
	DonationMinAmount     int    `json:"donation_min_amount"`
	DonationCheckInterval int    `json:"donation_check_interval"`
	YouTubeAPIKey         string `json:"youtube_api_key"`
	FallbackPlaylistURL   string `json:"fallback_playlist_url"`
}

type Track struct {
	VideoID     string    `json:"video_id"`
	Title       string    `json:"title"`
	DurationSec int       `json:"duration_sec"`
	Views       int       `json:"views"`
	AddedAt     time.Time `json:"added_at"`
	AddedBy     string    `json:"added_by,omitempty"`
	IsPaid      bool      `json:"is_paid"`
}

type PriorityQueue struct {
	items []*Track
	mu    sync.RWMutex
}

func (pq *PriorityQueue) Add(t *Track) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.addLocked(t, false)
}

func (pq *PriorityQueue) AddFront(t *Track) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.addLocked(t, true)
}

func (pq *PriorityQueue) addLocked(t *Track, front bool) {
	if t.IsPaid || front {
		pos := 0
		if !front {
			for i, tr := range pq.items {
				if !tr.IsPaid {
					break
				}
				pos = i + 1
			}
		}
		pq.items = append(pq.items[:pos], append([]*Track{t}, pq.items[pos:]...)...)
	} else {
		pq.items = append(pq.items, t)
	}
}

func (pq *PriorityQueue) Next() *Track {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if len(pq.items) == 0 {
		return nil
	}
	t := pq.items[0]
	pq.items = pq.items[1:]
	return t
}

func (pq *PriorityQueue) GetState() (int, []*Track) {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	r := make([]*Track, len(pq.items))
	copy(r, pq.items)
	return len(pq.items), r
}

func (pq *PriorityQueue) RemoveAt(i int) *Track {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if i < 0 || i >= len(pq.items) {
		return nil
	}
	t := pq.items[i]
	pq.items = append(pq.items[:i], pq.items[i+1:]...)
	return t
}

func (pq *PriorityQueue) Clear() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.items = []*Track{}
}

type PlayerState struct {
	Action   string   `json:"action"`
	Current  *Track   `json:"current,omitempty"`
	Queue    []*Track `json:"queue,omitempty"`
	Position int      `json:"position"`
}

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func main() {
	loadConfig()
	routes := map[string]http.HandlerFunc{
		"/api/add":              handleAddByURL,
		"/api/add-url":          handleAddByURL,
		"/api/play":             handlePlay,
		"/api/pause":            handlePause,
		"/api/stop":             handleStop,
		"/api/next":             handleNext,
		"/api/previous":         handlePrev,
		"/api/status":           handleStatus,
		"/api/queue":            handleQueueList,
		"/api/nowplaying":       handleNowPlaying,
		"/api/remove":           handleRemove,
		"/api/clear":            handleClear,
		"/api/playlist/set":     handlePlaylistSet,
		"/api/playlist/enable":  handlePlaylistEnable,
		"/api/playlist/disable": handlePlaylistDisable,
		"/api/playlist/status":  handlePlaylistStatus,
		"/api/playlist/reload":  handlePlaylistReload,
		"/api/playlist/tracks":  handlePlaylistTracks,
		"/api/playlist/jump":    handlePlaylistJump,
		"/api/playlist/shuffle": handlePlaylistShuffle,
		"/api/donation/status":  handleDonationStatus,
	}
	for p, h := range routes {
		http.HandleFunc(p, corsMiddleware(h))
	}
	http.HandleFunc("/", handleDashboard)
	http.HandleFunc("/overlay", handleOverlay)
	http.HandleFunc("/dock", handleDock)
	http.HandleFunc("/ws", handleWS)

	serverStarted := make(chan bool)
	go func() {
		log.Printf("Server started on :%d", conf.Port)
		serverStarted <- true
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), nil))
	}()

	go broadcaster()
	go cleanupOldTracks()
	go watchConfig()

	if conf.FallbackPlaylistURL != "" {
		pm = NewPlaylistManager(conf.FallbackPlaylistURL)
		if pm != nil {
			pm.Enable()
		}
	}

	if conf.DonationWidgetURL != "" {
		dm = NewDonationMonitor(conf.DonationWidgetURL, conf.DonationMinAmount)
		go dm.Start()
	}

	<-serverStarted
	log.Println("Dashboard is now accessible")

	select {}
}

func loadConfig() {
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("Failed to load config.json:", err)
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		log.Fatal("Failed to parse config.json:", err)
	}
	if conf.MaxQueueSize == 0 {
		conf.MaxQueueSize = 100
	}
}

func watchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Error creating watcher:", err)
	}
	defer watcher.Close()

	err = watcher.Add(filepath.Dir("config.json"))
	if err != nil {
		log.Fatal("Error adding config directory to watcher:", err)
	}

	for {
		select {
		case event := <-watcher.Events:
			if event.Name == "config.json" && event.Has(fsnotify.Write) {
				log.Println("Config file changed, reloading...")
				reloadConfig()
			}
		case err := <-watcher.Errors:
			log.Println("Watcher error:", err)
		}
	}
}

func reloadConfig() {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return
	}
	var nc Config
	if err := json.Unmarshal(data, &nc); err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	conf = nc
	log.Println("Config reloaded")
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func respondJSON(w http.ResponseWriter, sc int, resp APIResponse) {
	w.WriteHeader(sc)
	json.NewEncoder(w).Encode(resp)
}

func validateTrack(t Track) error {
	if conf.MaxDurationMinutes > 0 && t.DurationSec > conf.MaxDurationMinutes*60 {
		return fmt.Errorf("track too long (max %d minutes)", conf.MaxDurationMinutes)
	}
	if conf.MinViews > 0 && t.Views < conf.MinViews {
		return fmt.Errorf("insufficient views (min %d)", conf.MinViews)
	}
	if !canRepeat(t.VideoID) {
		return fmt.Errorf("track recently played (repeat limit reached)")
	}
	return nil
}

func validateAndAddTrack(vid, by string, paid bool) error {
	vi, err := GetYouTubeVideoInfo(vid)
	if err != nil {
		return err
	}
	t := &Track{VideoID: vid, Title: vi.Title, DurationSec: vi.Duration, Views: vi.Views, AddedAt: time.Now(), AddedBy: by, IsPaid: paid}
	if err := validateTrack(*t); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	l, _ := q.GetState()
	tot := l
	if cur != nil {
		tot++
	}
	if tot >= conf.MaxQueueSize {
		return fmt.Errorf("queue is full (max %d tracks)", conf.MaxQueueSize)
	}
	q.Add(t)
	log.Printf("Added: %s by %s (paid=%v)", t.Title, by, paid)
	empty := tot == 0
	if state == "stopped" && empty {
		playNext()
	}
	dirty = true
	bc <- currentState()
	return nil
}

func handleAddByURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	url := r.URL.Query().Get("url")
	if url == "" {
		url = r.URL.Query().Get("id")
	}
	by := r.URL.Query().Get("user")
	if by == "" {
		by = "User"
	}
	paid := r.URL.Query().Get("paid")
	if url == "" {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Missing video URL"})
		return
	}
	vid := ExtractYouTubeID(url)
	if vid == "" {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid YouTube URL"})
		return
	}
	if err := validateAndAddTrack(vid, by, paid == "true"); err != nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Track added to queue"})
}

func canRepeat(id string) bool {
	if conf.RepeatLimit == 0 {
		return true
	}
	cnt := 0
	for i := len(hist) - 1; i >= 0 && cnt < conf.RepeatLimit; i-- {
		if hist[i].VideoID == id {
			cnt++
		}
	}
	return cnt < conf.RepeatLimit
}

func handlePlay(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	l, _ := q.GetState()
	if l == 0 && cur == nil {
		if pm != nil && pm.isEnabled {
			playNext()
			dirty = true
			bc <- currentState()
			respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playback started"})
			return
		}
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Queue is empty"})
		return
	}
	if cur == nil && l > 0 {
		playNext()
	} else if state != "playing" {
		state = "playing"
	}
	if state == "playing" {
		log.Println("Playing")
		dirty = true
		bc <- currentState()
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playback started"})
}

func handlePause(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	if state != "paused" {
		state = "paused"
		log.Println("Paused")
		dirty = true
		bc <- currentState()
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playback paused"})
}

func handleStop(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	if state != "stopped" {
		state = "stopped"
		if pm != nil {
			pm.Disable()
		}
		log.Println("Stopped")
		dirty = true
		bc <- currentState()
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Playback stopped"})
}

func handleNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if cur != nil {
		hist = append(hist, cur)
		if len(hist) > 100 {
			hist = hist[1:]
		}
		if cur.AddedBy == "Playlist" && pm != nil {
			pm.AdvanceToNext()
		}
	}
	playNext()
	dirty = true
	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Skipped to next track"})
}

func playNext() {
	if t := q.Next(); t != nil {
		cur = t
		state = "playing"
		if pm != nil {
			pm.SetInterrupted(true)
		}
		log.Printf("Next track: %s", cur.Title)
		return
	}
	if pm != nil && pm.WasPlaying() {
		cur = pm.GetNext()
		if cur != nil {
			state = "playing"
			log.Printf("Next track (playlist): %s", cur.Title)
			return
		}
	}
	cur = nil
	state = "stopped"
	log.Println("Queue finished")
}

func handlePrev(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hist) == 0 {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No previous track available"})
		return
	}
	if cur != nil {
		if cur.AddedBy == "Playlist" && pm != nil {
			pm.GoToPrevious()
		} else {
			q.AddFront(cur)
		}
	}
	prev := hist[len(hist)-1]
	hist = hist[:len(hist)-1]
	if prev.AddedBy == "Playlist" && pm != nil {
		pm.GoToPrevious()
	}
	cur = prev
	state = "playing"
	log.Printf("Previous track: %s", cur.Title)
	dirty = true
	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Returned to previous track"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	l, _ := q.GetState()
	tot := l
	if cur != nil {
		tot++
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"state": state, "current": cur, "position": len(hist) + 1, "queue_length": tot}})
}

func handleQueueList(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	_, items := q.GetState()
	all := make([]*Track, 0)
	all = append(all, hist...)
	pos := len(hist)
	if cur != nil {
		all = append(all, cur)
	}
	all = append(all, items...)
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"queue": all, "current": pos, "state": state, "total": len(all)}})
}

func handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid index parameter"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	t := q.RemoveAt(idx)
	if t == nil {
		respondJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Index out of range"})
		return
	}
	log.Printf("Removed: %s", t.Title)
	dirty = true
	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Track removed from queue", Data: t})
}

func handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	l, _ := q.GetState()
	sz := l
	if cur != nil {
		sz++
	}
	q.Clear()
	cur = nil
	state = "stopped"
	log.Printf("Queue cleared (%d tracks removed)", sz)
	dirty = true
	bc <- currentState()
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Message: fmt.Sprintf("Queue cleared (%d tracks removed)", sz)})
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleOverlay(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("overlay.html")
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleDock(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("dock.html")
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	mu.Lock()
	clients[conn] = true
	mu.Unlock()
	mu.RLock()
	st := currentState()
	mu.RUnlock()
	conn.WriteJSON(st)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			mu.Lock()
			delete(clients, conn)
			mu.Unlock()
			conn.Close()
			break
		}
	}
}

func broadcaster() {
	for st := range bc {
		mu.RLock()
		cs := make([]*websocket.Conn, 0, len(clients))
		for c := range clients {
			cs = append(cs, c)
		}
		mu.RUnlock()
		var failed []*websocket.Conn
		for _, c := range cs {
			if err := c.WriteJSON(st); err != nil {
				c.Close()
				failed = append(failed, c)
			}
		}
		if len(failed) > 0 {
			mu.Lock()
			for _, c := range failed {
				delete(clients, c)
			}
			mu.Unlock()
		}
	}
}

func currentState() PlayerState {
	if !dirty {
		return cache
	}
	_, items := q.GetState()
	all := make([]*Track, 0, len(hist)+1+len(items))
	all = append(all, hist...)
	pos := len(hist)
	if cur != nil {
		all = append(all, cur)
	}
	all = append(all, items...)
	cache = PlayerState{Action: state, Current: cur, Queue: all, Position: pos}
	dirty = false
	return cache
}

func cleanupOldTracks() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		mu.Lock()
		if conf.CleanupAfterHours > 0 {
			cutoff := time.Now().Add(-time.Duration(conf.CleanupAfterHours) * time.Hour)
			_, items := q.GetState()
			new := []*Track{}
			for _, t := range items {
				if t.AddedAt.After(cutoff) {
					new = append(new, t)
				}
			}
			removed := len(items) - len(new)
			if removed > 0 {
				q.Clear()
				for _, t := range new {
					q.Add(t)
				}
				l, _ := q.GetState()
				if l == 0 && cur == nil {
					state = "stopped"
				}
				log.Printf("Cleanup: removed %d old tracks", removed)
				dirty = true
				bc <- currentState()
			}
		}
		mu.Unlock()
	}
}

func handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	resp := map[string]interface{}{"status": state, "artist": "", "title": "", "url": ""}
	if cur != nil {
		full := cur.Title
		art := ""
		tit := full
		if idx := strings.Index(full, " - "); idx != -1 {
			art = strings.TrimSpace(full[:idx])
			tit = strings.TrimSpace(full[idx+3:])
		}
		resp["artist"] = art
		resp["title"] = tit
		resp["full_title"] = full
		resp["url"] = fmt.Sprintf("https://www.youtube.com/watch?v=%s", cur.VideoID)
	}
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: resp})
}

func handleDonationStatus(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	enabled := dm != nil
	respondJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"enabled": enabled}})
}
