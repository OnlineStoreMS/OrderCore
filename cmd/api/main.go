package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"ordercore/internal/config"
	"ordercore/internal/database"
	"ordercore/internal/router"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "config file path")
	flag.Parse()

	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(absConfig)
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.Connect(&cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		log.Fatal(err)
	}
	log.Printf("database connected: driver=%s", cfg.Database.Driver)

	engine := router.Setup(db, cfg)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("OrderCore API listening on http://localhost%s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatal(err)
	}
}
