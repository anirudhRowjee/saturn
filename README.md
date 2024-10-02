# Kronos - a Timer Daemon

Kronos is a timer daemon built on golang's `time.AfterFunc`. It fires an event to a given webhook after a specified duration, returning user-assigned content.

## Usage 

Submit a POST request of the following format:

```
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first", "timeout_minutes": 10, "emit": "lol" }' \
  http://localhost:3000/register

```

And recieve a POST request on a specified webhook URL, after _X_ Minutes, with the following format:
```json
{"event_id": "", "emit": "", "time_initiated": ""}
```

Alternatively, you can cancel a timeout with the following  -

```
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first" }' \
  http://localhost:3000/cancel

```

## Design

Given that `time.AfterFunc` waits in its own goroutine until the time elapses, we need to give each call its own goroutine. This is mostly okay because all of these goroutines will be sleeping for the majority of their lifespan, and can thus be happily un-scheduled. 

