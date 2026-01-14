package cutover

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/cutover"
)

// HealthChecker executes health checks.
type HealthChecker struct {
	// httpClient is used for HTTP health checks.
	httpClient *http.Client

	// insecureClient is used when TLS verification is skipped.
	insecureClient *http.Client
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
		insecureClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: 10 * time.Second,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}

// Execute runs a health check and returns the result.
func (h *HealthChecker) Execute(ctx context.Context, check *cutover.HealthCheck) *cutover.HealthCheckResult {
	result := cutover.NewHealthCheckResult(check.ID, check.Name)
	startTime := time.Now()

	var err error
	for attempt := 0; attempt <= check.Retries; attempt++ {
		result.Attempts = attempt + 1

		switch check.Type {
		case cutover.HealthCheckHTTP:
			err = h.executeHTTP(ctx, check, result)
		case cutover.HealthCheckTCP:
			err = h.executeTCP(ctx, check, result)
		case cutover.HealthCheckDNS:
			err = h.executeDNS(ctx, check, result)
		case cutover.HealthCheckCommand:
			err = h.executeCommand(ctx, check, result)
		case cutover.HealthCheckDatabase:
			err = h.executeDatabase(ctx, check, result)
		default:
			err = fmt.Errorf("unsupported health check type: %s", check.Type)
		}

		if err == nil {
			result.MarkPassed(time.Since(startTime), result.Response)
			return result
		}

		if attempt < check.Retries {
			select {
			case <-ctx.Done():
				result.MarkFailed(time.Since(startTime), ctx.Err().Error())
				return result
			case <-time.After(check.RetryDelay):
				// Continue to next attempt
			}
		}
	}

	result.MarkFailed(time.Since(startTime), err.Error())
	return result
}

// executeHTTP performs an HTTP health check.
func (h *HealthChecker) executeHTTP(ctx context.Context, check *cutover.HealthCheck, result *cutover.HealthCheckResult) error {
	method := check.Method
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if check.Body != "" {
		bodyReader = strings.NewReader(check.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, check.Endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range check.Headers {
		req.Header.Set(key, value)
	}

	// Choose client based on TLS verification setting
	client := h.httpClient
	if check.SkipTLSVerify {
		client = h.insecureClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	result.StatusCode = resp.StatusCode
	result.Details["status_code"] = resp.StatusCode
	result.Details["headers"] = resp.Header

	// Read response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	result.Response = string(body)

	// Check expected status
	if check.ExpectedStatus > 0 && resp.StatusCode != check.ExpectedStatus {
		return fmt.Errorf("unexpected status: got %d, expected %d", resp.StatusCode, check.ExpectedStatus)
	}

	// Check expected body
	if check.ExpectedBody != "" {
		if check.ExpectedBodyIsRegex {
			re, err := regexp.Compile(check.ExpectedBody)
			if err != nil {
				return fmt.Errorf("invalid regex pattern: %w", err)
			}
			if !re.MatchString(result.Response) {
				return fmt.Errorf("response body does not match pattern: %s", check.ExpectedBody)
			}
		} else {
			if !strings.Contains(result.Response, check.ExpectedBody) {
				return fmt.Errorf("response body does not contain expected text")
			}
		}
	}

	return nil
}

// executeTCP performs a TCP health check.
func (h *HealthChecker) executeTCP(ctx context.Context, check *cutover.HealthCheck, result *cutover.HealthCheckResult) error {
	timeout := check.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	dialer := net.Dialer{
		Timeout: timeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", check.Endpoint)
	if err != nil {
		return fmt.Errorf("TCP connection failed: %w", err)
	}
	defer func() { _ = conn.Close() }()

	result.Response = fmt.Sprintf("TCP connection to %s successful", check.Endpoint)
	result.Details["remote_addr"] = conn.RemoteAddr().String()

	return nil
}

// executeDNS performs a DNS health check.
func (h *HealthChecker) executeDNS(ctx context.Context, check *cutover.HealthCheck, result *cutover.HealthCheckResult) error {
	resolver := net.DefaultResolver

	// Parse the endpoint to get the hostname
	hostname := check.Endpoint
	if strings.Contains(hostname, "://") {
		parts := strings.SplitN(hostname, "://", 2)
		if len(parts) > 1 {
			hostname = parts[1]
		}
	}
	hostname = strings.Split(hostname, "/")[0]
	hostname = strings.Split(hostname, ":")[0]

	addrs, err := resolver.LookupHost(ctx, hostname)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	result.Response = strings.Join(addrs, ", ")
	result.Details["resolved_addresses"] = addrs

	// Check expected value if specified
	if check.ExpectedDNSValue != "" {
		found := false
		for _, addr := range addrs {
			if addr == check.ExpectedDNSValue {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("DNS lookup did not return expected value: %s (got: %s)", check.ExpectedDNSValue, result.Response)
		}
	}

	return nil
}

// executeCommand performs a command health check.
func (h *HealthChecker) executeCommand(ctx context.Context, check *cutover.HealthCheck, result *cutover.HealthCheckResult) error {
	timeout := check.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", check.Command)
	output, err := cmd.CombinedOutput()
	result.Response = string(output)

	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	// Check expected output if specified
	if check.ExpectedOutput != "" {
		if !strings.Contains(result.Response, check.ExpectedOutput) {
			return fmt.Errorf("command output does not contain expected text: %s", check.ExpectedOutput)
		}
	}

	return nil
}

// executeDatabase performs a database health check.
func (h *HealthChecker) executeDatabase(ctx context.Context, check *cutover.HealthCheck, result *cutover.HealthCheckResult) error {
	// For now, just do a TCP check on the database port
	// Full database connectivity would require database drivers
	endpoint := check.Endpoint

	// Parse connection string to get host:port
	// Support formats like: postgres://user:pass@host:port/db or host:port
	host := endpoint
	if strings.Contains(endpoint, "://") {
		// Parse URL-style connection string
		parts := strings.SplitN(endpoint, "://", 2)
		if len(parts) > 1 {
			hostPart := parts[1]
			// Remove credentials if present
			if strings.Contains(hostPart, "@") {
				hostPart = strings.SplitN(hostPart, "@", 2)[1]
			}
			// Remove database name if present
			host = strings.Split(hostPart, "/")[0]
		}
	}

	// Default port based on endpoint prefix
	if !strings.Contains(host, ":") {
		if strings.HasPrefix(endpoint, "postgres") {
			host = host + ":5432"
		} else if strings.HasPrefix(endpoint, "mysql") {
			host = host + ":3306"
		} else if strings.HasPrefix(endpoint, "redis") {
			host = host + ":6379"
		} else {
			host = host + ":5432" // Default to postgres
		}
	}

	// Create a modified check for TCP
	tcpCheck := &cutover.HealthCheck{
		ID:       check.ID,
		Name:     check.Name,
		Type:     cutover.HealthCheckTCP,
		Endpoint: host,
		Timeout:  check.Timeout,
	}

	return h.executeTCP(ctx, tcpCheck, result)
}
