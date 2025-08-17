package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Timer struct {
	State *TimerMap
	SysWg sync.WaitGroup

	Logger zerolog.Logger

	WebhookUrl string
}

func CreateTimer(WebhookUrl string, logDir string) Timer {
	state := TimerMap{}
	state.TimerMap = make(map[string]TimerMapValue)

	consoleLogger := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	err := os.MkdirAll(logDir, 0777)
	if err != nil {
		panic(err)
	}

	logRoot, err := os.OpenRoot(logDir)
	if err != nil {
		panic("Could not open log directory")
	}

	logFile := fmt.Sprintf("log-%d.json", time.Now().Unix())
	jsonLogFile, err := logRoot.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0777)
	if err != nil {
		panic("Could not open file")
	}

	multiLogger := zerolog.MultiLevelWriter(consoleLogger, jsonLogFile)

	return Timer{
		State:      &state,
		SysWg:      sync.WaitGroup{},
		WebhookUrl: WebhookUrl,

		Logger: zerolog.New(multiLogger).With().Timestamp().Caller().Logger(),
	}
}

func (t *Timer) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Something broke")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Could not read request body"))
		return
	}

	var request RegisterEvent
	err = json.Unmarshal(body, &request)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Request data not to format")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Request data not to format"))
		return
	}

	t.Logger.Info().Str("event_id", request.EventID).Int("timeout_secs", request.TimeoutSecs).
		Str("emit_msg", request.Emit).Msg("Received register request")

	if time.Duration(request.TimeoutSecs)*time.Second > MaxTimeout || request.TimeoutSecs <= 0 {
		t.Logger.Warn().Msg(fmt.Sprintf("Duration of %d is illegal!", request.TimeoutSecs))
		extendResponseBytes, err := json.Marshal(TimeoutResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Illegal duration of %d seconds", request.TimeoutSecs),
		})

		if err != nil {
			t.Logger.Error().Err(err).Msg("Failed to marshal response")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to marshal response"))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(extendResponseBytes)
		return
	}

	// Fire off the goroutine

	t.State.Lock()

	_, ok := t.State.TimerMap[request.EventID]
	// this would mean that an existing event with
	// request.EventID already has a timer attached
	// with it
	if ok {
		t.Logger.Warn().Str("event_id", request.EventID).Msg("Existing timer attached with event")
		t.State.Unlock()
		// BUG:
		registerResponseBytes, _ := json.Marshal(TimeoutResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Existing timer attached with event %s", request.EventID),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(registerResponseBytes)
		return
	}

	timeInitiated := time.Now()

	cancelInstance := time.AfterFunc(time.Duration(request.TimeoutSecs)*time.Second, func() {
		t.State.Lock()
		defer t.State.Unlock()

		t.Logger.Info().Str("event_id", request.EventID).Int("timeout_secs", request.TimeoutSecs).
			Str("emit_msg", request.Emit).Str("webhook_url", t.WebhookUrl).
			Msg("Timer event firing to webhook")

		// Make the webhook call with emit
		response := TimeoutMessage{
			EventID:       request.EventID,
			Message:       request.Emit,
			TimeInitiated: timeInitiated.String(),
		}
		response_bytes, err := json.Marshal(response)
		if err != nil {
			t.Logger.Error().Err(err).Send()
		}

		delete(t.State.TimerMap, request.EventID)

		// TODO better error handling and logging
		_, err = http.Post(t.WebhookUrl, "application/json", bytes.NewReader(response_bytes))
		if err != nil {
			t.Logger.Error().Err(err).Msg("Could not send response to webhook")
		}
		t.Logger.Info().Str("event_id", request.EventID).Str("webhook_url", t.WebhookUrl).
			Msg("Timer completed, sent to webhook")
	})

	t.State.TimerMap[request.EventID] = TimerMapValue{
		timer:    cancelInstance,
		duration: time.Duration(request.TimeoutSecs) * time.Second,
		initTime: timeInitiated,
	}
	t.State.Unlock()

	registerResponseBytes, _ := json.Marshal(TimeoutResponse{
		EventID: request.EventID,
		Message: fmt.Sprintf("Created event with event ID %s", request.EventID),
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(registerResponseBytes)
}

func (t *Timer) CancelHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Something broke")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Could not read request body"))
		return
	}
	var request CancelEvent
	err = json.Unmarshal(body, &request)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Request data not to format")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Request data not to format"))
		return
	}

	t.Logger.Info().Str("event_id", request.EventID).Msg("Received cancel request")

	t.State.Lock()

	cancelTimerHandle, ok := t.State.TimerMap[request.EventID]
	if !ok {
		t.State.Unlock()

		t.Logger.Error().Str("event_id", request.EventID).Msg("No event has been registered")

		cancelResponse, err := json.Marshal(&CancelResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
		})
		if err != nil {
			t.Logger.Error().Err(err).Send()
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(cancelResponse)
		return
	}

	timerStopRequest := cancelTimerHandle.timer.Stop()
	// If a stopped timer is still existing in the map,
	// not deleted .Stop() retunrs a false boolean value
	if !timerStopRequest {
		t.State.Unlock()
		// NOTE: Failed to get an event with EventID
		t.Logger.Warn().Str("event_id", request.EventID).Msg("Event has already been stopped")

		cancelResponse, err := json.Marshal(&CancelResponse{
			Message: fmt.Sprintf("Event with event_id %s has already been stopped", request.EventID),
		})
		if err != nil {
			t.Logger.Error().Err(err).Send()
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(cancelResponse)
		return
	} else {
		delete(t.State.TimerMap, request.EventID)
		t.State.Unlock()
	}

	t.Logger.Info().Str("event_id", request.EventID).Msg("Sucessfully cancelled event")
	cancelResponseBytes, err := json.Marshal(&CancelResponse{
		EventID: request.EventID,
		Message: fmt.Sprintf("Cancelled event with event_id %s", request.EventID),
	})
	if err != nil {
		t.Logger.Error().Err(err).Send()
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cancelResponseBytes)
}

func (t *Timer) RemainingHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logger.Error().Err(err).Send()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Failed to read body of request"))
		return
	}

	var request RemainingEvent
	err = json.Unmarshal(body, &request)
	if err != nil {
		t.Logger.Error().Err(err).Send()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Failed to unmarshal request json"))
		return
	}

	t.Logger.Info().Str("event_id", request.EventID).Msg("Received remaining request")

	t.State.Lock()

	if timerMapValue, ok := t.State.TimerMap[request.EventID]; !ok {
		t.State.Unlock()

		t.Logger.Error().Str("event_id", request.EventID).Msg("No associated timer found")

		remainingResponse, err := json.Marshal(RemainingResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("No associated timer with event_id %s", request.EventID),
		})

		if err != nil {
			t.Logger.Error().Err(err).Send()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to marshal response"))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(remainingResponse)
	} else {
		// diff := time.Now().Sub(timerMapValue.initTime)
		diff := time.Since(timerMapValue.initTime)
		remaining := t.State.TimerMap[request.EventID].duration - diff

		t.State.Unlock()

		response := RemainingResponse{
			EventID:       request.EventID,
			TimeRemaining: remaining.String(),
			Message: fmt.Sprintf("Remaining time for event_id %s is %s",
				request.EventID,
				remaining.String(),
			),
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Logger.Error().Err(err).Send()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to marshal response"))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBytes)
	}
}

func (t *Timer) ExtendHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Something broke")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Could not read request body"))
		return
	}

	var request ExtendEvent

	err = json.Unmarshal(body, &request)
	if err != nil {
		t.Logger.Error().Err(err).Msg("Request data not to format")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Request data not to format"))
		return
	}

	t.Logger.Info().Str("event_id", request.EventID).Int("timeout_secs", request.TimeoutSecs).
		Msg("Received extend request")

	t.State.Lock()

	// !ok for condition where no previous timer has
	// not been set
	if cancelTimerHandle, ok := t.State.TimerMap[request.EventID]; !ok {
		t.State.Unlock()

		t.Logger.Error().Str("event_id", request.EventID).Msg("No evetn has been registered")
		extendResponse, err := json.Marshal(&ExtendResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("No event with event_id %s has been registered", request.EventID),
		})

		if err != nil {
			t.Logger.Error().Err(err).Send()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to marshal response"))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(extendResponse)
		return
	} else {
		diff := time.Since(cancelTimerHandle.initTime)
		remaining := t.State.TimerMap[request.EventID].duration - diff
		extraDuration := time.Duration(request.TimeoutSecs) * time.Second

		extendedDuration := remaining + extraDuration

		if extendedDuration > MaxTimeout || request.TimeoutSecs <= 0 {
			t.State.Unlock()
			t.Logger.Error().Int("duration", request.TimeoutSecs).Msg("Illegal Timeout Duration")
			extendResponseBytes, err := json.Marshal(ExtendResponse{
				EventID: request.EventID,
				Message: fmt.Sprintf("Illegal duration of %d seconds", request.TimeoutSecs),
			})

			if err != nil {
				t.Logger.Error().Err(err)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Failed to marshal response"))
				return
			}

			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(extendResponseBytes)

			return
		}

		timeInitiated := time.Now()
		cancelTimerHandle.timer.Reset(extendedDuration)
		cancelTimerHandle.initTime = timeInitiated
		cancelTimerHandle.duration = extendedDuration

		// updating the state TimerMap
		t.State.TimerMap[request.EventID] = cancelTimerHandle

		t.State.Unlock()

		extendEventResponse, err := json.Marshal(&ExtendResponse{
			EventID: request.EventID,
			Message: fmt.Sprintf("Extended timer for event_id %s with duration %s",
				request.EventID,
				extendedDuration.String(),
			),
		})

		if err != nil {
			t.Logger.Error().Err(err).Send()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to marshal response"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(extendEventResponse)
	}
}

func (t *Timer) WebhookTest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logger.Error().Err(err).Send()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Failed to read body of request"))
		return
	}

	var request TimeoutMessage
	err = json.Unmarshal(body, &request)
	if err != nil {
		t.Logger.Error().Err(err).Send()
		return
	}

	t.Logger.Info().Str("event_id", request.EventID).Str("message", request.Message).
		Msg("Received webhook request")
}
