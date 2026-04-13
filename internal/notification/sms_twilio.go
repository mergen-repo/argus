package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const twilioBaseURL = "https://api.twilio.com/2010-04-01"

type twilioClient struct {
	accountID      string
	authToken      string
	fromPhone      string
	statusCallback string
	http           *http.Client
	logger         zerolog.Logger
}

func newTwilioClient(cfg SMSConfig, logger zerolog.Logger) *twilioClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &twilioClient{
		accountID:      cfg.AccountID,
		authToken:      cfg.AuthToken,
		fromPhone:      cfg.FromPhone,
		statusCallback: cfg.StatusCallbackURL,
		http: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

type twilioResponse struct {
	SID    string `json:"sid"`
	Status string `json:"status"`
}

func (c *twilioClient) Send(ctx context.Context, to, body string) error {
	endpoint := fmt.Sprintf("%s/Accounts/%s/Messages.json", twilioBaseURL, c.accountID)

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", c.fromPhone)
	form.Set("Body", body)
	if c.statusCallback != "" {
		form.Set("StatusCallback", c.statusCallback)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("notification: twilio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.accountID, c.authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("notification: twilio send: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var twilioResp twilioResponse
	_ = json.Unmarshal(respBody, &twilioResp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification: twilio status %d sid=%s status=%s", resp.StatusCode, twilioResp.SID, twilioResp.Status)
	}

	c.logger.Info().
		Str("sid", twilioResp.SID).
		Str("status", twilioResp.Status).
		Str("to", to).
		Msg("twilio SMS sent")

	return nil
}

func (c *twilioClient) VerifyStatusSignature(fullURL string, form url.Values, headerSig string) bool {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(form.Get(k))
	}

	mac := hmac.New(sha1.New, []byte(c.authToken))
	mac.Write([]byte(sb.String()))
	computed := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(computed), []byte(headerSig))
}

// SendWithResult sends an SMS and returns the Twilio SID on success.
func (c *twilioClient) SendWithResult(ctx context.Context, to, body string) (string, error) {
	endpoint := fmt.Sprintf("%s/Accounts/%s/Messages.json", twilioBaseURL, c.accountID)

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", c.fromPhone)
	form.Set("Body", body)
	if c.statusCallback != "" {
		form.Set("StatusCallback", c.statusCallback)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("notification: twilio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.accountID, c.authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("notification: twilio send: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var twilioResp twilioResponse
	_ = json.Unmarshal(respBody, &twilioResp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("notification: twilio status %d sid=%s status=%s", resp.StatusCode, twilioResp.SID, twilioResp.Status)
	}

	c.logger.Info().
		Str("sid", twilioResp.SID).
		Str("status", twilioResp.Status).
		Str("to", to).
		Msg("twilio SMS sent")

	return twilioResp.SID, nil
}
