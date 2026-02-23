package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const historySize = 100

type PlayerState struct {
	Action   string         `json:"action"`
	Current  *Track         `json:"current,omitempty"`
	Queue    []*Track       `json:"queue,omitempty"`
	Position int            `json:"position"`
	Playlist PlaylistStatus `json:"playlist"`
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
	q       PriorityQueue
	hist    *RingBuffer
	cur     *Track
	state   string
	cfg     *ConfigManager
	yt      *YouTubeClient
	pl      *Playlist
	updates chan PlayerState
}

func newPlayer(cfg *ConfigManager, yt *YouTubeClient) *Player {
	return &Player{
		hist:    newRingBuffer(historySize),
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
		VideoID: vid, Title: info.Title,
		DurationSec: info.Duration, Views: info.Views,
		AddedAt: time.Now(), AddedBy: by, IsPaid: paid,
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
	if cfg.MaxQueueSize > 0 {
		total := p.q.len()
		if p.cur != nil {
			total++
		}
		if total >= cfg.MaxQueueSize {
			return fmt.Errorf("queue is full (max %d tracks)", cfg.MaxQueueSize)
		}
	}
	wasEmpty := p.q.len() == 0 && p.cur == nil
	p.q.add(t, false)
	log.Printf("Added: %s by %s (paid=%v)", t.Title, by, paid)
	if p.state == "stopped" && wasEmpty {
		p.playNext()
	}
	p.broadcast()
	return nil
}

func (p *Player) play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.q.len() == 0 && p.cur == nil {
		if p.pl != nil && p.pl.isEnabledVal() {
			p.playNext()
			p.broadcast()
			return nil
		}
		return fmt.Errorf("queue is empty")
	}
	if p.cur == nil {
		p.playNext()
	} else {
		p.state = "playing"
	}
	log.Println("Playing")
	p.broadcast()
	return nil
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
	if p.cur != nil {
		p.hist.push(p.cur)
		if p.cur.AddedBy == "Playlist" && p.pl != nil {
			p.pl.advanceToNext()
		}
	}
	p.playNext()
	p.broadcast()
}

func (p *Player) previous() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hist.len() == 0 {
		return fmt.Errorf("no previous track available")
	}
	if p.cur != nil {
		if p.cur.AddedBy == "Playlist" && p.pl != nil {
			p.pl.goToPrevious()
		} else {
			p.q.add(p.cur, true)
		}
	}
	prev := p.hist.pop()
	if prev.AddedBy == "Playlist" && p.pl != nil {
		p.pl.goToPrevious()
	}
	p.cur = prev
	p.state = "playing"
	log.Printf("Previous track: %s", p.cur.Title)
	p.broadcast()
	return nil
}

func (p *Player) playlistJump(idx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pl == nil {
		return fmt.Errorf("no playlist loaded")
	}
	t := p.pl.getAt(idx)
	if t == nil {
		return fmt.Errorf("index out of range")
	}
	_ = p.pl.jumpToIndex(idx + 1)
	if p.cur != nil {
		p.hist.push(p.cur)
	}
	p.cur = t
	p.state = "playing"
	log.Printf("Playlist jump to: %s", t.Title)
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
	sz := p.q.len()
	if p.cur != nil {
		sz++
	}
	p.q.clear()
	p.cur = nil
	p.state = "stopped"
	log.Printf("Queue cleared (%d tracks)", sz)
	p.broadcast()
	return sz
}

func (p *Player) cleanupOld(hours int) {
	if hours == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	items := p.q.snapshot()
	var keep []*Track
	for _, t := range items {
		if t.AddedAt.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if removed := len(items) - len(keep); removed > 0 {
		p.q.clear()
		for _, t := range keep {
			p.q.add(t, false)
		}
		if p.q.len() == 0 && p.cur == nil {
			p.state = "stopped"
		}
		log.Printf("Cleanup: removed %d old tracks", removed)
		p.broadcast()
	}
}

func (p *Player) currentState() PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buildState()
}

func (p *Player) status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := p.q.len()
	if p.cur != nil {
		total++
	}
	return map[string]any{
		"state":        p.state,
		"current":      p.cur,
		"position":     p.hist.len() + 1,
		"queue_length": total,
	}
}

func (p *Player) nowPlaying() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp := map[string]any{"status": p.state, "artist": "", "title": "", "url": ""}
	if p.cur == nil {
		return resp
	}
	full := p.cur.Title
	art, tit := "", full
	if i := strings.Index(full, " - "); i >= 0 {
		art = full[:i]
		tit = full[i+3:]
	}
	resp["artist"] = art
	resp["title"] = tit
	resp["full_title"] = full
	resp["url"] = fmt.Sprintf("https://www.youtube.com/watch?v=%s", p.cur.VideoID)
	return resp
}

func (p *Player) playNext() {
	if t := p.q.next(); t != nil {
		p.cur = t
		p.state = "playing"
		log.Printf("Next track: %s", p.cur.Title)
		return
	}
	if p.pl != nil && p.pl.isEnabledVal() {
		if t := p.pl.getNext(); t != nil {
			p.cur = t
			p.state = "playing"
			log.Printf("Next track (playlist): %s", p.cur.Title)
			return
		}
	}
	p.cur = nil
	p.state = "stopped"
	log.Println("Queue finished")
}

func (p *Player) canRepeat(id string) bool {
	limit := p.cfg.get().RepeatLimit
	if limit == 0 {
		return true
	}
	hist := p.hist.snapshot()
	cnt := 0
	for i := len(hist) - 1; i >= 0 && cnt < limit; i-- {
		if hist[i].VideoID == id {
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
	hist := p.hist.snapshot()
	items := p.q.snapshot()
	all := make([]*Track, 0, len(hist)+1+len(items))
	all = append(all, hist...)
	pos := len(hist)
	if p.cur != nil {
		all = append(all, p.cur)
	}
	all = append(all, items...)
	var plState PlaylistStatus
	if p.pl != nil {
		plState = PlaylistStatus{
			Loaded:       p.pl.loaded(),
			Enabled:      p.pl.isEnabledVal(),
			Shuffled:     p.pl.isShuffledVal(),
			PlaylistID:   p.pl.getPlaylistID(),
			TotalTracks:  p.pl.lenVal(),
			CurrentIndex: p.pl.currentIndexVal(),
		}
	}
	return PlayerState{Action: p.state, Current: p.cur, Queue: all, Position: pos, Playlist: plState}
}
