package player

import (
	"fmt"
	"log"
	"sync"
	"time"

	"yt-player/internal/config"
	"yt-player/internal/playlist"
	"yt-player/internal/queue"
	"yt-player/internal/youtube"
)

const historySize = 100

type PlaylistState struct {
	Loaded       bool   `json:"loaded"`
	Enabled      bool   `json:"enabled"`
	Shuffled     bool   `json:"shuffled"`
	PlaylistID   string `json:"playlist_id"`
	TotalTracks  int    `json:"total_tracks"`
	CurrentIndex int    `json:"current_index"`
}

type State struct {
	Action   string         `json:"action"`
	Current  *queue.Track   `json:"current,omitempty"`
	Queue    []*queue.Track `json:"queue,omitempty"`
	Position int            `json:"position"`
	Playlist PlaylistState  `json:"playlist"`
}

type Player struct {
	mu      sync.Mutex
	q       queue.Priority
	hist    *queue.RingBuffer
	cur     *queue.Track
	state   string
	cfg     *config.Manager
	yt      *youtube.Client
	pl      *playlist.Manager
	updates chan State
}

func New(cfg *config.Manager, yt *youtube.Client) *Player {
	return &Player{
		hist:    queue.NewRingBuffer(historySize),
		state:   "stopped",
		cfg:     cfg,
		yt:      yt,
		updates: make(chan State, 50),
	}
}

func (p *Player) Updates() <-chan State { return p.updates }

func (p *Player) SetPlaylist(pl *playlist.Manager) {
	p.mu.Lock()
	p.pl = pl
	p.mu.Unlock()
}

func (p *Player) Playlist() *playlist.Manager {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pl
}

func (p *Player) BroadcastPlaylistUpdate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.broadcast()
}

func (p *Player) ValidateAndAdd(vid, by string, paid bool) error {
	info, err := p.yt.GetVideoInfo(vid)
	if err != nil {
		return err
	}

	cfg := p.cfg.Get()
	t := &queue.Track{
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

	total := p.q.Len()
	if p.cur != nil {
		total++
	}
	if total >= cfg.MaxQueueSize {
		return fmt.Errorf("queue is full (max %d tracks)", cfg.MaxQueueSize)
	}

	wasEmpty := total == 0
	p.q.Add(t)
	log.Printf("Added: %s by %s (paid=%v)", t.Title, by, paid)

	if p.state == "stopped" && wasEmpty {
		p.playNext()
	}
	p.broadcast()
	return nil
}

func (p *Player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.q.Len() == 0 && p.cur == nil {
		if p.pl != nil && p.pl.IsEnabled() {
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

func (p *Player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != "paused" {
		p.state = "paused"
		log.Println("Paused")
		p.broadcast()
	}
}

func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != "stopped" {
		p.state = "stopped"
		if p.pl != nil {
			p.pl.Disable()
		}
		log.Println("Stopped")
		p.broadcast()
	}
}

func (p *Player) Next() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil {
		p.hist.Push(p.cur)
		if p.cur.AddedBy == "Playlist" && p.pl != nil {
			p.pl.AdvanceToNext()
		}
	}
	p.playNext()
	p.broadcast()
}

func (p *Player) Previous() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hist.Len() == 0 {
		return fmt.Errorf("no previous track available")
	}
	if p.cur != nil {
		if p.cur.AddedBy == "Playlist" && p.pl != nil {
			p.pl.GoToPrevious()
		} else {
			p.q.AddFront(p.cur)
		}
	}
	prev := p.hist.Pop()
	if prev.AddedBy == "Playlist" && p.pl != nil {
		p.pl.GoToPrevious()
	}
	p.cur = prev
	p.state = "playing"
	log.Printf("Previous track: %s", p.cur.Title)
	p.broadcast()
	return nil
}

func (p *Player) PlaylistJump(idx int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pl == nil {
		return fmt.Errorf("no playlist loaded")
	}
	t := p.pl.GetAt(idx)
	if t == nil {
		return fmt.Errorf("index out of range")
	}
	if err := p.pl.JumpToIndex(idx + 1); err != nil {
		// best effort, non-fatal
	}
	if p.cur != nil {
		p.hist.Push(p.cur)
	}
	p.cur = t
	p.state = "playing"
	log.Printf("Playlist jump to: %s", t.Title)
	p.broadcast()
	return nil
}

func (p *Player) Remove(idx int) (*queue.Track, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.q.RemoveAt(idx)
	if t == nil {
		return nil, fmt.Errorf("index out of range")
	}
	log.Printf("Removed: %s", t.Title)
	p.broadcast()
	return t, nil
}

func (p *Player) Clear() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	sz := p.q.Len()
	if p.cur != nil {
		sz++
	}
	p.q.Clear()
	p.cur = nil
	p.state = "stopped"
	log.Printf("Queue cleared (%d tracks)", sz)
	p.broadcast()
	return sz
}

func (p *Player) CleanupOld(hours int) {
	if hours == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	items := p.q.Snapshot()
	var keep []*queue.Track
	for _, t := range items {
		if t.AddedAt.After(cutoff) {
			keep = append(keep, t)
		}
	}
	removed := len(items) - len(keep)
	if removed == 0 {
		return
	}
	p.q.Clear()
	for _, t := range keep {
		p.q.Add(t)
	}
	if p.q.Len() == 0 && p.cur == nil {
		p.state = "stopped"
	}
	log.Printf("Cleanup: removed %d old tracks", removed)
	p.broadcast()
}

func (p *Player) CurrentState() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buildState()
}

func (p *Player) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := p.q.Len()
	if p.cur != nil {
		total++
	}
	return map[string]any{
		"state":        p.state,
		"current":      p.cur,
		"position":     p.hist.Len() + 1,
		"queue_length": total,
	}
}

func (p *Player) NowPlaying() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp := map[string]any{"status": p.state, "artist": "", "title": "", "url": ""}
	if p.cur == nil {
		return resp
	}
	full := p.cur.Title
	art, tit := "", full
	for i := 0; i < len(full)-2; i++ {
		if full[i] == ' ' && full[i+1] == '-' && full[i+2] == ' ' {
			art = full[:i]
			tit = full[i+3:]
			break
		}
	}
	resp["artist"] = art
	resp["title"] = tit
	resp["full_title"] = full
	resp["url"] = fmt.Sprintf("https://www.youtube.com/watch?v=%s", p.cur.VideoID)
	return resp
}

func (p *Player) playNext() {
	if t := p.q.Next(); t != nil {
		p.cur = t
		p.state = "playing"
		log.Printf("Next track: %s", p.cur.Title)
		return
	}
	if p.pl != nil && p.pl.IsEnabled() {
		if t := p.pl.GetNext(); t != nil {
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
	limit := p.cfg.Get().RepeatLimit
	if limit == 0 {
		return true
	}
	hist := p.hist.Snapshot()
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

func (p *Player) buildState() State {
	hist := p.hist.Snapshot()
	items := p.q.Snapshot()
	all := make([]*queue.Track, 0, len(hist)+1+len(items))
	all = append(all, hist...)
	pos := len(hist)
	if p.cur != nil {
		all = append(all, p.cur)
	}
	all = append(all, items...)

	var plState PlaylistState
	if p.pl != nil {
		plState = PlaylistState{
			Loaded:       p.pl.Loaded(),
			Enabled:      p.pl.IsEnabled(),
			Shuffled:     p.pl.IsShuffled(),
			PlaylistID:   p.pl.PlaylistID(),
			TotalTracks:  p.pl.Len(),
			CurrentIndex: p.pl.CurrentIndex(),
		}
	}

	return State{Action: p.state, Current: p.cur, Queue: all, Position: pos, Playlist: plState}
}
