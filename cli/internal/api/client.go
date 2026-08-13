package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxProblemBody = 1 << 20

type Client struct {
	BaseURL    *url.URL
	Token      string
	HTTPClient *http.Client
}

type ProblemError struct {
	Status int
	Code   string
	Detail string
}

func (e *ProblemError) Error() string {
	message := e.Detail
	if message == "" {
		message = http.StatusText(e.Status)
	}
	if e.Code != "" {
		message = fmt.Sprintf("%s (%s)", message, e.Code)
	}
	if e.Status > 0 {
		return fmt.Sprintf("HTTP %d: %s", e.Status, message)
	}
	return message
}

func NewClient(host string, token string) (*Client, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	baseURL, err := url.Parse(host)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid host %q", host)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("host must use http or https: %q", host)
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("host must not include a query or fragment: %q", host)
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return &Client{
		BaseURL:    baseURL,
		Token:      strings.TrimSpace(token),
		HTTPClient: http.DefaultClient,
	}, nil
}

func (c *Client) Do(
	ctx context.Context,
	method string,
	requestPath string,
	body io.Reader,
	headers http.Header,
) (*http.Response, error) {
	target, err := c.urlFor(requestPath)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create API request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for key, values := range headers {
		request.Header.Del(key)
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send API request: %w", err)
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return response, nil
	}

	contents, readErr := io.ReadAll(io.LimitReader(response.Body, maxProblemBody))
	_ = response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read API error response: %w", readErr)
	}
	return nil, DecodeProblem(response.StatusCode, contents)
}

func (c *Client) GetJSON(ctx context.Context, requestPath string, output any) error {
	response, err := c.Do(ctx, http.MethodGet, requestPath, nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

func (c *Client) PostJSON(ctx context.Context, requestPath string, input any, output any) error {
	var body io.Reader
	if input != nil {
		contents, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode API request: %w", err)
		}
		body = bytes.NewReader(contents)
	}
	response, err := c.Do(ctx, http.MethodPost, requestPath, body, http.Header{
		"Content-Type": {"application/json"},
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

func DecodeProblem(status int, contents []byte) error {
	var envelope struct {
		Error struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"error"`
	}
	problem := &ProblemError{Status: status}
	if err := json.Unmarshal(contents, &envelope); err == nil {
		problem.Code = strings.TrimSpace(envelope.Error.Code)
		problem.Detail = strings.TrimSpace(envelope.Error.Detail)
	}
	if problem.Detail == "" && problem.Code == "" {
		problem.Detail = strings.TrimSpace(string(contents))
	}
	return problem
}

func (c *Client) urlFor(requestPath string) (*url.URL, error) {
	parsed, err := url.Parse(requestPath)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return nil, fmt.Errorf("invalid API path %q", requestPath)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		parsed.Path = "/" + parsed.Path
	}
	target := *c.BaseURL
	target.Path = strings.TrimRight(c.BaseURL.Path, "/") + parsed.Path
	target.RawPath = ""
	target.RawQuery = parsed.RawQuery
	target.Fragment = ""
	return &target, nil
}
