//go:build ignore

// Live check: runs the real engine with the 17track-web adapter and prints
// the parsed events (or the raw error/state) for a given code.
package main

import (
	"fmt"
	"os"
	"time"

	"plurideliver/internal/engine"
	"plurideliver/internal/models"
)

func main() {
	code := "RR123456789CN"
	carrier := "generic"
	if len(os.Args) > 1 {
		code = os.Args[1]
	}
	if len(os.Args) > 2 {
		carrier = os.Args[2]
	}

	e := engine.New("", "")
	fmt.Printf("registered carriers: ")
	for _, c := range e.Carriers() {
		fmt.Printf("%s ", c.ID)
	}
	fmt.Println()

	st := models.DefaultSettings()
	tr := &models.Tracking{
		ID:        models.TrackingID(carrier, code),
		Code:      code,
		Carrier:   carrier,
		CreatedAt: time.Now().UTC().Add(-time.Hour),
	}

	events, err := e.Track(tr, st)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		// try a second pass to see the queued->data transition
		fmt.Println("--- second pass after 20s...")
		time.Sleep(20 * time.Second)
		events, err = e.Track(tr, st)
		if err != nil {
			fmt.Printf("ERROR2: %v\n", err)
			return
		}
	}
	fmt.Printf("GOT %d EVENTS:\n", len(events))
	for _, ev := range events {
		fmt.Printf("  %s | %-16s | %-28s | %s\n",
			ev.Date.Format("2006-01-02 15:04"), ev.Status, ev.Location, ev.Description)
	}
}
