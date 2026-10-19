package statementsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type InfraiClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

type Envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *InfraiError    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type InfraiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *InfraiError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type PDFGenerateRequest struct {
	HTML        string `json:"html,omitempty"`
	PageSize    string `json:"page_size,omitempty"`
	Orientation string `json:"orientation,omitempty"`
	Store       bool   `json:"store,omitempty"`
}

type PDFGenerateResponse struct {
	PDF   string `json:"pdf"`
	JobID string `json:"job_id"`
}

type UsageTimeseriesPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

type UsageTimeseriesResponse struct {
	Points []UsageTimeseriesPoint `json:"points"`
}

func NewInfraiClient(apiKey, baseURL string, httpClient *http.Client) *InfraiClient {
	return &InfraiClient{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

func (c *InfraiClient) pdfGenerate(ctx context.Context, req PDFGenerateRequest) (PDFGenerateResponse, error) {
	var out PDFGenerateResponse
	err := c.doJSON(ctx, http.MethodPost, "/pdf/generate", req, nil, &out)
	return out, err
}

func (c *InfraiClient) accountUsageTimeseries(ctx context.Context, query map[string]string) (UsageTimeseriesResponse, error) {
	var out UsageTimeseriesResponse
	err := c.doJSON(ctx, http.MethodGet, "/account/usage/timeseries", nil, query, &out)
	return out, err
}

func (c *InfraiClient) doJSON(ctx context.Context, method, path string, body any, query map[string]string, out any) error {
	var bodyReader io.Reader
	if body != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return err
		}
		bodyReader = buf
	}

	fullURL := c.baseURL + path
	if len(query) > 0 {
		u, err := url.Parse(fullURL)
		if err != nil {
			return err
		}
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		fullURL = u.String()
	}

	attempt := 0
	for {
		attempt++
		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}

		respBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env Envelope
		if len(respBytes) > 0 {
			if err := json.Unmarshal(respBytes, &env); err != nil {
				if resp.StatusCode >= 500 {
					return fmt.Errorf("transport status %d", resp.StatusCode)
				}
				return fmt.Errorf("decode envelope: %w", err)
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			sleep := retryDelay(resp.Header.Get("Retry-After"), attempt)
			time.Sleep(sleep)
			if body != nil {
				bodyReader = bytes.NewReader(respawnBody(body))
			}
			continue
		}

		if !env.OK {
			if env.Error == nil {
				return errors.New("infrai request failed")
			}
			env.Error.Status = resp.StatusCode
			return env.Error
		}

		if resp.StatusCode >= 500 {
			return fmt.Errorf("transport status %d", resp.StatusCode)
		}

		if out != nil && len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return err
			}
		}
		return nil
	}
}

func respawnBody(body any) []byte {
	b, _ := json.Marshal(body)
	return b
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<uint(attempt-1)) * 200 * time.Millisecond
}

type infraiFacade struct {
	pdf struct {
		generate func(context.Context, PDFGenerateRequest) (PDFGenerateResponse, error)
	}
	account struct {
		usage struct {
			timeseries func(context.Context, map[string]string) (UsageTimeseriesResponse, error)
		}
	}
}

func newInfraiFacade(client *InfraiClient) infraiFacade {
	var infrai infraiFacade
	infrai.pdf.generate = client.pdfGenerate
	infrai.account.usage.timeseries = client.accountUsageTimeseries
	return infrai
}
