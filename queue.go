package main

import "time"

type Track struct {
	VideoID     string    `json:"video_id"`
	Title       string    `json:"title"`
	DurationSec int       `json:"duration_sec"`
	Views       int       `json:"views"`
	AddedAt     time.Time `json:"added_at"`
	AddedBy     string    `json:"added_by,omitempty"`
	IsPaid      bool      `json:"is_paid"`
}

type Queue struct {
	items  []*Track
	cursor int
}

func (q *Queue) current() *Track {
	if q.cursor < 0 || q.cursor >= len(q.items) {
		return nil
	}
	return q.items[q.cursor]
}

func (q *Queue) add(t *Track) {
	if t.IsPaid {
		pos := min(q.cursor+1, len(q.items))
		for pos < len(q.items) && q.items[pos].IsPaid {
			pos++
		}
		q.items = append(q.items[:pos], append([]*Track{t}, q.items[pos:]...)...)
	} else {
		q.items = append(q.items, t)
	}
}

func (q *Queue) advance() *Track {
	if q.cursor+1 >= len(q.items) {
		return nil
	}
	q.cursor++
	return q.items[q.cursor]
}

func (q *Queue) goBack() *Track {
	if q.cursor <= 0 {
		return nil
	}
	q.cursor--
	return q.items[q.cursor]
}

func (q *Queue) removeAt(relIdx int) *Track {
	abs := q.cursor + 1 + relIdx
	if abs >= len(q.items) {
		return nil
	}
	t := q.items[abs]
	q.items = append(q.items[:abs], q.items[abs+1:]...)
	return t
}

func (q *Queue) resetCursor() int {
	n := q.cursor
	if n > 0 {
		q.items = q.items[q.cursor:]
		q.cursor = 0
	}
	return n
}

func (q *Queue) clear() int {
	n := len(q.items)
	q.items = q.items[:0]
	q.cursor = 0
	return n
}

func (q *Queue) total() int       { return len(q.items) }
func (q *Queue) hasCurrent() bool { return q.cursor >= 0 && q.cursor < len(q.items) }

func (q *Queue) snapshot() []*Track {
	out := make([]*Track, len(q.items))
	copy(out, q.items)
	return out
}
