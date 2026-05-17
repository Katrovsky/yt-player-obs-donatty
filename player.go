package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type PlayerState struct {
	Action            string             `json:"action"`
	Current           *Track             `json:"current,omitempty"`
	Queue             []*Track           `json:"queue,omitempty"`
	Position          int                `json:"position"`
	Playlist          PlaylistStatus     `json:"playlist"`
	OverlayMode       string             `json:"overlay_mode,omitempty"`
	PendingModeration []*PendingDonation `json:"pending_moderation,omitempty"`
}

type PlaylistStatus struct {
	Loaded       bool   `json:"loaded"`
	Enabled      bool   `json:"enabled"`
	Shuffled     bool   `json:"shuffled"`
	PlaylistID   string `json:"playlist_id"`
	TotalTracks  int    `json:"total_tracks"`
	CurrentIndex int    `json:"current_index"`
}

type Player struct {
	mu      sync.Mutex
	q       Queue
	cfg     *ConfigManager
	yt      *YouTubeClient
	pl      *Playlist
	state   string
	updates chan PlayerState
}

func newPlayer(cfg *ConfigManager, yt *YouTubeClient) *Player {
	return &Player{
		state:   "stopped",
		cfg:     cfg,
		yt:      yt,
		updates: make(chan PlayerState, 50),
	}
}

func (p *Player) setPlaylist(pl *Playlist) {
	p.mu.Lock()
	p.pl = pl
	p.mu.Unlock()
}

func (p *Player) getPlaylist() *Playlist {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pl
}

func (p *Player) broadcastPlaylistUpdate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.broadcast()
}

func (p *Player) validateAndAdd(vid, by string, paid bool) error {
	info, err := p.yt.getVideoInfo(vid)
	if err != nil {
		return err
	}
	if !info.Embeddable {
		return fmt.Errorf("video is not available for playback")
	}
	cfg := p.cfg.get()
	t := &Track{
		VideoID:     vid,
		Title:       info.Title,
		DurationSec: info.Duration,
		Views:       info.Views,
		AddedAt:     time.Now(),
		AddedBy:     by,
		IsPaid:      paid,
	}
	if cfg.MaxDurationMinutes > 0 && t.DurationSec > cfg.MaxDurationMinutes*60 {
		return fmt.Errorf("track too long (max %d minutes)", cfg.MaxDurationMinutes)
	}
	if cfg.MinViews > 0 && t.Views < cfg.MinViews {
		return fmt.Errorf("insufficient views (min %d)", cfg.MinViews)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.canRepeat(vid) {
		return fmt.Errorf("track recently played (repeat limit reached)")
	}
	if cfg.MaxQueueSize > 0 && p.q.total() >= cfg.MaxQueueSize {
		return fmt.Errorf("queue is full (max %d tracks)", cfg.MaxQueueSize)
	}
	hadNoCurrent := p.q.current() == nil
	p.q.add(t)
	log.Printf("Added: %s by %s (paid=%v)", t.Title, by, paid)
	if p.state == "stopped" && hadNoCurrent {
		p.state = "playing"
	}
	p.broadcast()
	return nil
}

func (p *Player) play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.q.current() != nil {
		p.state = "playing"
		p.broadcast()
		return nil
	}
	if p.pl != nil && p.pl.isEnabledVal() {
		if t := p.pl.getNext(); t != nil {
			p.q.items = append(p.q.items, t)
			p.q.cursor = len(p.q.items) - 1
			p.state = "playing"
			p.broadcast()
			return nil
		}
	}
	return fmt.Errorf("queue is empty")
}

func (p *Player) pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != "paused" {
		p.state = "paused"
		log.Println("Paused")
		p.broadcast()
	}
}

func (p *Player) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != "stopped" {
		p.state = "stopped"
		if p.pl != nil {
			p.pl.disable()
		}
		log.Println("Stopped")
		p.broadcast()
	}
}

func (p *Player) next() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.q.advance(); t != nil {
		p.state = "playing"
		log.Printf("Next: %s", t.Title)
		p.broadcast()
		return
	}
	if p.pl != nil && p.pl.isEnabledVal() {
		if t := p.pl.getNext(); t != nil {
			p.q.items = append(p.q.items, t)
			p.q.cursor = len(p.q.items) - 1
			p.state = "playing"
			log.Printf("Next (playlist): %s", t.Title)
			p.broadcast()
			return
		}
	}
	p.q.cursor = len(p.q.items)
	p.state = "stopped"
	log.Println("Queue finished")
	p.broadcast()
}

func (p *Player) previous() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.q.goBack()
	if t == nil {
		return fmt.Errorf("no previous track available")
	}
	p.state = "playing"
	log.Printf("Previous: %s", t.Title)
	p.broadcast()
	return nil
}

func (p *Player) playlistJump(trackIdx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pl == nil {
		return fmt.Errorf("no playlist loaded")
	}
	t := p.pl.jumpTo(trackIdx)
	if t == nil {
		return fmt.Errorf("index out of range")
	}
	pos := min(p.q.cursor+1, len(p.q.items))
	p.q.items = append(p.q.items[:pos], append([]*Track{t}, p.q.items[pos:]...)...)
	p.q.cursor = pos
	p.state = "playing"
	log.Printf("Playlist jump: %s", t.Title)
	p.broadcast()
	return nil
}

func (p *Player) remove(idx int) (*Track, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.q.removeAt(idx)
	if t == nil {
		return nil, fmt.Errorf("index out of range")
	}
	log.Printf("Removed: %s", t.Title)
	p.broadcast()
	return t, nil
}

func (p *Player) clear() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.q.clear()
	p.state = "stopped"
	log.Printf("Queue cleared (%d tracks)", n)
	p.broadcast()
	return n
}

func (p *Player) clearHistory() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.q.resetCursor()
	if n > 0 {
		log.Printf("Cleared %d played tracks", n)
		p.broadcast()
	}
	return n
}

func (p *Player) cleanupOld(hours int) {
	if hours == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	cur := p.q.current()
	keep := make([]*Track, 0, len(p.q.items))
	newCursor := len(p.q.items)
	for _, t := range p.q.items {
		if t.AddedAt.After(cutoff) {
			if t == cur {
				newCursor = len(keep)
			}
			keep = append(keep, t)
		}
	}
	removed := len(p.q.items) - len(keep)
	if removed == 0 {
		return
	}
	p.q.items = keep
	if newCursor > len(keep) {
		newCursor = len(keep)
	}
	p.q.cursor = newCursor
	if p.q.current() == nil {
		p.state = "stopped"
	}
	log.Printf("Cleanup: removed %d old tracks", removed)
	p.broadcast()
}

func (p *Player) currentState() PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buildState()
}

func (p *Player) status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"state":        p.state,
		"current":      p.q.current(),
		"position":     p.q.cursor + 1,
		"queue_length": p.q.total(),
	}
}

func (p *Player) nowPlaying() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp := map[string]any{"status": p.state, "artist": "", "title": "", "url": ""}
	cur := p.q.current()
	if cur == nil {
		return resp
	}
	full := cur.Title
	art, tit := "", full
	if before, after, ok := strings.Cut(full, " - "); ok {
		art = before
		tit = after
	}
	resp["artist"] = art
	resp["title"] = tit
	resp["full_title"] = full
	resp["url"] = fmt.Sprintf("https://www.youtube.com/watch?v=%s", cur.VideoID)
	return resp
}

func (p *Player) approveTrack(vid, by string) error {
	info, err := p.yt.getVideoInfoForce(vid)
	if err != nil {
		return err
	}
	if !info.Embeddable {
		return fmt.Errorf("video is not available for playback")
	}
	t := &Track{
		VideoID:     vid,
		Title:       info.Title,
		DurationSec: info.Duration,
		Views:       info.Views,
		AddedAt:     time.Now(),
		AddedBy:     by,
		IsPaid:      true,
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	hadNoCurrent := p.q.current() == nil
	p.q.add(t)
	log.Printf("Approved donation track: %s by %s", t.Title, by)
	if p.state == "stopped" && hadNoCurrent {
		p.state = "playing"
	}
	p.broadcast()
	return nil
}

func (p *Player) canRepeat(id string) bool {
	limit := p.cfg.get().RepeatLimit
	if limit == 0 {
		return true
	}
	cnt := 0
	for i := p.q.cursor - 1; i >= 0 && cnt < limit; i-- {
		if p.q.items[i].VideoID == id {
			cnt++
		}
	}
	return cnt < limit
}

func (p *Player) broadcast() {
	st := p.buildState()
	select {
	case p.updates <- st:
	default:
	}
}

func (p *Player) buildState() PlayerState {
	var plState PlaylistStatus
	if p.pl != nil {
		plState = PlaylistStatus{
			Loaded:       p.pl.loaded(),
			Enabled:      p.pl.isEnabledVal(),
			Shuffled:     p.pl.isShuffledVal(),
			PlaylistID:   p.pl.getPlaylistID(),
			TotalTracks:  p.pl.lenVal(),
			CurrentIndex: p.pl.activeTrackIndex(),
		}
	}
	return PlayerState{
		Action:   p.state,
		Current:  p.q.current(),
		Queue:    p.q.snapshot(),
		Position: p.q.cursor,
		Playlist: plState,
	}
}
