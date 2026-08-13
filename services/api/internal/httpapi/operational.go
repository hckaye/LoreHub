package httpapi

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxRateLimitClients = 100_000

var requestDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type OperationalOptions struct {
	MetricsToken      string
	RateLimitRequests int
	RateLimitWindow   time.Duration
	TrustedProxyCIDRs []netip.Prefix
}

type operationalState struct {
	metrics      *httpMetrics
	rateLimiter  *fixedWindowLimiter
	metricsToken string
}

type httpMetricKey struct {
	method string
	route  string
	status string
}

type httpMetricValue struct {
	count   uint64
	sum     float64
	buckets []uint64
}

type httpMetrics struct {
	mu         sync.Mutex
	requests   map[httpMetricKey]*httpMetricValue
	inFlight   atomic.Int64
	rejections atomic.Uint64
}

type rateLimitWindow struct {
	started time.Time
	count   int
}

type fixedWindowLimiter struct {
	mu             sync.Mutex
	requests       int
	window         time.Duration
	trustedProxies []netip.Prefix
	clients        map[netip.Addr]rateLimitWindow
	lastCleanup    time.Time
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *statusResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func WithOperationalEndpoints(options OperationalOptions) Option {
	return func(api *API) {
		api.operations = &operationalState{
			metrics: newHTTPMetrics(),
			rateLimiter: &fixedWindowLimiter{
				requests:       options.RateLimitRequests,
				window:         options.RateLimitWindow,
				trustedProxies: append([]netip.Prefix(nil), options.TrustedProxyCIDRs...),
				clients:        make(map[netip.Addr]rateLimitWindow),
			},
			metricsToken: options.MetricsToken,
		}
	}
}

func newHTTPMetrics() *httpMetrics {
	return &httpMetrics{requests: make(map[httpMetricKey]*httpMetricValue)}
}

func (api *API) operationalMiddleware(next http.Handler) http.Handler {
	if api.operations == nil {
		return next
	}
	return api.operations.metrics.observe(api.operations.rateLimiter.middleware(next, api.operations.metrics))
}

func (metrics *httpMetrics) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/metrics" {
			next.ServeHTTP(writer, request)
			return
		}
		started := time.Now()
		metrics.inFlight.Add(1)
		response := &statusResponseWriter{ResponseWriter: writer}
		next.ServeHTTP(response, request)
		metrics.inFlight.Add(-1)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		route := request.Pattern
		route = strings.TrimPrefix(route, request.Method+" ")
		if route == "" && status == http.StatusTooManyRequests {
			route = "rate_limited"
		} else if route == "" {
			route = "unmatched"
		}
		metrics.record(metricMethod(request.Method), route, status, time.Since(started))
	})
}

func (metrics *httpMetrics) record(method string, route string, status int, duration time.Duration) {
	key := httpMetricKey{method: method, route: route, status: strconv.Itoa(status)}
	seconds := duration.Seconds()
	metrics.mu.Lock()
	value := metrics.requests[key]
	if value == nil {
		value = &httpMetricValue{buckets: make([]uint64, len(requestDurationBuckets))}
		metrics.requests[key] = value
	}
	value.count++
	value.sum += seconds
	for index, boundary := range requestDurationBuckets {
		if seconds <= boundary {
			value.buckets[index]++
		}
	}
	metrics.mu.Unlock()
}

func metricMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func (metrics *httpMetrics) write(writer http.ResponseWriter) {
	metrics.mu.Lock()
	keys := make([]httpMetricKey, 0, len(metrics.requests))
	values := make(map[httpMetricKey]httpMetricValue, len(metrics.requests))
	for key, value := range metrics.requests {
		keys = append(keys, key)
		values[key] = httpMetricValue{
			count: value.count, sum: value.sum, buckets: append([]uint64(nil), value.buckets...),
		}
	}
	metrics.mu.Unlock()
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].route != keys[right].route {
			return keys[left].route < keys[right].route
		}
		if keys[left].method != keys[right].method {
			return keys[left].method < keys[right].method
		}
		return keys[left].status < keys[right].status
	})

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintln(writer, "# HELP lorehub_up Whether the LoreHub HTTP process is running.")
	_, _ = fmt.Fprintln(writer, "# TYPE lorehub_up gauge")
	_, _ = fmt.Fprintln(writer, "lorehub_up 1")
	_, _ = fmt.Fprintln(writer, "# HELP lorehub_http_requests_in_flight Requests currently being handled.")
	_, _ = fmt.Fprintln(writer, "# TYPE lorehub_http_requests_in_flight gauge")
	_, _ = fmt.Fprintf(writer, "lorehub_http_requests_in_flight %d\n", metrics.inFlight.Load())
	_, _ = fmt.Fprintln(writer, "# HELP lorehub_http_rate_limit_rejections_total Requests rejected by the rate limit.")
	_, _ = fmt.Fprintln(writer, "# TYPE lorehub_http_rate_limit_rejections_total counter")
	_, _ = fmt.Fprintf(writer, "lorehub_http_rate_limit_rejections_total %d\n", metrics.rejections.Load())
	_, _ = fmt.Fprintln(writer, "# HELP lorehub_http_requests_total Completed HTTP requests.")
	_, _ = fmt.Fprintln(writer, "# TYPE lorehub_http_requests_total counter")
	_, _ = fmt.Fprintln(writer, "# HELP lorehub_http_request_duration_seconds Request duration in seconds.")
	_, _ = fmt.Fprintln(writer, "# TYPE lorehub_http_request_duration_seconds histogram")
	for _, key := range keys {
		value := values[key]
		labels := metricLabels(key)
		_, _ = fmt.Fprintf(writer, "lorehub_http_requests_total{%s} %d\n", labels, value.count)
		for index, boundary := range requestDurationBuckets {
			_, _ = fmt.Fprintf(writer,
				"lorehub_http_request_duration_seconds_bucket{%s,le=\"%g\"} %d\n",
				labels, boundary, value.buckets[index])
		}
		_, _ = fmt.Fprintf(writer,
			"lorehub_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, value.count)
		_, _ = fmt.Fprintf(writer, "lorehub_http_request_duration_seconds_sum{%s} %g\n", labels, value.sum)
		_, _ = fmt.Fprintf(writer, "lorehub_http_request_duration_seconds_count{%s} %d\n", labels, value.count)
	}
}

func metricLabels(key httpMetricKey) string {
	return fmt.Sprintf("method=\"%s\",route=\"%s\",status=\"%s\"",
		metricLabelValue(key.method), metricLabelValue(key.route), key.status)
}

func metricLabelValue(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return replacer.Replace(value)
}

func (api *API) metrics(writer http.ResponseWriter, request *http.Request) {
	if api.operations == nil {
		http.NotFound(writer, request)
		return
	}
	if token := api.operations.metricsToken; token != "" {
		authorization := request.Header.Get("Authorization")
		expected := "Bearer " + token
		if len(authorization) != len(expected) ||
			subtle.ConstantTimeCompare([]byte(authorization), []byte(expected)) != 1 {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="LoreHub metrics"`)
			writeProblem(writer, http.StatusUnauthorized, "authentication_required", "A metrics token is required")
			return
		}
	}
	api.operations.metrics.write(writer)
}

func (limiter *fixedWindowLimiter) middleware(next http.Handler, metrics *httpMetrics) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/") && !strings.HasPrefix(request.URL.Path, "/auth/") {
			next.ServeHTTP(writer, request)
			return
		}
		if limiter.allow(limiter.clientAddress(request), time.Now()) {
			next.ServeHTTP(writer, request)
			return
		}
		metrics.rejections.Add(1)
		retryAfter := max(1, int((limiter.window+time.Second-1)/time.Second))
		writer.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeProblem(writer, http.StatusTooManyRequests, "rate_limited", "Too many requests")
	})
}

func (limiter *fixedWindowLimiter) allow(client netip.Addr, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.lastCleanup.IsZero() || now.Sub(limiter.lastCleanup) >= limiter.window {
		for address, window := range limiter.clients {
			if now.Sub(window.started) >= limiter.window {
				delete(limiter.clients, address)
			}
		}
		limiter.lastCleanup = now
	}
	window, exists := limiter.clients[client]
	if !exists {
		if len(limiter.clients) >= maxRateLimitClients {
			return false
		}
		limiter.clients[client] = rateLimitWindow{started: now, count: 1}
		return true
	}
	if now.Sub(window.started) >= limiter.window {
		limiter.clients[client] = rateLimitWindow{started: now, count: 1}
		return true
	}
	if window.count >= limiter.requests {
		return false
	}
	window.count++
	limiter.clients[client] = window
	return true
}

func (limiter *fixedWindowLimiter) clientAddress(request *http.Request) netip.Addr {
	remote, ok := parseRemoteAddress(request.RemoteAddr)
	if !ok || !limiter.trusted(remote) {
		return remote
	}
	values := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	for index := len(values) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(values[index]))
		if err != nil {
			return remote
		}
		candidate = candidate.Unmap()
		if !limiter.trusted(candidate) {
			return candidate
		}
		remote = candidate
	}
	return remote
}

func parseRemoteAddress(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.IPv4Unspecified(), false
	}
	return address.Unmap(), true
}

func (limiter *fixedWindowLimiter) trusted(address netip.Addr) bool {
	for _, prefix := range limiter.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
