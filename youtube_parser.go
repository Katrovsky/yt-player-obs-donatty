package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type YouTubeVideoInfo struct {
	Title    string
	Duration int
	Views    int
}

type YouTubeAPIResponse struct {
	Items []struct {
		Snippet struct {
			Title string `json:"title"`
		} `json:"snippet"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
		Statistics struct {
			ViewCount string `json:"viewCount"`
		} `json:"statistics"`
	} `json:"items"`
}

var youtubeRegex = []*regexp.Regexp{
	regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/)([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`\b([a-zA-Z0-9_-]{11})\b`),
}

func ExtractYouTubeID(text string) string {
	for _, re := range youtubeRegex {
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

func GetYouTubeVideoInfo(vid string) (*YouTubeVideoInfo, error) {
	ytMu.RLock()
	if cached, ok := ytCache[vid]; ok {
		ytMu.RUnlock()
		return cached, nil
	}
	ytMu.RUnlock()
	if conf.YouTubeAPIKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?part=snippet,contentDetails,statistics&id=%s&key=%s", vid, conf.YouTubeAPIKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube API returned status: %d", resp.StatusCode)
	}
	var apiResp YouTubeAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	if len(apiResp.Items) == 0 {
		return nil, fmt.Errorf("video not found")
	}
	item := apiResp.Items[0]
	dur, err := parseDuration(item.ContentDetails.Duration)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration: %w", err)
	}
	views := 0
	if item.Statistics.ViewCount != "" {
		views, _ = strconv.Atoi(item.Statistics.ViewCount)
	}
	info := &YouTubeVideoInfo{Title: item.Snippet.Title, Duration: dur, Views: views}
	ytMu.Lock()
	if len(ytCache) >= 100 {
		for k := range ytCache {
			delete(ytCache, k)
			break
		}
	}
	ytCache[vid] = info
	ytMu.Unlock()
	return info, nil
}

func parseDuration(iso string) (int, error) {
	re := regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
	matches := re.FindStringSubmatch(iso)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid duration format")
	}
	h, m, s := 0, 0, 0
	if matches[1] != "" {
		h, _ = strconv.Atoi(matches[1])
	}
	if matches[2] != "" {
		m, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		s, _ = strconv.Atoi(matches[3])
	}
	return h*3600 + m*60 + s, nil
}
