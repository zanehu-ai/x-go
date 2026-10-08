package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zanehu-ai/x-go/mailer"
	"github.com/zanehu-ai/x-go/timeutil"
)

// Message represents a notification to be sent.
type Message struct {
	To      string // recipient (email address, phone number, webhook URL, etc.)
	Subject string // subject line (used by email, ignored by webhook/SMS)
	Body    string // message body (plain text)
}

// Notifier sends notifications through a specific channel.
type Notifier interface {
	Send(ctx context.Context, msg Message) error
}

// emailNotifier sends notifications via email.
type emailNotifier struct {
	mailer mailer.Mailer
}

// NewEmail creates a Notifier that sends via the given Mailer.
func NewEmail(m mailer.Mailer) Notifier {
	return &emailNotifier{mailer: m}
}

// Send sends an email notification.
func (n *emailNotifier) Send(_ context.Context, msg Message) error {
	return n.mailer.Send(msg.To, msg.Subject, msg.Body)
}

// NewFeishu creates a Notifier that sends messages to a Feishu/Lark webhook bot.
func NewFeishu(webhookURL string) Notifier {
	return &webhookNotifier{
		url:    webhookURL,
		client: &http.Client{Timeout: 10 * time.Second},
		bodyFn: func(msg Message) ([]byte, error) {
			return json.Marshal(map[string]any{
				"msg_type": "text",
				"content":  map[string]string{"text": msg.Subject + "\n" + msg.Body},
			})
		},
	}
}

// webhookNotifier sends notifications via HTTP POST to a URL.
type webhookNotifier struct {
	url    string
	client *http.Client
	bodyFn func(Message) ([]byte, error) // custom body builder (nil = default JSON)
}

// NewWebhook creates a Notifier that POSTs JSON to the given URL.
func NewWebhook(url string) Notifier {
	return &webhookNotifier{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts the message as JSON to the webhook URL.
func (n *webhookNotifier) Send(ctx context.Context, msg Message) error {
	var body []byte
	var err error
	if n.bodyFn != nil {
		body, err = n.bodyFn(msg)
	} else {
		body, err = json.Marshal(map[string]string{
			"subject": msg.Subject,
			"body":    msg.Body,
		})
	}
	if err != nil {
		return fmt.Errorf("notify marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notify: webhook returned %d", resp.StatusCode)
	}
	return nil
}

// retryNotifier wraps a Notifier with retry logic.
type retryNotifier struct {
	inner      Notifier
	maxRetries int
}

// WithRetry wraps a Notifier to retry failed sends up to maxRetries times
// with exponential backoff (1s, 2s, 4s, ...).
func WithRetry(n Notifier, maxRetries int) Notifier {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &retryNotifier{inner: n, maxRetries: maxRetries}
}

// Send attempts to send the message, retrying on failure with exponential backoff.
func (n *retryNotifier) Send(ctx context.Context, msg Message) error {
	var lastErr error
	for i := 0; i <= n.maxRetries; i++ {
		if err := n.inner.Send(ctx, msg); err != nil {
			lastErr = err
			if timeutil.ShouldRetry(i, n.maxRetries) {
				wait := timeutil.Backoff(i, 1*time.Second, 60*time.Second)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("notify: all %d retries failed: %w", n.maxRetries, lastErr)
}
