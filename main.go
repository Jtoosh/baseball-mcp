package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	path := flag.String("config", "config.json", "administrator JSON configuration")
	flag.Parse()
	config, err := LoadConfig(*path)
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("Set OPENAI_API_KEY on the server")
	}
	if err = os.MkdirAll(filepath.Dir(config.DeveloperLog), 0700); err != nil {
		log.Fatal(err)
	}
	file, err := os.OpenFile(config.DeveloperLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	db, err := OpenDatabase(config.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	service := NewQueryService(db, NewOpenAIModel(config.Model), config, log.New(file, "", log.LstdFlags))
	server := http.Server{Addr: config.Listen, Handler: NewHandler(service), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second}
	log.Printf("Listening at http://%s", config.Listen)
	log.Fatal(server.ListenAndServe())
}
