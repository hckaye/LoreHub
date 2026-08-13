package runnerclient

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

	"github.com/lorehub/lorehub/services/api/internal/runner"
)

const runnerControlPath = "/api/v1/actions/runner"

type Client struct {
	baseURL    *url.URL
	credential string
	httpClient *http.Client
}

type RegistrationRequest struct {
	Name    string   `json:"name"`
	Labels  []string `json:"labels"`
	Version string   `json:"version"`
}

type RegistrationResponse struct {
	Token  string `json:"token"`
	Runner struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Labels []string `json:"labels"`
	} `json:"runner"`
}

type Claim struct {
	Job          *runner.Job `json:"job"`
	LeaseSeconds int64       `json:"leaseSeconds"`
}

func NewClient(rawURL string, credential string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("runner URL must be an HTTP or HTTPS origin")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, errors.New("runner URL must use HTTP or HTTPS")
	}
	if strings.TrimSpace(credential) == "" {
		return nil, errors.New("runner credential is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: parsed, credential: credential, httpClient: httpClient}, nil
}

func Register(
	ctx context.Context,
	rawURL string,
	registrationToken string,
	input RegistrationRequest,
	httpClient *http.Client,
) (RegistrationResponse, error) {
	client, err := NewClient(rawURL, registrationToken, httpClient)
	if err != nil {
		return RegistrationResponse{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return RegistrationResponse{}, fmt.Errorf("encode runner registration: %w", err)
	}
	var response RegistrationResponse
	if _, err := client.request(
		ctx, http.MethodPost, runnerControlPath+"/register", body, nil, &response,
		[]int{http.StatusCreated},
	); err != nil {
		return RegistrationResponse{}, err
	}
	if strings.TrimSpace(response.Token) == "" || strings.TrimSpace(response.Runner.ID) == "" {
		return RegistrationResponse{}, errors.New("runner registration returned incomplete credentials")
	}
	return response, nil
}

func (client *Client) Claim(ctx context.Context) (Claim, error) {
	var claim Claim
	status, err := client.request(ctx, http.MethodPost, runnerControlPath+"/claim", nil, nil, &claim, nil)
	if err != nil {
		return Claim{}, err
	}
	if status == http.StatusNoContent {
		return Claim{}, nil
	}
	if claim.Job == nil || claim.LeaseSeconds <= 0 {
		return Claim{}, errors.New("runner claim returned an invalid lease")
	}
	return claim, nil
}

func (client *Client) Heartbeat(ctx context.Context, jobID string) error {
	_, err := client.request(ctx, http.MethodPost, client.jobPath(jobID, "heartbeat"), nil, nil, nil, nil)
	return err
}

func (client *Client) CancellationRequested(ctx context.Context, jobID string) (bool, error) {
	var response struct {
		Requested bool `json:"requested"`
	}
	_, err := client.request(ctx, http.MethodGet, client.jobPath(jobID, "cancellation"), nil, nil, &response, nil)
	return response.Requested, err
}

func (client *Client) ExecutionContext(
	ctx context.Context,
	jobID string,
) (runner.ResolvedExecutionContext, error) {
	var response runner.ResolvedExecutionContext
	err := client.requestJSON(ctx, http.MethodGet, client.jobPath(jobID, "context"), nil, &response, nil)
	return response, err
}

func (client *Client) JobToken(ctx context.Context, jobID string) (runner.JobCredentials, error) {
	var response runner.JobCredentials
	err := client.requestJSON(ctx, http.MethodPost, client.jobPath(jobID, "token"), nil, &response, nil)
	return response, err
}

func (client *Client) UploadLog(ctx context.Context, jobID string, content []byte) error {
	headers := http.Header{"X-LoreHub-Upload-Offset": []string{"0"}}
	_, err := client.request(ctx, http.MethodPost, client.jobPath(jobID, "logs"), content, headers, nil, nil)
	return err
}

func (client *Client) UploadArtifact(
	ctx context.Context,
	jobID string,
	name string,
	content []byte,
) error {
	headers := http.Header{
		"X-LoreHub-Upload-Offset":   []string{"0"},
		"X-LoreHub-Upload-Complete": []string{"true"},
		"X-LoreHub-Artifact-Name":   []string{name},
	}
	_, err := client.request(ctx, http.MethodPost, client.jobPath(jobID, "artifacts"), content, headers, nil, nil)
	return err
}

func (client *Client) Complete(ctx context.Context, jobID string, conclusion string) error {
	body, err := json.Marshal(map[string]string{"conclusion": conclusion})
	if err != nil {
		return err
	}
	_, err = client.request(ctx, http.MethodPost, client.jobPath(jobID, "complete"), body, nil, nil, nil)
	return err
}

func (client *Client) jobPath(jobID string, operation string) string {
	return runnerControlPath + "/jobs/" + url.PathEscape(jobID) + "/" + operation
}

func (client *Client) requestJSON(
	ctx context.Context,
	method string,
	path string,
	body []byte,
	target any,
	headers http.Header,
) error {
	_, err := client.request(ctx, method, path, body, headers, target, nil)
	return err
}

func (client *Client) request(
	ctx context.Context,
	method string,
	path string,
	body []byte,
	headers http.Header,
	target any,
	expectedStatus []int,
) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.String()+path, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create runner control request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.credential)
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if len(body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("send runner control request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if len(expectedStatus) == 0 {
		expectedStatus = []int{http.StatusOK, http.StatusNoContent}
	}
	if !containsStatus(expectedStatus, response.StatusCode) {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return response.StatusCode, fmt.Errorf("runner control returned %s: %s",
			response.Status, strings.TrimSpace(string(message)))
	}
	if target != nil && response.StatusCode != http.StatusNoContent {
		decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
		if err := decoder.Decode(target); err != nil {
			return response.StatusCode, fmt.Errorf("decode runner control response: %w", err)
		}
	}
	return response.StatusCode, nil
}

func containsStatus(values []int, expected int) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func LeaseDuration(claim Claim) time.Duration {
	return time.Duration(claim.LeaseSeconds) * time.Second
}

func OffsetHeader(offset int64) string {
	return strconv.FormatInt(offset, 10)
}
