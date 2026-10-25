//go:build livecpt

// Live check: the v1.4.0 Correios flow end-to-end — Cainiao's keyless
// network with the user's real code, pt-BR wording, then the registry-free
// service chain (no captcha anywhere).
package main

import (
	"fmt"
	"os"

	"plurideliver/internal/cainiao"
)

func main() {
	code := "NN428571043BR"
	if len(os.Args) > 1 {
		code = os.Args[1]
	}
	lang := "pt-BR"
	if len(os.Args) > 2 {
		lang = os.Args[2]
	}
	fmt.Printf("=== Cainiao live track %s (lang %s) ===\n", code, lang)
	c := cainiao.New()
	res, err := c.Track(code, lang)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("origin=%s dest=%s lastCarrier=%s state=%s events=%d\n",
		res.Origin, res.Destination, res.LastCarrier, res.OverallState, len(res.Events))
	for i, ev := range res.Events {
		fmt.Printf("%2d. %s | %-16s | %-28s | %s\n",
			i+1, ev.Date.UTC().Format("2006-01-02 15:04"), ev.Status, ev.Location, ev.Description)
	}
}
