package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {

	WEBHOOK_URL := flag.String("webhook_url", "http://localhost:3000/test", "where do you want your emitted event to go?")
	flag.Parse()

	log.Printf("Sending events on webhook URL %s\n", *WEBHOOK_URL)

	mux := http.NewServeMux()
	state := TimerMap{}
	state.TimerMap = make(map[string]TimerMapValue)
	var wg sync.WaitGroup

	// System goroutine;
	wg.Add(1)

	// Handle when a user tries to register a timeout
	// Spawn the goroutine
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		// Parse JSON
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request TimeoutEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}

		log.Printf("Recieved request -> ID %s TIMEOUT %d EMIT %s\n", request.EventID, request.TimeoutSecs, request.Emit)

		// validate the request
		if request.TimeoutSecs > MaxTimeoutSeconds || request.TimeoutSecs <= 0 {
			log.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutSecs))
			// send the error message to the webhook
		}

		// Fire off the goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeInitiated := time.Now()

			cancelInstance := time.AfterFunc(time.Duration(request.TimeoutSecs)*time.Second, func() {
				log.Println("Emitting Event -> ", request)
				// Make the webhook call with emit
				response := TimeoutMessage{
					EventID:       request.EventID,
					Message:       request.Emit,
					TimeInitiated: timeInitiated.String(),
				}
				response_bytes, err := json.Marshal(response)
				if err != nil {
					log.Println(fmt.Errorf("Something went wrong -> %v", err))
				}
				// TODO better error handling and logging
				http.Post(*WEBHOOK_URL, "application/json", bytes.NewReader(response_bytes))
			})

			// Add metadata
			state.Lock()
			state.TimerMap[request.EventID] = TimerMapValue{
				timer:    cancelInstance,
				duration: time.Duration(request.TimeoutSecs) * time.Second,
				initTime: timeInitiated,
			}
			state.Unlock()
		}()

	})

	mux.HandleFunc("POST /cancel", func(w http.ResponseWriter, r *http.Request) {

		// parse all the arguments
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request CancelEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}

		log.Printf("Recieved cancel request -> ID %s \n", request.EventID)

		state.Lock()

		cancelTimerHandle, ok := state.TimerMap[request.EventID]
		if !ok {
			state.Unlock()
			cancelResposne, err := json.Marshal(&CancelResponse{
				Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
			})
			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write(cancelResposne)
			return
		}

		timerStopRequest := cancelTimerHandle.timer.Stop()
		if !timerStopRequest {
			state.Unlock()

			// NOTE:
			// Failed to get an event with EventID
			cancelResposne, err := json.Marshal(&CancelResponse{
				Message: fmt.Sprintf("Event with event_id %s has already been stopped", request.EventID),
			})
			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write(cancelResposne)

			return
		} else {
			state.TimerMap[request.EventID].timer.Stop()
			// NOTE: Pointless to set to TimerMapValaue{}
			state.Unlock()
		}

		cancelResponseBytes, err := json.Marshal(&CancelResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Cancelled event with event_id %s", request.EventID),
		})

		w.WriteHeader(http.StatusOK)
		w.Write(cancelResponseBytes)
	})

	mux.HandleFunc("POST /remaining", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("%v", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var request RemainingEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		state.Lock()

		timerMapValue := state.TimerMap[request.EventID]
		diff := time.Now().Sub(timerMapValue.initTime)
		remaining := state.TimerMap[request.EventID].duration - diff

		state.Unlock()

		response := RemainingResponse{
			EventID:       request.EventID,
			TimeRemaining: remaining.String(),
		}

		response_bytes, err := json.Marshal(response)
		if err != nil {
			log.Println(fmt.Errorf("Something went wrong -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(response_bytes)
	})

	// NOTE:
	// elimintate a 3 round trips for
	// remaining -> cancel -> register
	mux.HandleFunc("POST /extend", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("%v", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var request ExtendEvent

		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Printf("Recieved extend event | ID %s TIMEOUT %d EMIT %s\n", request.EventID, request.TimeoutSecs, request.Emit)

		if request.TimeoutSecs > MaxTimeoutSeconds || request.TimeoutSecs <= 0 {
			log.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutSecs))
			return
		}

		// get a lock on the state's TimerMap
		state.Lock()

		cancelTimerHandle, ok := state.TimerMap[request.EventID]
		if !ok {
			state.Unlock()

			extendResponse, err := json.Marshal(&ExtendResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
			})

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(extendResponse)
			return
		}

		diff := time.Now().Sub(cancelTimerHandle.initTime)
		remaining := state.TimerMap[request.EventID].duration - diff
		extraDuration := time.Duration(request.TimeoutSecs) * time.Second

		extendedTime := remaining + extraDuration

		// stop the current Timer that will be fired with AfterFunc
		// create a new cancelInstance in place of the one already
		// present in timer map
		cancelTimerHandle.timer.Stop()
		timeInitiated := time.Now()

		state.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Extended duration to %q for event_id %s", extendedTime, request.EventID)

			cancelInstance := time.AfterFunc(extendedTime, func() {
				log.Println("Emitting Event -> ", request)
				responseBytes, err := json.Marshal(&TimeoutMessage{
					EventID:       request.EventID,
					Message:       request.Emit,
					TimeInitiated: timeInitiated.String(),
				})

				if err != nil {
					log.Println(fmt.Errorf("Something went wrong -> %v", err))
				}

				http.Post(*WEBHOOK_URL, "application/json", bytes.NewReader(responseBytes))
			})

			state.Lock()
			state.TimerMap[request.EventID] = TimerMapValue{
				timer:    cancelInstance,
				duration: extendedTime,
				initTime: timeInitiated,
			}
			log.Printf("Updated State EVENT %s EXTENDED TIME %q",
				request.EventID,
				state.TimerMap[request.EventID].duration,
			)
			state.Unlock()
		}()

		extendEvetResponse, err := json.Marshal(&ExtendResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Extended timer for event_id %s", request.EventID),
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(extendEvetResponse)

	})

	// placeholder for the webhook
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {

		// parse all the arguments
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}
		var request TimeoutMessage
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}

		log.Printf("Recieved webhook request -> ID %s Message -> %s\n", request.EventID, request.Message)

	})

	var systemwg sync.WaitGroup
	systemwg.Add(2)

	// Spawn server goroutine
	go func() {
		log.Println("Starting server ...")
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
