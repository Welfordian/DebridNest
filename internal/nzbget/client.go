package nzbget

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	StatusFinished = 14
	StatusFailed   = 16
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

type Group struct {
	NZBID          int    `json:"NZBID"`
	NZBName        string `json:"NZBName"`
	Status         int    `json:"Status"`
	Progress       int    `json:"Progress"`
	FileSizeMB     int    `json:"FileSizeMB"`
	RemainingSizeMB int   `json:"RemainingSizeMB"`
	DestDir        string `json:"DestDir"`
	Category       string `json:"Category"`
}

type File struct {
	ID       int    `json:"ID"`
	FileName string `json:"FileName"`
	FileSize int64  `json:"FileSizeLo"`
	DestDir  string `json:"DestDir"`
}

func New(baseURL, username, password string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("nzbget url is required")
	}
	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func (c *Client) AppendURL(ctx context.Context, filename, nzbURL, category string) (int, error) {
	nzbURL = strings.TrimSpace(nzbURL)
	if nzbURL == "" {
		return 0, fmt.Errorf("nzb url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nzbURL, nil)
	if err != nil {
		return 0, err
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch nzb: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return 0, fmt.Errorf("fetch nzb: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return 0, err
	}
	if len(body) == 0 {
		return 0, fmt.Errorf("empty nzb response")
	}
	return c.AppendContent(ctx, filename, body, category)
}

func (c *Client) AppendContent(ctx context.Context, filename string, content []byte, category string) (int, error) {
	if strings.TrimSpace(filename) == "" {
		filename = "download.nzb"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".nzb") {
		filename += ".nzb"
	}
	if category == "" {
		category = "debridnest"
	}
	encoded := base64.StdEncoding.EncodeToString(content)
	var nzbID int
	if err := c.call(ctx, "append", []any{
		filename,
		encoded,
		category,
		0,     // priority
		false, // addToTop
		false, // addPaused
		"",    // dupeKey
		0,     // dupeScore
		"SCORE", // dupeMode
		[]any{}, // ppParameters
	}, &nzbID); err != nil {
		return 0, err
	}
	if nzbID <= 0 {
		return 0, fmt.Errorf("nzbget append returned invalid id")
	}
	return nzbID, nil
}

func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	var groups []Group
	if err := c.call(ctx, "listgroups", []any{}, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (c *Client) ListFiles(ctx context.Context, nzbID int) ([]File, error) {
	var files []File
	if err := c.call(ctx, "listfiles", []any{nzbID, 0}, &files); err != nil {
		return nil, err
	}
	return files, nil
}

func (c *Client) GroupDelete(ctx context.Context, nzbID int) error {
	var ok bool
	return c.call(ctx, "editqueue", []any{"GroupDelete", 0, "", nzbID}, &ok)
}

func (c *Client) History(ctx context.Context, limit int) ([]Group, error) {
	if limit <= 0 {
		limit = 10
	}
	var items []Group
	if err := c.call(ctx, "history", []any{false, limit, ""}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) call(ctx context.Context, method string, params []any, result any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint, err := url.Parse(c.baseURL + "/jsonrpc")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("nzbget rpc: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("decode nzbget rpc: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("nzbget rpc %s: %s", method, envelope.Error.Message)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode nzbget result: %w", err)
	}
	return nil
}
