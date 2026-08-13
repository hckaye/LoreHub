package cmdutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/spf13/cobra"
)

func newAPICommand(state *rootState) *cobra.Command {
	var method string
	var fields []string
	var rawFields []string
	var headers []string
	var raw bool

	command := &cobra.Command{
		Use:   "api PATH",
		Short: "Make an authenticated API request",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			host := state.host()
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			entry, _ := state.selectedHostEntry(hosts, host)
			token, _ := config.ResolveToken(entry.Token)
			client, err := state.client(host, token)
			if err != nil {
				return err
			}

			values := make(map[string]any, len(fields)+len(rawFields))
			for _, field := range fields {
				key, value, err := parseField(field, false)
				if err != nil {
					return err
				}
				values[key] = value
			}
			for _, field := range rawFields {
				key, value, err := parseField(field, true)
				if err != nil {
					return err
				}
				values[key] = value
			}

			requestPath := apiRequestPath(args[0])
			requestMethod := strings.ToUpper(strings.TrimSpace(method))
			if requestMethod == "" {
				requestMethod = http.MethodGet
			}
			var body io.Reader
			if methodUsesQuery(requestMethod) {
				requestPath, err = addQueryFields(requestPath, values)
				if err != nil {
					return err
				}
			} else if len(values) > 0 {
				contents, err := json.Marshal(values)
				if err != nil {
					return fmt.Errorf("encode API request: %w", err)
				}
				body = bytes.NewReader(contents)
			}

			requestHeaders := make(http.Header, len(headers))
			for _, header := range headers {
				name, value, err := parseHeader(header)
				if err != nil {
					return err
				}
				requestHeaders.Add(name, value)
			}
			if body != nil && requestHeaders.Get("Content-Type") == "" {
				requestHeaders.Set("Content-Type", "application/json")
			}

			response, err := client.Do(command.Context(), requestMethod, requestPath, body, requestHeaders)
			if err != nil {
				return err
			}
			contents, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				return fmt.Errorf("read API response: %w", readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close API response: %w", closeErr)
			}
			if raw {
				_, err = command.OutOrStdout().Write(contents)
				return err
			}
			return writeAPIResponse(command.OutOrStdout(), contents)
		},
	}
	command.Flags().StringVarP(&method, "method", "X", http.MethodGet, "HTTP method")
	command.Flags().StringArrayVar(&fields, "field", nil, "add a JSON field (key=value)")
	command.Flags().StringArrayVar(&rawFields, "raw-field", nil, "add a string field (key=value)")
	command.Flags().StringArrayVarP(&headers, "header", "H", nil, "set a request header (Name: value)")
	command.Flags().BoolVar(&raw, "raw", false, "write the response without formatting")
	return command
}

func apiRequestPath(value string) string {
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/api/v1/" + strings.TrimPrefix(value, "/")
}

func parseField(value string, raw bool) (string, any, error) {
	key, fieldValue, found := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", nil, fmt.Errorf("field must use key=value")
	}
	if raw {
		return key, fieldValue, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(fieldValue), &parsed); err != nil {
		parsed = fieldValue
	}
	return key, parsed, nil
}

func parseHeader(value string) (string, string, error) {
	name, headerValue, found := strings.Cut(value, ":")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return "", "", fmt.Errorf("header must use Name: value")
	}
	return name, strings.TrimSpace(headerValue), nil
}

func methodUsesQuery(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		return true
	default:
		return false
	}
}

func addQueryFields(requestPath string, values map[string]any) (string, error) {
	parsed, err := url.Parse(requestPath)
	if err != nil {
		return "", fmt.Errorf("parse API path: %w", err)
	}
	query := parsed.Query()
	for key, value := range values {
		text, err := fieldText(value)
		if err != nil {
			return "", err
		}
		query.Set(key, text)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func fieldText(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	contents, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode query field: %w", err)
	}
	return string(contents), nil
}

func writeAPIResponse(output io.Writer, contents []byte) error {
	var value any
	if err := json.Unmarshal(contents, &value); err != nil {
		if _, writeErr := output.Write(contents); writeErr != nil {
			return writeErr
		}
		if len(contents) == 0 || contents[len(contents)-1] != '\n' {
			_, writeErr := io.WriteString(output, "\n")
			return writeErr
		}
		return nil
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
