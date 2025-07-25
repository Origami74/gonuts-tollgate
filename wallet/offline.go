package wallet

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// isNetworkError checks if an error is related to network connectivity
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	networkErrors := []string{
		"no such host",
		"connection refused",
		"connection timeout",
		"network is unreachable",
		"temporary failure in name resolution",
		"i/o timeout",
		"no route to host",
		"connection reset by peer",
		"server misbehaving",
		"dial tcp",
		"lookup",
	}

	for _, netErr := range networkErrors {
		if strings.Contains(strings.ToLower(errStr), netErr) {
			return true
		}
	}

	// Check for specific network error types
	if _, ok := err.(net.Error); ok {
		return true
	}
	if _, ok := err.(*net.OpError); ok {
		return true
	}
	if _, ok := err.(*url.Error); ok {
		return true
	}

	return false
}

// checkConnectivity performs a basic connectivity check to a mint
func checkConnectivity(mintURL string) bool {
	// Simple timeout-based connectivity check
	conn, err := net.DialTimeout("tcp", extractHost(mintURL), 5*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

// extractHost extracts host:port from a URL for connectivity testing
func extractHost(mintURL string) string {
	parsedURL, err := url.Parse(mintURL)
	if err != nil {
		return ""
	}

	host := parsedURL.Host
	if parsedURL.Port() == "" {
		// Add default port based on scheme
		switch parsedURL.Scheme {
		case "https":
			host += ":443"
		case "http":
			host += ":80"
		default:
			host += ":80"
		}
	}

	return host
}

// wrapNetworkError creates a user-friendly error message for network issues
func wrapNetworkError(err error, operation string) error {
	if err == nil {
		return nil
	}

	if !isNetworkError(err) {
		return err
	}

	// Create informative error message for users
	return fmt.Errorf("network connection issue during %s: %v\n"+
		"This may indicate:\n"+
		"- Internet connection is down\n"+
		"- DNS resolution problems\n"+
		"- Mint server is unreachable\n"+
		"Consider enabling offline mode or checking your connection", operation, err)
}
