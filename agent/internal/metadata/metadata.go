package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const imdsBase = "http://169.254.169.254/latest"

// InstanceID fetches the EC2 instance-id via IMDSv2 (with IMDSv1 fallback).
// Returns "" without error when not running on EC2.
func InstanceID(ctx context.Context, client *http.Client) (string, error) {
	c := client
	if c == nil {
		c = &http.Client{Timeout: 3 * time.Second}
	}

	token := ""
	// IMDSv2 token handshake.
	if req, err := http.NewRequestWithContext(ctx, http.MethodPut, imdsBase+"/api/token", nil); err == nil {
		req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
		if resp, err := c.Do(req); err == nil {
			if resp.StatusCode == http.StatusOK {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				token = string(b)
			}
			resp.Body.Close()
		}
	}

	get := func() (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, imdsBase+"/meta-data/instance-id", nil)
		if err != nil {
			return "", err
		}
		if token != "" {
			req.Header.Set("X-aws-ec2-metadata-token", token)
		}
		resp, err := c.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("imds status %d", resp.StatusCode)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return string(b), nil
	}

	id, err := get()
	if err != nil && token == "" {
		return "", nil // not on EC2 (or metadata unreachable) — treat as non-EC2
	}
	return id, err
}
