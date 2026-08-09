package runner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const jobProxyImage = "alpine:3.20"

type engineNetwork struct {
	client      *engineHTTPClient
	networkID   string
	networkName string
	proxyID     string
	proxyURL    string
}

func (network *engineNetwork) Close(ctx context.Context) error {
	var firstErr error
	if network.networkID != "" {
		var inspected struct {
			Containers map[string]struct{} `json:"Containers"`
		}
		if err := network.client.get(ctx, "/networks/"+network.networkID, &inspected); err != nil {
			if !isEngineNotFound(err) {
				firstErr = err
			}
		} else {
			for containerID := range inspected.Containers {
				path := "/containers/" + containerID + "?force=true"
				if err := network.client.delete(ctx, path); err != nil && !isEngineNotFound(err) && firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	if network.proxyID != "" {
		path := "/containers/" + network.proxyID + "?force=true"
		if err := network.client.delete(ctx, path); err != nil && !isEngineNotFound(err) && firstErr == nil {
			firstErr = err
		}
	}
	if network.networkID != "" {
		if err := network.client.delete(ctx, "/networks/"+network.networkID); err != nil && !isEngineNotFound(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (worker *Worker) createJobNetwork(ctx context.Context, jobID string) (*engineNetwork, error) {
	client, err := newEngineHTTPClient()
	if err != nil {
		return nil, err
	}
	networkName := "lorehub-job-" + strings.ToLower(strings.ReplaceAll(jobID, "-", ""))
	if len(networkName) > 63 {
		networkName = networkName[:63]
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := client.post(ctx, "/networks/create", map[string]any{
		"Name":       networkName,
		"Driver":     "bridge",
		"Internal":   true,
		"Attachable": true,
	}, &created); err != nil {
		return nil, fmt.Errorf("create internal job network: %w", err)
	}
	network := &engineNetwork{
		client:      client,
		networkID:   created.ID,
		networkName: networkName,
	}
	if network.networkID == "" {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, errors.New("Docker returned no job network ID")
	}
	proxyURL, err := url.Parse(worker.config.EngineProxyURL)
	if err != nil || proxyURL.Scheme != "http" || proxyURL.Host == "" || proxyURL.Path != "" ||
		proxyURL.User != nil {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, errors.New("engine forward proxy URL is invalid")
	}
	proxyHost, proxyPort, splitErr := net.SplitHostPort(proxyURL.Host)
	parsedProxyPort, portErr := strconv.Atoi(proxyPort)
	if splitErr != nil || net.ParseIP(proxyHost) == nil || portErr != nil ||
		parsedProxyPort < 1 || parsedProxyPort > 65535 {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, errors.New("engine forward proxy URL must use an IP address and port")
	}
	proxyCommand := "while true; do nc -lk -p 3128 -e nc " + net.JoinHostPort(proxyHost, proxyPort) + "; done"
	var container struct {
		ID string `json:"Id"`
	}
	if err := client.post(ctx, "/containers/create", map[string]any{
		"Image": jobProxyImage,
		"Cmd":   []string{"sh", "-c", proxyCommand},
		"HostConfig": map[string]any{
			"AutoRemove":     true,
			"NetworkMode":    networkName,
			"ReadonlyRootfs": true,
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges:true"},
		},
	}, &container); err != nil {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("create internal proxy gateway: %w", err)
	}
	network.proxyID = container.ID
	if network.proxyID == "" {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, errors.New("Docker returned no proxy gateway ID")
	}
	if err := client.post(ctx, "/containers/"+network.proxyID+"/start", nil, nil); err != nil {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("start internal proxy gateway: %w", err)
	}
	if err := client.post(ctx, "/networks/bridge/connect", map[string]any{
		"Container": network.proxyID,
	}, nil); err != nil {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("connect internal proxy gateway to disposable bridge: %w", err)
	}
	var inspected struct {
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := client.get(ctx, "/containers/"+network.proxyID+"/json", &inspected); err != nil {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("inspect internal proxy gateway: %w", err)
	}
	proxyEndpoint, ok := inspected.NetworkSettings.Networks[networkName]
	if !ok || proxyEndpoint.IPAddress == "" {
		_ = network.Close(context.WithoutCancel(ctx))
		return nil, errors.New("Docker returned no internal proxy gateway address")
	}
	network.proxyURL = "http://" + proxyEndpoint.IPAddress + ":3128"
	return network, nil
}

type engineHTTPClient struct {
	client  *http.Client
	baseURL string
}

func newEngineHTTPClient() (*engineHTTPClient, error) {
	host := os.Getenv("DOCKER_HOST")
	if !strings.HasPrefix(host, "tcp://") {
		return nil, errors.New("DOCKER_HOST must use the Docker TCP endpoint")
	}
	endpoint := "https://" + strings.TrimPrefix(host, "tcp://")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.User != nil {
		return nil, errors.New("DOCKER_HOST is not a valid Docker TCP endpoint")
	}
	certPath := os.Getenv("DOCKER_CERT_PATH")
	if certPath == "" || os.Getenv("DOCKER_TLS_VERIFY") != "1" {
		return nil, errors.New("Docker engine mTLS is required")
	}
	caPEM, err := os.ReadFile(filepath.Join(certPath, "ca.pem"))
	if err != nil {
		return nil, fmt.Errorf("read Docker CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("Docker CA is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(
		filepath.Join(certPath, "cert.pem"), filepath.Join(certPath, "key.pem"),
	)
	if err != nil {
		return nil, fmt.Errorf("load Docker client certificate: %w", err)
	}
	return &engineHTTPClient{
		baseURL: endpoint,
		client: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      pool,
				Certificates: []tls.Certificate{certificate},
				MinVersion:   tls.VersionTLS12,
			},
		}},
	}, nil
}

func (client *engineHTTPClient) get(ctx context.Context, path string, destination any) error {
	return client.request(ctx, http.MethodGet, path, nil, destination)
}

func (client *engineHTTPClient) post(ctx context.Context, path string, body any, destination any) error {
	return client.request(ctx, http.MethodPost, path, body, destination)
}

func (client *engineHTTPClient) delete(ctx context.Context, path string) error {
	return client.request(ctx, http.MethodDelete, path, nil, nil)
}

func (client *engineHTTPClient) request(
	ctx context.Context,
	method string,
	path string,
	body any,
	destination any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return engineHTTPError{status: response.StatusCode, message: string(message)}
	}
	if destination == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

type engineHTTPError struct {
	status  int
	message string
}

func (err engineHTTPError) Error() string {
	return fmt.Sprintf("Docker API returned %d: %s", err.status, strings.TrimSpace(err.message))
}

func isEngineNotFound(err error) bool {
	var engineErr engineHTTPError
	return errors.As(err, &engineErr) && engineErr.status == http.StatusNotFound
}

func cleanupJobNetwork(network *engineNetwork) {
	if network == nil {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = network.Close(cleanupContext)
}
