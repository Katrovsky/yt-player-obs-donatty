package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type VideoInfo struct {
	Title      string
	Duration   int
	Views      int
	Embeddable bool
}

type YouTubeClient struct {
	apiKey string
	cache  *Cache
}

var youtubeIDRegex = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/)([a-zA-Z0-9_-]{11})`)

func extractVideoID(text string) string {
	if matches := youtubeIDRegex.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1]
	}
	if len(text) == 11 {
		matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]{11}$`, text)
		if matched {
			return text
		}
	}
	return ""
}

func newYouTubeClient(apiKey string, c *Cache) *YouTubeClient {
	return &YouTubeClient{apiKey: apiKey, cache: c}
}

func (c *YouTubeClient) getVideoInfo(vid string) (VideoInfo, error) {
	return c.getVideoInfoWithClient(vid, &http.Client{Timeout: 20 * time.Second})
}

func (c *YouTubeClient) getVideoInfoWithClient(vid string, client *http.Client) (VideoInfo, error) {
	if e, ok := c.cache.getVideo(vid); ok {
		return VideoInfo{Title: e.Title, Duration: e.Duration, Views: e.Views, Embeddable: e.Embeddable}, nil
	}
	if c.apiKey == "" {
		return VideoInfo{}, fmt.Errorf("YouTube API key not configured")
	}
	url := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/videos?part=snippet,contentDetails,statistics,status&id=%s&key=%s",
		vid, c.apiKey,
	)
	resp, err := client.Get(url)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("failed to fetch video info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return VideoInfo{}, fmt.Errorf("youtube API returned status: %d", resp.StatusCode)
	}
	var apiResp struct {
		Items []struct {
			Snippet        struct{ Title string `json:"title"` } `json:"snippet"`
			ContentDetails struct{ Duration string `json:"duration"` } `json:"contentDetails"`
			Statistics     struct{ ViewCount string `json:"viewCount"` } `json:"statistics"`
			Status         struct {
				Embeddable    bool   `json:"embeddable"`
				PrivacyStatus string `json:"privacyStatus"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return VideoInfo{}, fmt.Errorf("failed to parse API response: %w", err)
	}
	if len(apiResp.Items) == 0 {
		return VideoInfo{}, fmt.Errorf("video not found")
	}
	item := apiResp.Items[0]
	dur, err := parseISO8601Duration(item.ContentDetails.Duration)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("failed to parse duration: %w", err)
	}
	views := 0
	if item.Statistics.ViewCount != "" {
		views, _ = strconv.Atoi(item.Statistics.ViewCount)
	}
	info := VideoInfo{
		Title:      item.Snippet.Title,
		Duration:   dur,
		Views:      views,
		Embeddable: item.Status.Embeddable && item.Status.PrivacyStatus == "public",
	}
	c.cache.setVideo(vid, VideoEntry{Title: info.Title, Duration: info.Duration, Views: info.Views, Embeddable: info.Embeddable})
	return info, nil
}

func parseISO8601Duration(iso string) (int, error) {
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
