package main

import (
	"context"
	"log"
	"time"

	tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := tszclient.New(tszclient.Config{
		BaseURL: "http://localhost:8080",
	})
	if err != nil {
		log.Fatalf("failed to create tsz client: %v", err)
	}

	resp, err := client.Detect(ctx, tszclient.DetectRequest{
		Text:       "Contact me at john@example.com",
		RID:        "RID-EXAMPLE-DETECT-001",
		Guardrails: []string{"TOXIC_LANGUAGE"},
	})
	if err != nil {
		log.Fatalf("detect failed: %v", err)
	}

	if resp.Blocked {
		log.Printf("Request blocked by TSZ: %s", resp.Message)
		return
	}

	log.Printf("Redacted text: %s", resp.RedactedText)
	log.Printf("Detected types: %+v", resp.Breakdown)
}
