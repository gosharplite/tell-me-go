# Standard Operating Procedure (SOP): Authentication and Token Management

### Objective
To define how `tell-me-go` authenticates with Google's Generative AI services, supporting both API Key (Google AI Studio) and OAuth2 Bearer Tokens (Google Vertex AI / Service Accounts).

---

### Prerequisites
- Go toolchain 1.21+.
- `gcloud` SDK installed (for User Authentication).
- Valid Google Cloud Project ID and Region (for Vertex AI).

---

### Step-by-Step Instructions

#### 1. Identification of Auth Mode
The system must determine the authentication method based on the configuration:
- **API Key Mode**: Triggered when `API_KEY` is provided in the config or environment. Used for the `generativelanguage.googleapis.com` (AI Studio) endpoint.
- **Bearer Token Mode**: Triggered when the `AIURL` contains `aiplatform.googleapis.com` (Vertex AI) or when a `KEY_FILE` is provided.

#### 2. Token Generation (Bearer Mode)
- **User Credentials**: If `KEY_FILE` is empty, the system should attempt to retrieve the token using the equivalent of `gcloud auth print-access-token`.
- **Service Account**: If `KEY_FILE` is provided, the system must load the JSON key and generate a JWT-based OAuth2 token with the appropriate scope (`https://www.googleapis.com/auth/cloud-platform`).
- **Caching**: Tokens should be cached in memory (or a temporary local file) until they expire to minimize latency.

#### 3. Client Header Injection
- **AI Studio**: The API Key is passed as a query parameter: `?key=YOUR_API_KEY`.
- **Vertex AI**: The token must be passed in the HTTP `Authorization` header: `Authorization: Bearer <TOKEN>`.

---

### Package Structure
- **`internal/auth`**: A new package dedicated to token discovery and generation.
- **`internal/api`**: Must be updated to accept an `Authenticator` interface or a token string.

---

### Code Templates

#### Authenticator Interface:
```go
type Authenticator interface {
    GetToken() (string, error)
    AuthHeader() (string, string) // e.g., "Authorization", "Bearer <token>"
}
```

#### Token Retrieval (Example):
```go
func GetGcloudToken() (string, error) {
    out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(string(out)), nil
}
```

---

### Verification
1.  **Unit Tests**: Mock the token generation process.
2.  **Integration**: Verify that the correct headers are sent for each mode using a mock HTTP server.
3.  **Security**: Ensure tokens are never logged to `stdout` or saved in insecure history files.

---

### Best Practices
- **Minimal Scopes**: Only request the scopes necessary for the AI API.
- **Expiry Awareness**: Check the token's remaining life before using it; refresh if less than 60 seconds remain.
- **Service Account Safety**: Never hardcode the content of a `KEY_FILE`. Always refer to the file path.

