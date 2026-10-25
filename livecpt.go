//go:build ignore

// Live check for the Correios direct client: fetches a captcha image to
// /tmp/go_captcha.png, polls /tmp/captcha_answer.txt for the typed answer,
// queries resultado.php and prints the parsed events.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"plurideliver/internal/correios"
)

func main() {
	code := "NN428571043BR"
	if len(os.Args) > 1 {
		code = os.Args[1]
	}

	c := correios.New()
	img, err := c.CaptchaImage()
	if err != nil {
		fmt.Println("CAPTCHA ERROR:", err)
		os.Exit(1)
	}
	_ = os.WriteFile("/tmp/go_captcha.png", img, 0644)
	fmt.Println("captcha image saved to /tmp/go_captcha.png — waiting for /tmp/captcha_answer.txt")

	var answer string
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile("/tmp/captcha_answer.txt"); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				answer = s
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if answer == "" {
		fmt.Println("no answer arrived in time")
		os.Exit(1)
	}
	fmt.Println("answer:", answer)

	res, err := c.Track(code, answer)
	if err != nil {
		fmt.Println("TRACK ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("CODE %s | category %s | ETA %s\n", res.Code, res.Category, res.ETA)
	for _, ev := range res.Events {
		fmt.Printf("  %s | %-16s | %-34s | %s\n",
			ev.Date.Local().Format("2006-01-02 15:04"), ev.Status, ev.Location, ev.Description)
	}
}
