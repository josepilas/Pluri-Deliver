//go:build livee2e

package main

import (
	"fmt"
	"os"

	"plurideliver/internal/jt"
	"plurideliver/internal/spx"
)

// Run: go run -tags livee2e ./livee2e.go
// Validates the request formats against the real endpoints (fake numbers ->
// expected "not found" semantics, not auth/format errors).
func main() {
	fmt.Println("=== SPX order endpoint (unknown number expected) ===")
	c := spx.New()
	res, err := c.Track("SPXBR1234567890")
	fmt.Printf("res=%+v err=%v\n", res, err)

	fmt.Println("=== J&T endpoint (fake CPF + unknown waybill expected) ===")
	j := jt.New()
	res2, err2 := j.Track("JT1234567890", "11144477735")
	fmt.Printf("res=%+v err=%v\n", res2, err2)
	os.Exit(0)
}
