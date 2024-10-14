# Saturn - a Timer Daemon

Saturn is an in-memory timer daemon built on golang's `time.AfterFunc`. It fires an event to a given webhook after a specified duration, returning user-assigned content.

## Usage 

```sh
# if you have a pre-defined webhook URL:
$ go run main.go --webhook_url="http://<YOUR API ENDPOINT HERE>"
# if you don't, and just want to see how it works
$ go run main.go 
```

Submit a POST request of the following format:

```sh
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first", "timeout_seconds": 10, "emit": "lol" }' \
  http://localhost:3000/register
```

And recieve a POST request on a specified webhook URL, after 10 seconds, with the following format:
```json
{"event_id": "first", "message": "lol", "time_initiated": ""}
```

Alternatively, you can cancel a timeout with the following  -

```sh
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first" }' \
  http://localhost:3000/cancel

```

You can request for the remaining time with the following request format:

```sh 
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first" }' \
  http://localhost:3000/remaining
```

You can extend the timer for an event with the format:

```sh
curl --header "Content-Type: application/json" \
  --request POST \
  --data '{ "event_id": "first", "timeout_seconds": 10, "emit": "lol" }' \
  http://localhost:3000/extend
```

Once the timer is finished, we recieve a POST request in the similar format as mentioned before

```

## Design

Given that `time.AfterFunc` waits in its own goroutine until the time elapses, we need to give each call its own goroutine. This is mostly okay because all of these goroutines will be sleeping for the majority of their lifespan, and can thus be happily un-scheduled. 

