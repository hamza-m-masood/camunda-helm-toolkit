package watcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

// PostWebhook sends new findings as a JSON POST body. Kept deliberately generic
// (not Slack-specific formatting) — most webhook receivers (Slack incoming
// webhooks included, via a relay, or a generic alerting endpoint) can consume a
// plain JSON array; a Slack-specific "text:" wrapper can be layered on by the
// caller if --webhook-url is known to be a Slack incoming webhook.
func PostWebhook(url string, findings []rules.Finding) error {
	body, err := json.Marshal(map[string]interface{}{"newFindings": findings})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("posting to webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
