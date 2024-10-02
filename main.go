package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// TODO get this in from the command line
const WEBHOOK_URL string = "http://localhost:3000/webhook"

type TimerMap struct {
	sync.Mutex
	// This is the actual map that contains the list of timer objects
	TimerMap   map[string]*time.Timer
	cancelList []string
}

// Input event type
type TimeoutEvent struct {
	EventID     string `json:"event_id"`
	TimeoutMins int    `json:"timeout_mins"`
	Emit        string `json:"emit"`
}

type TimeoutResponse struct {
	Message string `json:"message"`
}

// Input cancel event struct
type CancelEvent struct {
	EventID string `json:"event_id"`
}

type CancalResponse struct {
	Message string `json:"message"`
}

func main() {
	mux := http.NewServeMux()
	state := TimerMap{}
	state.TimerMap = make(map[string]*time.Timer)
	var wg sync.WaitGroup

	// System goroutine;
	wg.Add(1)
	// TODO substitute this with the actual endpoint, see if we need to pull this
	// from an environment variable

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

		fmt.Printf("Recieved request -> ID %s TIMEOUT %d EMIT %s\n", request.EventID, request.TimeoutMins, request.Emit)

		// validate the request
		if request.TimeoutMins > 60 || request.TimeoutMins <= 0 {
			fmt.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutMins))
			// send the error message to the webhook
		}

		// Fire off the goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()

			cancelInstance := time.AfterFunc(time.Duration(request.TimeoutMins)*time.Second, func() {
				fmt.Println("Emitting Event -> ", request)
				// Make the webhook call with emit
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
