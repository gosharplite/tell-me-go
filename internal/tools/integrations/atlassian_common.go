package integrations

import (
	"encoding/base64"
	"fmt"
	"os"
)

const defaultAtlassianBaseURL = "https://02007.atlassian.net"

type atlassianProvider struct {
	baseURL string
}

func newAtlassianProvider() *atlassianProvider {
	baseURL := os.Getenv("ATLASSIAN_BASE_URL")
	if baseURL == "" {
		baseURL = defaultAtlassianBaseURL
	}
	return &atlassianProvider{baseURL: baseURL}
}

func (p *atlassianProvider) getAuthHeader() (string, error) {
	email := os.Getenv("ATLASSIAN_EMAIL")
	if email == "" {
		return "", fmt.Errorf("missing ATLASSIAN_EMAIL environment variable")
	}
	token := os.Getenv("ATLASSIAN_TOKEN")
	if token == "" {
		return "", fmt.Errorf("missing ATLASSIAN_TOKEN environment variable")
	}

	auth := fmt.Sprintf("%s:%s", email, token)
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	return fmt.Sprintf("Basic %s", encoded), nil
}
