package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type TimerMap struct {
	sync.Mutex
	// This is the actual map that contains the list of timer objects
	TimerMap   map[string]*time.Timer
	cancelList []string
}

// Input event type
type TimeoutEvent struct {
	EventID     string `json:"event_id"`
	TimeoutSecs int    `json:"timeout_seconds"`
	Emit        string `json:"emit"`
}

// This is the response that's sent to the webhook
type TimeoutMessage struct {
	EventID       string `json:"event_id"`
	Message       string `json:"message"`
	TimeInitiated string `json:"time_initiated"`
}

// Input cancel event struct
type CancelEvent struct {
	EventID string `json:"event_id"`
}

type CancelResponse struct {
	Message string `json:"message"`
}

func main() {

	WEBHOOK_URL := flag.String("webhook_url", "http://localhost:3000/test", "where do you want your emitted event to go?")
	flag.Parse()

	fmt.Printf("Sending events on webhook URL %s\n", *WEBHOOK_URL)

	mux := http.NewServeMux()
	state := TimerMap{}
	state.TimerMap = make(map[string]*time.Timer)
	var wg sync.WaitGroup

	// System goroutine;
	wg.Add(1)

	// Handle when a user tries to register a timeout
	// Spawn the goroutine
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		// Parse JSON
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request TimeoutEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}

		fmt.Printf("Recieved request -> ID %s TIMEOUT %d EMIT %s\n", request.EventID, request.TimeoutSecs, request.Emit)

		// validate the request
		if request.TimeoutSecs > 60*60 || request.TimeoutSecs <= 0 {
			fmt.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutSecs))
			// send the error message to the webhook
		}

		// Fire off the goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeInitiated := time.Now()

			cancelInstance := time.AfterFunc(time.Duration(request.TimeoutSecs)*time.Second, func() {
				fmt.Println("Emitting Event -> ", request)
				// Make the webhook call with emit
				response := TimeoutMessage{
					EventID:       request.EventID,
					Message:       request.Emit,
					TimeInitiated: timeInitiated.String(),
				}
				response_bytes, err := json.Marshal(response)
				if err != nil {
					fmt.Println(fmt.Errorf("Something went wrong -> %v", err))
				}
				// TODO better error handling and logging
				http.Post(*WEBHOOK_URL, "application/json", bytes.NewReader(response_bytes))
			})

			// Add metadata
			state.Lock()
			state.TimerMap[request.EventID] = cancelInstance
			state.Unlock()
		}()

	})

	mux.HandleFunc("POST /cancel", func(w http.ResponseWriter, r *http.Request) {

		// parse all the arguments
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request CancelEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}

		fmt.Printf("Recieved cancel request -> ID %s \n", request.EventID)

		state.Lock()
		state.TimerMap[request.EventID].Stop()
		state.Unlock()
	})

	// placeholder for the webhook
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {

		// parse all the arguments
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request TimeoutMessage
		err = json.Unmarshal(body, &request)
		if err != nil {
			fmt.Println(fmt.Errorf("Something broke -> %v", err))
		}

		fmt.Printf("Recieved webhook request -> ID %s Message -> %s\n", request.EventID, request.Message)

	})

	var systemwg sync.WaitGroup
	systemwg.Add(2)

	// Spawn server goroutine
	go func() {
		fmt.Println("Starting server ...")
		err := http.ListenAndServe(":3000", mux)
		if err != nil {
			fmt.Println(fmt.Errorf("Error in server -> %v", err))
			defer systemwg.Done()
		}
	}()

	// Spawn timer goroutine
	go func() {
		defer systemwg.Done()
		wg.Wait()
	}()

	systemwg.Wait()
}
