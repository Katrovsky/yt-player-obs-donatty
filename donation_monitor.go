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

const seenDonationsMaxSize = 500

type DonationMonitor struct {
	widgetURL     string
	minAmount     int
	widgetID      string
	widgetToken   string
	accessToken   string
	seenDonations map[string]time.Time
	mu            sync.Mutex
	backoff       time.Duration
}

type AuthResponse struct {
	Response struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpireAt     string `json:"expireAt"`
	} `json:"response"`
}
type SSEDonationData struct {
	Action string `json:"action"`
	Data   struct {
		StreamEventType string `json:"streamEventType"`
		StreamEventData string `json:"streamEventData"`
	} `json:"data"`
}
type DonationEventData struct {
	RefID       string `json:"refId"`
	Amount      int    `json:"amount"`
	DisplayName string `json:"displayName"`
	Message     string `json:"message"`
}

func NewDonationMonitor(wu string, ma int) *DonationMonitor {
	dm := &DonationMonitor{
		widgetURL:     wu,
		minAmount:     ma,
		seenDonations: make(map[string]time.Time),
		backoff:       10 * time.Second,
	}
	if err := dm.parseWidgetURL(); err != nil {
		log.Printf("Failed to parse widget URL: %v", err)
		return nil
	}
	return dm
}

func (dm *DonationMonitor) parseWidgetURL() error {
	u, err := url.Parse(dm.widgetURL)
	if err != nil {
		return err
	}
	q := u.Query()
	dm.widgetID = q.Get("ref")
	dm.widgetToken = q.Get("token")
	if dm.widgetID == "" || dm.widgetToken == "" {
		return fmt.Errorf("missing ref or token in widget URL")
	}
	return nil
}

func (dm *DonationMonitor) getAccessToken() error {
	url := fmt.Sprintf("https://api.donatty.com/auth/tokens/%s", dm.widgetToken)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get access token: %d", resp.StatusCode)
	}
	var ar AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return err
	}
	dm.accessToken = ar.Response.AccessToken
	log.Println("Donation monitor: access token obtained")
	return nil
}

func (dm *DonationMonitor) Start() {
	log.Printf("Starting donation monitor (min: %d)", dm.minAmount)
	for {
		if err := dm.getAccessToken(); err != nil {
			log.Printf("Failed to get access token: %v", err)
			time.Sleep(dm.backoff)
			dm.increaseBackoff()
			continue
		}
		if err := dm.connectSSE(); err != nil {
			log.Printf("SSE connection error: %v", err)
		}
		time.Sleep(dm.backoff)
		dm.increaseBackoff()
	}
}

func (dm *DonationMonitor) increaseBackoff() {
	if dm.backoff < 5*time.Minute {
		dm.backoff = dm.backoff * 2
		if dm.backoff > 5*time.Minute {
			dm.backoff = 5 * time.Minute
		}
	}
}

func (dm *DonationMonitor) resetBackoff() {
	dm.backoff = 10 * time.Second
}

func (dm *DonationMonitor) connectSSE() error {
	url := fmt.Sprintf("https://api.donatty.com/widgets/%s/sse?jwt=%s", dm.widgetID, dm.accessToken)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE connection failed: %d", resp.StatusCode)
	}
	log.Println("Connected to donation SSE stream")
	dm.resetBackoff()
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
		dm.processDonationEvent(strings.TrimPrefix(line, "data:"))
	}
}

func (dm *DonationMonitor) processDonationEvent(data string) {
	var ev SSEDonationData
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	if ev.Action != "DATA" || ev.Data.StreamEventType != "DONATTY_DONATION" {
		return
	}
	var dd DonationEventData
	if err := json.Unmarshal([]byte(ev.Data.StreamEventData), &dd); err != nil {
		return
	}
	log.Printf("Donation received: %s donated %d - %s", dd.DisplayName, dd.Amount, dd.Message)
	if dd.Amount < dm.minAmount {
		log.Printf("Skipping donation (%d < %d min)", dd.Amount, dm.minAmount)
		return
	}
	dm.mu.Lock()
	if _, seen := dm.seenDonations[dd.RefID]; seen {
		dm.mu.Unlock()
		log.Printf("Donation already processed: %s", dd.RefID)
		return
	}
	dm.seenDonations[dd.RefID] = time.Now()
	if len(dm.seenDonations) > seenDonationsMaxSize {
		dm.evictOldestDonation()
	}
	dm.mu.Unlock()

	vid := ExtractYouTubeID(dd.Message)
	if vid == "" {
		log.Printf("No YouTube link in donation from %s", dd.DisplayName)
		return
	}
	log.Printf("Adding donation track from %s: YouTube ID %s", dd.DisplayName, vid)
	go dm.addDonationTrack(vid, dd.DisplayName)
}

func (dm *DonationMonitor) evictOldestDonation() {
	var oldestKey string
	var oldestTime time.Time
	for k, t := range dm.seenDonations {
		if oldestKey == "" || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
		}
	}
	if oldestKey != "" {
		delete(dm.seenDonations, oldestKey)
	}
}

func (dm *DonationMonitor) addDonationTrack(vid, name string) {
	if err := validateAndAddTrack(vid, name, true); err != nil {
		log.Printf("Failed to add donation track: %v", err)
	}
}
