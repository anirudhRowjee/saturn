package main

import (
	"sync"
	"time"
)

// global structure defintions

type TimerMapValue struct {
	timer    *time.Timer
	duration time.Duration
	initTime time.Time
}

type TimerMap struct {
	sync.Mutex
	// This is the actual map that contains the list of timer objects
	TimerMap   map[string]TimerMapValue
	cancelList []string
}

// Input event type
type TimeoutEvent struct {
	EventID     string `json:"event_id"`
	TimeoutSecs int    `json:"timeout_seconds"`
	Emit        string `json:"emit"`
}

// Input event type
type TimeoutResponse struct {
	EventID string `json:"event_id"`
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
	EventID string `json:"event_id"`
	Message string `json:"message"`
}

type RemainingEvent struct {
	EventID string `json:"event_id"`
}

type RemainingResponse struct {
	EventID       string `json:"event_id"`
	TimeRemaining string `json:"time_remaining"`
}

type ExtendEvent struct {
	EventID     string `json:"event_id"`
	TimeoutSecs int    `json:"timeout_seconds"`
	Emit        string `json:"emit"`
	// Preserve the original emit event
}

type ExtendResponse struct {
	EventID string `json:"event_id"`
	Message string `json:"message"`
}

// global constants
const MaxTimeoutSeconds = 60 * 120
