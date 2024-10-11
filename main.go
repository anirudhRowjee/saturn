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

	WEBHOOK_URL := flag.String("webhook_url", "http://localhost:3000/webhook", "where do you want your emitted event to go?")
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
		if time.Duration(request.TimeoutSecs)*time.Second > MaxTimeoutSeconds || request.TimeoutSecs <= 0 {
			log.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutSecs))
			extendReponseBytes, err := json.Marshal(ExtendResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("Illegal duration of %d seconds", request.TimeoutSecs),
			})

			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v\n", err))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to marshal body of request"))
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(extendReponseBytes)

			return
		}

		// Fire off the goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()

			state.Lock()

			_, ok := state.TimerMap[request.EventID]
			// this would mean that an existing event with
			// request.EventID already has a timer attached
			// with it
			if ok {
				log.Printf("Existing timer attached with event %s\n", request.EventID)
				state.Unlock()
				// w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(fmt.Sprintf("Existing timer attached with event %s", request.EventID)))
				return
			}

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
					log.Printf("Something went wrong -> %v\n", err)
				}

				state.Lock()
				delete(state.TimerMap, request.EventID)
				state.Unlock()

				// TODO better error handling and logging
				_, err = http.Post(*WEBHOOK_URL, "application/json", bytes.NewReader(response_bytes))
				if err != nil {
					log.Printf("Something went wrong -> %v\n", err)
				}
				log.Printf("Completion of event_id %s sent to WebHook URL %s", request.EventID, *WEBHOOK_URL)
			})

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

		log.Printf("Recieved cancel request -> ID %s\n", request.EventID)

		state.Lock()

		cancelTimerHandle, ok := state.TimerMap[request.EventID]
		if !ok {
			state.Unlock()

			cancelResponse, err := json.Marshal(&CancelResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
			})
			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write(cancelResponse)
			return
		}

		timerStopRequest := cancelTimerHandle.timer.Stop()
		// If a stopped timer is still existing in the map,
		// not deleted .Stop() retunrs a false boolean value
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
			delete(state.TimerMap, request.EventID)
			state.Unlock()
		}

		cancelResponseBytes, err := json.Marshal(&CancelResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Cancelled event with event_id %s", request.EventID),
		})
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
		}

		w.WriteHeader(http.StatusOK)
		w.Write(cancelResponseBytes)
	})

	mux.HandleFunc("POST /remaining", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to read body of request"))
			return
		}

		var request RemainingEvent
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to unmarshal request json"))
			return
		}

		state.Lock()

		if timerMapValue, ok := state.TimerMap[request.EventID]; !ok {
			state.Unlock()

			log.Printf("No associated timer with event_id %s\n", request.EventID)

			remainingResponse, err := json.Marshal(RemainingResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("No associated timer with event_id %s", request.EventID),
			})

			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to marshal body of request"))
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(remainingResponse)
		} else {

			diff := time.Now().Sub(timerMapValue.initTime)
			remaining := state.TimerMap[request.EventID].duration - diff

			state.Unlock()

			response := RemainingResponse{
				EventID:       request.EventID,
				TimeRemaining: remaining.String(),
				Message:       fmt.Sprintf("Remaining time for event_id %s is %s", request.EventID, remaining.String()),
			}

			responseBytes, err := json.Marshal(response)
			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to marshal body of request"))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write(responseBytes)
		}
	})

	// NOTE:
	// elimintate a 3 round trips for
	// remaining -> cancel -> register
	mux.HandleFunc("POST /extend", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to read extend event request body"))
			return
		}

		var request ExtendEvent

		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to unmarshal extend event request json"))
			return
		}

		log.Printf("Recieved extend event ID %s with extended timeout %d\n", request.EventID, request.TimeoutSecs)

		state.Lock()

		// !ok for condition where no previous timer has
		// not been set
		if cancelTimerHandle, ok := state.TimerMap[request.EventID]; !ok {
			state.Unlock()

			extendResponse, err := json.Marshal(&ExtendResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
			})

			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to marshal body of request"))
				return
			}

			w.WriteHeader(http.StatusBadRequest)
			w.Write(extendResponse)
			return
		} else {

			diff := time.Now().Sub(cancelTimerHandle.initTime)
			remaining := state.TimerMap[request.EventID].duration - diff
			extraDuration := time.Duration(request.TimeoutSecs) * time.Second

			extendedDuration := remaining + extraDuration

			if extendedDuration > MaxTimeoutSeconds || request.TimeoutSecs <= 0 {
				log.Println(fmt.Errorf("Duration of %d is illegal!", request.TimeoutSecs))
				extendReponseBytes, err := json.Marshal(ExtendResponse{
					EventID: request.EventID,
					Message: fmt.Sprintf("Illegal duration of %d seconds", request.TimeoutSecs),
				})

				if err != nil {
					log.Println(fmt.Errorf("Something broke -> %v\n", err))
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("Failed to marshal body of request"))
					return
				}

				w.WriteHeader(http.StatusBadRequest)
				w.Write(extendReponseBytes)

				return
			}

			timeInitiated := time.Now()
			cancelTimerHandle.timer.Reset(extendedDuration)
			cancelTimerHandle.initTime = timeInitiated
			cancelTimerHandle.duration = extendedDuration

			// updating the state TimerMap
			state.TimerMap[request.EventID] = cancelTimerHandle

			state.Unlock()

			extendEventResponse, err := json.Marshal(&ExtendResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("Extended timer for event_id %s with duration %s", request.EventID, extendedDuration.String()),
			})
			if err != nil {
				log.Println(fmt.Errorf("Something broke -> %v", err))
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to marshal body of request"))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write(extendEventResponse)
		}
	})

	// placeholder for the webhook
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {

		// parse all the arguments
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to marshal body of request"))
			return
		}

		var request TimeoutMessage
		err = json.Unmarshal(body, &request)
		if err != nil {
			log.Println(fmt.Errorf("Something broke -> %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to marshal body of request"))
			return
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
			log.Println(fmt.Errorf("Error in server -> %v", err))
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
