//go:build ignore

// Live check: the v1.7.0 keyless direct adapters through the real engine —
// Amazon (track.amazon.com/api/tracker), DHL (utapi), UPS (XSRF handshake)
// and Loggi (graphql). Run with:
//
//	go run livecarriers.go
//
// From datacenter IPs DHL/UPS/Loggi are expected to be refused by their edges
// (and the chain should then fall back to the 17TRACK web engine or report a
// clean error); from residential IPs they answer like the website. Amazon's
// public tracker answers from anywhere.
package main

import (
	"fmt"
	"os"

	"plurideliver/internal/engine"
	"plurideliver/internal/models"
	"plurideliver/internal/store"
)

func main() {
	dir, _ := os.MkdirTemp("", "live")
	st, _ := store.NewAt(dir)
	e := engine.New("", "")
	settings := st.Settings()

	cases := []struct {
		carrier, code string
	}{
		{"correios", "SS123456789BR"},
		{"amazon", "TBA123456789000"},
	}

	for _, tc := range cases {
		fmt.Printf("=== %s %s ===\n", tc.carrier, tc.code)
		tr := &models.Tracking{Code: tc.code, Carrier: tc.carrier}
		events, err := e.Track(tr, settings)
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		fmt.Printf("  %d events\n", len(events))
	}
	os.Exit(0)
}
