package main

import (
	"sync"
	"time"
)

const (
	pendingMaxSize = 50
	pendingTTL     = 24 * time.Hour
)

type PendingDonation struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Amount      int       `json:"amount"`
	VideoID     string    `json:"video_id"`
	VideoTitle  string    `json:"video_title"`
	Reason      string    `json:"reason"`
	ReceivedAt  time.Time `json:"received_at"`
}

type ModerationQueue struct {
	mu      sync.Mutex
	items   []*PendingDonation
	onChange func()
}

func newModerationQueue(onChange func()) *ModerationQueue {
	return &ModerationQueue{onChange: onChange}
}

func (m *ModerationQueue) add(d *PendingDonation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictExpiredLocked()
	if len(m.items) >= pendingMaxSize {
		m.items = m.items[1:]
	}
	m.items = append(m.items, d)
	if m.onChange != nil {
		go m.onChange()
	}
}

func (m *ModerationQueue) get(id string) (*PendingDonation, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, item := range m.items {
		if item.ID == id {
			return item, i
		}
	}
	return nil, -1
}

func (m *ModerationQueue) remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, item := range m.items {
		if item.ID == id {
			m.items = append(m.items[:i], m.items[i+1:]...)
			return true
		}
	}
	return false
}

func (m *ModerationQueue) list() []*PendingDonation {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictExpiredLocked()
	out := make([]*PendingDonation, len(m.items))
	copy(out, m.items)
	return out
}

func (m *ModerationQueue) evictExpiredLocked() {
	cutoff := time.Now().Add(-pendingTTL)
	i := 0
	for _, item := range m.items {
		if item.ReceivedAt.After(cutoff) {
			m.items[i] = item
			i++
		}
	}
	m.items = m.items[:i]
}
