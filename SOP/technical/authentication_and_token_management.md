// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT


# Standard Operating Procedure (SOP): Authentication and Token Management

### Objective
To define how `tell-me-go` authenticates exclusively with Google Vertex AI using OAuth2 Bearer Tokens (User Credentials or Service Accounts).

---

### Prerequisites
- Go toolchain 1.24+.
- `gcloud` SDK installed (for User Authentication).
- Valid Google Cloud Project ID and Region (for Vertex AI).

---

### Step-by-Step Instructions

#### 1. Authentication Strategy
`tell-me-go` uses Google Vertex AI as its sole provider. All requests require an OAuth2 Bearer Token passed in the HTTP `Authorization` header.

#### 2. Token Generation
- **User Credentials**: By default, the system retrieves the access token using the equivalent of `gcloud auth print-access-token`.
- **Service Account**: (Planned) If a `KEY_FILE` is provided, the system must load the JSON key and generate a JWT-based OAuth2 token.
- **Caching**: Tokens should be retrieved once per session or refreshed as needed.

#### 3. Client Header Injection
- **Vertex AI**: The token is retrieved via the `internal/auth` package and injected into the `google.golang.org/genai` client via `HTTPOptions.Headers`.
- **Project/Location**: The project ID and location are parsed from the `AIURL` or environment and passed to the SDK's `ClientConfig`.

---

### Package Structure
- **`internal/auth`**: Dedicated package for token discovery (e.g., via `gcloud`).
- **`internal/api`**: Configures the GenAI SDK client with the injected authentication headers.

---

### Code Templates

#### Authenticator Interface:
```go
type Authenticator interface {
    Apply(req *Request) error
}
```

---

### Verification
1.  **Unit Tests**: Mock the token generation process.
2.  **Integration**: Verify that the correct Bearer headers are sent using a mock HTTP server.
3.  **Security**: Ensure tokens are never logged or saved in history files.

---

### Best Practices
- **Minimal Scopes**: Only request the `https://www.googleapis.com/auth/cloud-platform` scope.
- **Service Account Safety**: Never hardcode keys; always use file paths or environment variables.
- **Vertex AI Only**: Do not implement logic for AI Studio (API Keys).
