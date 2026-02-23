package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"

	"yt-player/internal/player"
	"yt-player/internal/playlist"
	"yt-player/internal/youtube"
)

type response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type Hub struct {
	mu       sync.Mutex
	conns    map[*websocket.Conn]struct{}
	upgrader websocket.Upgrader
}

func NewHub() *Hub {
	return &Hub{
		conns:    make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

func (h *Hub) Send(st player.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if err := c.WriteJSON(st); err != nil {
			c.Close()
			delete(h.conns, c)
		}
	}
}

type Server struct {
	p           *player.Player
	hub         *Hub
	yt          *youtube.Client
	dm          bool
	staticFiles embed.FS
}

func NewServer(p *player.Player, hub *Hub, yt *youtube.Client, donationEnabled bool, static embed.FS) *Server {
	return &Server{p: p, hub: hub, yt: yt, dm: donationEnabled, staticFiles: static}
}

func (s *Server) Register(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"/api/add":              s.handleAdd,
		"/api/add-url":          s.handleAdd,
		"/api/play":             s.handlePlay,
		"/api/pause":            s.handlePause,
		"/api/stop":             s.handleStop,
		"/api/next":             s.handleNext,
		"/api/previous":         s.handlePrevious,
		"/api/status":           s.handleStatus,
		"/api/queue":            s.handleQueue,
		"/api/nowplaying":       s.handleNowPlaying,
		"/api/remove":           s.handleRemove,
		"/api/clear":            s.handleClear,
		"/api/playlist/set":     s.handlePlaylistSet,
		"/api/playlist/enable":  s.handlePlaylistEnable,
		"/api/playlist/disable": s.handlePlaylistDisable,
		"/api/playlist/status":  s.handlePlaylistStatus,
		"/api/playlist/reload":  s.handlePlaylistReload,
		"/api/playlist/tracks":  s.handlePlaylistTracks,
		"/api/playlist/jump":    s.handlePlaylistJump,
		"/api/playlist/shuffle": s.handlePlaylistShuffle,
		"/api/donation/status":  s.handleDonationStatus,
	}
	for path, h := range routes {
		mux.HandleFunc(path, cors(h))
	}
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/", s.handleStatic("dashboard.html", "text/html"))
	mux.HandleFunc("/overlay", s.handleStatic("overlay.html", "text/html"))
	mux.HandleFunc("/dock", s.handleStatic("dock.html", "text/html"))
}

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func reply(w http.ResponseWriter, code int, r response) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(r)
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, response{Success: false, Message: "Method not allowed"})
		return false
	}
	return true
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		rawURL = r.URL.Query().Get("id")
	}
	by := r.URL.Query().Get("user")
	if by == "" {
		by = "User"
	}
	paid := r.URL.Query().Get("paid") == "true"
	if rawURL == "" {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "Missing video URL"})
		return
	}
	vid := youtube.ExtractID(rawURL)
	if vid == "" {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "Invalid YouTube URL"})
		return
	}
	if err := s.p.ValidateAndAdd(vid, by, paid); err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Track added to queue"})
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.p.Play(); err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Playback started"})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.p.Pause()
	reply(w, http.StatusOK, response{Success: true, Message: "Playback paused"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.p.Stop()
	reply(w, http.StatusOK, response{Success: true, Message: "Playback stopped"})
}

func (s *Server) handleNext(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.p.Next()
	reply(w, http.StatusOK, response{Success: true, Message: "Skipped to next track"})
}

func (s *Server) handlePrevious(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.p.Previous(); err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Returned to previous track"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, response{Success: true, Data: s.p.Status()})
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	st := s.p.CurrentState()
	reply(w, http.StatusOK, response{Success: true, Data: map[string]any{
		"queue":   st.Queue,
		"current": st.Position,
		"state":   st.Action,
		"total":   len(st.Queue),
	}})
}

func (s *Server) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, response{Success: true, Data: s.p.NowPlaying()})
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		reply(w, http.StatusMethodNotAllowed, response{Success: false, Message: "Method not allowed"})
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "Invalid index parameter"})
		return
	}
	t, err := s.p.Remove(idx)
	if err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Track removed from queue", Data: t})
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	n := s.p.Clear()
	reply(w, http.StatusOK, response{Success: true, Message: fmt.Sprintf("Queue cleared (%d tracks removed)", n)})
}

func (s *Server) handlePlaylistSet(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	pu := r.URL.Query().Get("url")
	if pu == "" {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "Missing playlist URL"})
		return
	}
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusInternalServerError, response{Success: false, Message: "Playlist manager not initialized"})
		return
	}
	if err := pl.Load(pu); err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Playlist loaded successfully", Data: pl.Status()})
}

func (s *Server) handlePlaylistEnable(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "No playlist loaded"})
		return
	}
	pl.Enable()
	s.p.BroadcastPlaylistUpdate()
	if err := s.p.Play(); err != nil {
		log.Println("Play after enable:", err)
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Playlist enabled", Data: pl.Status()})
}

func (s *Server) handlePlaylistDisable(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "No playlist loaded"})
		return
	}
	pl.Disable()
	s.p.BroadcastPlaylistUpdate()
	reply(w, http.StatusOK, response{Success: true, Message: "Playlist disabled", Data: pl.Status()})
}

func (s *Server) handlePlaylistStatus(w http.ResponseWriter, r *http.Request) {
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusOK, response{Success: true, Data: map[string]any{"enabled": false, "loaded": false}})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Data: pl.Status()})
}

func (s *Server) handlePlaylistReload(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "No playlist loaded"})
		return
	}
	st := pl.Status()
	pid, _ := st["playlist_id"].(string)
	if pid == "" {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "No playlist loaded"})
		return
	}
	if err := pl.Load("https://www.youtube.com/playlist?list=" + pid); err != nil {
		reply(w, http.StatusInternalServerError, response{Success: false, Message: "Failed to reload: " + err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Playlist reloaded successfully", Data: pl.Status()})
}

func (s *Server) handlePlaylistTracks(w http.ResponseWriter, r *http.Request) {
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusOK, response{Success: true, Data: map[string]any{"tracks": []any{}}})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Data: map[string]any{
		"tracks":        pl.Tracks(),
		"current_index": pl.CurrentIndex(),
		"total":         len(pl.Tracks()),
	}})
}

func (s *Server) handlePlaylistJump(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "Invalid index parameter"})
		return
	}
	if err := s.p.PlaylistJump(idx); err != nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, response{Success: true, Message: "Jumped to track"})
}

func (s *Server) handlePlaylistShuffle(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	pl := s.p.Playlist()
	if pl == nil {
		reply(w, http.StatusBadRequest, response{Success: false, Message: "No playlist loaded"})
		return
	}
	pl.ToggleShuffle()
	s.p.BroadcastPlaylistUpdate()
	reply(w, http.StatusOK, response{Success: true, Message: "Playlist shuffle toggled", Data: pl.Status()})
}

func (s *Server) handleDonationStatus(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, response{Success: true, Data: map[string]any{"enabled": s.dm}})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.mu.Lock()
	s.hub.conns[conn] = struct{}{}
	s.hub.mu.Unlock()

	conn.WriteJSON(s.p.CurrentState())

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			s.hub.mu.Lock()
			delete(s.hub.conns, conn)
			s.hub.mu.Unlock()
			conn.Close()
			return
		}
	}
}

func (s *Server) handleStatic(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := s.staticFiles.ReadFile(name)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
		w.Write(data)
	}
}

func BroadcastLoop(p *player.Player, hub *Hub) {
	for st := range p.Updates() {
		hub.Send(st)
	}
}

func (s *Server) SetPlaylist(pl *playlist.Manager) {
	s.p.SetPlaylist(pl)
}
