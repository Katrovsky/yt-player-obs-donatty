package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"time"

	"yt-player/internal/api"
	"yt-player/internal/config"
	"yt-player/internal/donation"
	"yt-player/internal/player"
	"yt-player/internal/playlist"
	"yt-player/internal/youtube"
)

//go:embed dashboard.html overlay.html dock.html
var staticFiles embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}
	go cfg.Watch()

	c := cfg.Get()
	yt := youtube.NewClient(c.YouTubeAPIKey)
	p := player.New(cfg, yt)
	hub := api.NewHub()

	var pl *playlist.Manager
	if c.FallbackPlaylistURL != "" {
		pl = playlist.New(yt)
		if err := pl.Load(c.FallbackPlaylistURL); err != nil {
			log.Printf("Failed to load fallback playlist: %v", err)
			pl = nil
		} else {
			pl.Enable()
		}
	}
	if pl == nil {
		pl = playlist.New(yt)
	}
	p.SetPlaylist(pl)

	donationEnabled := false
	if c.DonationWidgetURL != "" {
		mon, err := donation.New(c.DonationWidgetURL, c.DonationMinAmount, p.ValidateAndAdd)
		if err != nil {
			log.Printf("Failed to init donation monitor: %v", err)
		} else {
			donationEnabled = true
			go mon.Start()
		}
	}

	go api.BroadcastLoop(p, hub)
	go cleanupLoop(p, cfg)

	srv := api.NewServer(p, hub, yt, donationEnabled, staticFiles)
	mux := http.NewServeMux()
	srv.Register(mux)

	addr := fmt.Sprintf(":%d", c.Port)
	log.Printf("Server started on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func cleanupLoop(p *player.Player, cfg *config.Manager) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		p.CleanupOld(cfg.Get().CleanupAfterHours)
	}
}
