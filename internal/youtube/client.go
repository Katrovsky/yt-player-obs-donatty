package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"yt-player/internal/cache"
)

type VideoInfo struct {
	Title      string
	Duration   int
	Views      int
	Embeddable bool
}

type Client struct {
	apiKey string
	cache  *cache.Cache
}

var youtubeIDRegex = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/)([a-zA-Z0-9_-]{11})`)

func ExtractID(text string) string {
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

func NewClient(apiKey string, c *cache.Cache) *Client {
	return &Client{apiKey: apiKey, cache: c}
}

func (c *Client) APIKey() string { return c.apiKey }

func (c *Client) GetVideoInfo(vid string) (VideoInfo, error) {
	return c.GetVideoInfoWithClient(vid, &http.Client{Timeout: 20 * time.Second})
}

func (c *Client) GetVideoInfoWithClient(vid string, client *http.Client) (VideoInfo, error) {
	if e, ok := c.cache.GetVideo(vid); ok {
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
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
			ContentDetails struct {
				Duration string `json:"duration"`
			} `json:"contentDetails"`
			Statistics struct {
				ViewCount string `json:"viewCount"`
			} `json:"statistics"`
			Status struct {
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
	dur, err := parseDuration(item.ContentDetails.Duration)
	if err != nil {
		return VideoInfo{}, fmt.Errorf("failed to parse duration: %w", err)
	}
	views := 0
	if item.Statistics.ViewCount != "" {
		views, _ = strconv.Atoi(item.Statistics.ViewCount)
	}
	embeddable := item.Status.Embeddable && item.Status.PrivacyStatus == "public"

	info := VideoInfo{Title: item.Snippet.Title, Duration: dur, Views: views, Embeddable: embeddable}
	c.cache.SetVideo(vid, cache.VideoEntry{Title: info.Title, Duration: info.Duration, Views: info.Views, Embeddable: info.Embeddable})
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
