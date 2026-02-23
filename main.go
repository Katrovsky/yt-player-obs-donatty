package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"time"
)

//go:embed dashboard.html overlay.html dock.html
var staticFiles embed.FS

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}
	go cfg.watch()

	c := cfg.get()

	db, err := openCache("cache.db", 7*24*time.Hour)
	if err != nil {
		log.Fatal("Failed to open cache:", err)
	}
	defer db.close()

	yt := newYouTubeClient(c.YouTubeAPIKey, db)
	p := newPlayer(cfg, yt)
	hub := newHub()

	pl := newPlaylist(yt, db)
	p.setPlaylist(pl)
	if c.FallbackPlaylistURL != "" {
		go func() {
			if err := pl.load(c.FallbackPlaylistURL); err != nil {
				log.Printf("Failed to load fallback playlist: %v", err)
				return
			}
			pl.enable()
			log.Println("Fallback playlist ready")
		}()
	}

	if c.DonationWidgetURL != "" {
		go func() {
			mon, err := newDonationMonitor(c.DonationWidgetURL, c.DonationMinAmount, p.validateAndAdd)
			if err != nil {
				log.Printf("Failed to init donation monitor: %v", err)
				return
			}
			mon.start()
		}()
	}

	go broadcastLoop(p, hub)
	go cleanupLoop(p, cfg)

	srv := newServer(p, hub, yt, c.DonationWidgetURL != "", staticFiles)
	mux := http.NewServeMux()
	srv.register(mux)

	addr := fmt.Sprintf(":%d", c.Port)
	log.Printf("Server started on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func cleanupLoop(p *Player, cfg *ConfigManager) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		p.cleanupOld(cfg.get().CleanupAfterHours)
	}
}
