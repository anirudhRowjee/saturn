package main

import (
	"sync"
	"time"
)

// global structure defintions

// Holding more contextual information
// associated with a timer, specifically
// for extentions in timer
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
	Message string `json:"message"`
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
	Message       string `json:"message"`
}

type ExtendEvent struct {
	EventID     string `json:"event_id"`
	TimeoutSecs int    `json:"timeout_seconds"`
}

type ExtendResponse struct {
	EventID string `json:"event_id"`
	Message string `json:"message"`
}

// global constants
const MaxTimeoutSeconds = time.Duration(60 * 120) * time.Second
