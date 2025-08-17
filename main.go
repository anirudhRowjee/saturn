package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"net/http"
	"sync"
	"time"
)

func main() {

	WEBHOOK_URL := flag.String("webhook_url", "http://localhost:3000/webhook", "where do you want your emitted event to go?")
	LOG_DIR_JSON := flag.String("log_dir", "./logs/", "directory for logger")
	flag.Parse()

	mux := http.NewServeMux()
	timer := CreateTimer(*WEBHOOK_URL, *LOG_DIR_JSON)

	timer.Logger.Info().Msg(fmt.Sprintf("Sending events on webhook URL %s", *WEBHOOK_URL))

	// System goroutine;
	timer.SysWg.Add(1)

	// Handle when a user tries to register a timeout
	// Spawn the goroutine
	mux.HandleFunc("POST /register", timer.RegisterHandler)
	mux.HandleFunc("POST /cancel", timer.CancelHandler)
	mux.HandleFunc("POST /remaining", timer.RemainingHandler)
	mux.HandleFunc("POST /extend", timer.ExtendHandler)

	mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		// log.Println("Received GET /test")
		timer.Logger.Info().Msg("Received GET /test")
		response := struct {
			Time int64  `json:"time"`
			Data string `json:"data"`
		}{
			Time: time.Now().Unix(),
			Data: fmt.Sprintf("saturn is up: %d timer(s) running", len(timer.State.TimerMap)),
		}

		responseBytes, _ := json.Marshal(response)
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBytes)
	})

	// placeholder for the webhook
	mux.HandleFunc("POST /webhook", timer.WebhookTest)

	var systemwg sync.WaitGroup
	systemwg.Add(2)

	// Spawn server goroutine
	go func() {
		timer.Logger.Info().Msg("Starting Server...")
		err := http.ListenAndServe(":3000", mux)
		if err != nil {
			// log.Println(fmt.Errorf("Error in server -> %v", err))
			timer.Logger.Error().Msg(fmt.Sprintf("Error in server: %v", err))
			defer systemwg.Done()
		}
	}()

	// Spawn timer goroutine
	go func() {
		defer systemwg.Done()
		timer.SysWg.Wait()
	}()

	systemwg.Wait()
}
