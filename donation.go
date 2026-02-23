package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxSeenDonations = 500

type DonationMonitor struct {
	widgetURL     string
	minAmount     int
	widgetID      string
	widgetToken   string
	accessToken   string
	seenDonations map[string]time.Time
	mu            sync.Mutex
	backoff       time.Duration
	addTrack      func(vid, by string, paid bool) error
}

type donationAuthResponse struct {
	Response struct {
		AccessToken string `json:"accessToken"`
	} `json:"response"`
}

type donationSSEEvent struct {
	Action string `json:"action"`
	Data   struct {
		StreamEventType string `json:"streamEventType"`
		StreamEventData string `json:"streamEventData"`
	} `json:"data"`
}

type donationData struct {
	RefID       string `json:"refId"`
	Amount      int    `json:"amount"`
	DisplayName string `json:"displayName"`
	Message     string `json:"message"`
}

func newDonationMonitor(widgetURL string, minAmount int, addTrack func(vid, by string, paid bool) error) (*DonationMonitor, error) {
	m := &DonationMonitor{
		widgetURL:     widgetURL,
		minAmount:     minAmount,
		seenDonations: make(map[string]time.Time),
		backoff:       10 * time.Second,
		addTrack:      addTrack,
	}
	u, err := url.Parse(widgetURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	m.widgetID = q.Get("ref")
	m.widgetToken = q.Get("token")
	if m.widgetID == "" || m.widgetToken == "" {
		return nil, fmt.Errorf("missing ref or token in widget URL")
	}
	return m, nil
}

func (m *DonationMonitor) start() {
	log.Printf("Starting donation monitor (min: %d)", m.minAmount)
	for {
		if err := m.getAccessToken(); err != nil {
			log.Printf("Failed to get access token: %v", err)
			time.Sleep(m.backoff)
			m.increaseBackoff()
			continue
		}
		if err := m.connectSSE(); err != nil {
			log.Printf("SSE connection error: %v", err)
		}
		time.Sleep(m.backoff)
		m.increaseBackoff()
	}
}

func (m *DonationMonitor) getAccessToken() error {
	resp, err := http.Get(fmt.Sprintf("https://api.donatty.com/auth/tokens/%s", m.widgetToken))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get access token: %d", resp.StatusCode)
	}
	var ar donationAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return err
	}
	m.accessToken = ar.Response.AccessToken
	log.Println("Donation monitor: access token obtained")
	return nil
}

func (m *DonationMonitor) connectSSE() error {
	u := fmt.Sprintf("https://api.donatty.com/widgets/%s/sse?jwt=%s", m.widgetID, m.accessToken)
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE connection failed: %d", resp.StatusCode)
	}
	log.Println("Connected to donation SSE stream")
	m.backoff = 10 * time.Second
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("SSE stream closed")
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		m.processEvent(strings.TrimPrefix(line, "data:"))
	}
}

func (m *DonationMonitor) processEvent(data string) {
	var ev donationSSEEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	if ev.Action != "DATA" || ev.Data.StreamEventType != "DONATTY_DONATION" {
		return
	}
	var dd donationData
	if err := json.Unmarshal([]byte(ev.Data.StreamEventData), &dd); err != nil {
		return
	}
	log.Printf("Donation received: %s donated %d - %s", dd.DisplayName, dd.Amount, dd.Message)
	if dd.Amount < m.minAmount {
		log.Printf("Skipping donation (%d < %d min)", dd.Amount, m.minAmount)
		return
	}
	m.mu.Lock()
	if _, seen := m.seenDonations[dd.RefID]; seen {
		m.mu.Unlock()
		log.Printf("Donation already processed: %s", dd.RefID)
		return
	}
	m.seenDonations[dd.RefID] = time.Now()
	if len(m.seenDonations) > maxSeenDonations {
		m.evictOldest()
	}
	m.mu.Unlock()
	vid := extractVideoID(dd.Message)
	if vid == "" {
		log.Printf("No YouTube link in donation from %s", dd.DisplayName)
		return
	}
	log.Printf("Adding donation track from %s: %s", dd.DisplayName, vid)
	go func() {
		if err := m.addTrack(vid, dd.DisplayName, true); err != nil {
			log.Printf("Failed to add donation track: %v", err)
		}
	}()
}

func (m *DonationMonitor) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, t := range m.seenDonations {
		if oldestKey == "" || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
		}
	}
	if oldestKey != "" {
		delete(m.seenDonations, oldestKey)
	}
}

func (m *DonationMonitor) increaseBackoff() {
	if m.backoff < 5*time.Minute {
		m.backoff *= 2
		if m.backoff > 5*time.Minute {
			m.backoff = 5 * time.Minute
		}
	}
}
