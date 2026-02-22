package queue

import (
	"sync"
	"time"
)

type Track struct {
	VideoID     string    `json:"video_id"`
	Title       string    `json:"title"`
	DurationSec int       `json:"duration_sec"`
	Views       int       `json:"views"`
	AddedAt     time.Time `json:"added_at"`
	AddedBy     string    `json:"added_by,omitempty"`
	IsPaid      bool      `json:"is_paid"`
}

type RingBuffer struct {
	buf  []*Track
	head int
	size int
	cap  int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{buf: make([]*Track, capacity), cap: capacity}
}

func (r *RingBuffer) Push(t *Track) {
	r.buf[r.head] = t
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

func (r *RingBuffer) Pop() *Track {
	if r.size == 0 {
		return nil
	}
	r.size--
	idx := (r.head - 1 - r.size%r.cap + r.cap) % r.cap
	t := r.buf[idx]
	r.buf[idx] = nil
	return t
}

func (r *RingBuffer) Len() int { return r.size }

func (r *RingBuffer) Snapshot() []*Track {
	out := make([]*Track, r.size)
	start := (r.head - r.size + r.cap) % r.cap
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}

type Priority struct {
	items []*Track
	mu    sync.RWMutex
}

func (pq *Priority) Add(t *Track) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.addLocked(t, false)
}

func (pq *Priority) AddFront(t *Track) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.addLocked(t, true)
}

func (pq *Priority) addLocked(t *Track, front bool) {
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

func (pq *Priority) Next() *Track {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if len(pq.items) == 0 {
		return nil
	}
	t := pq.items[0]
	pq.items = pq.items[1:]
	return t
}

func (pq *Priority) Snapshot() []*Track {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	out := make([]*Track, len(pq.items))
	copy(out, pq.items)
	return out
}

func (pq *Priority) Len() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.items)
}

func (pq *Priority) RemoveAt(i int) *Track {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if i < 0 || i >= len(pq.items) {
		return nil
	}
	t := pq.items[i]
	pq.items = append(pq.items[:i], pq.items[i+1:]...)
	return t
}

func (pq *Priority) Clear() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.items = pq.items[:0]
}
