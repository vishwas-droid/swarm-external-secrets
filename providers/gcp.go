package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/docker/go-plugins-helpers/secrets"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
)

type GCPProvider struct {
	client *secretmanager.Client
	config *GCPConfig
	ctx    context.Context
}

type GCPConfig struct {
	ProjectID       string
	CredentialsPath string
	CredentialsJSON string
}

func (g *GCPProvider) GetProviderName() string {
	return "gcp"
}

func (g *GCPProvider) Initialize(config map[string]string) error {
	g.ctx = context.Background()
	g.config = &GCPConfig{
		ProjectID:       getConfigOrDefault(config, "GCP_PROJECT_ID", ""),
		CredentialsPath: getConfigOrDefault(config, "GOOGLE_APPLICATION_CREDENTIALS", ""),
		CredentialsJSON: config["GCP_CREDENTIALS_JSON"],
	}

	if g.config.ProjectID == "" {
		return fmt.Errorf("GCP_PROJECT_ID must be provided")
	}

	var client *secretmanager.Client
	var err error

	if g.config.CredentialsJSON != "" {
		client, err = secretmanager.NewClient(g.ctx, option.WithCredentialsJSON([]byte(g.config.CredentialsJSON)))
	} else if g.config.CredentialsPath != "" {
		client, err = secretmanager.NewClient(g.ctx, option.WithCredentialsFile(g.config.CredentialsPath))
	} else {
		client, err = secretmanager.NewClient(g.ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to create secretmanager client: %w", err)
	}

	g.client = client
	log.Printf("Initialized GCP Secret Manager provider for project: %s", g.config.ProjectID)
	return nil
}

func (g *GCPProvider) GetSecret(ctx context.Context, req secrets.Request) ([]byte, error) {
	if g.client == nil {
		return nil, fmt.Errorf("gcp provider not initialized")
	}

	secretVersionName := g.buildSecretVersionName(req)

	secretRequest := &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretVersionName,
	}

	result, err := g.client.AccessSecretVersion(ctx, secretRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to access secret version: %w", err)
	}

	if result.Payload == nil || result.Payload.Data == nil {
		return nil, fmt.Errorf("secret %s has no payload data", secretVersionName)
	}

	return g.extractSecretValue(string(result.Payload.Data), req)
}

func (g *GCPProvider) buildSecretVersionName(req secrets.Request) string {
	if customPath, exists := req.SecretLabels["gcp_secret_name"]; exists {
		if strings.Contains(customPath, "/versions/") {
			return customPath
		}
		return fmt.Sprintf("%s/versions/latest", customPath)
	}

	projectID := g.config.ProjectID

	secretName := req.SecretName
	if req.ServiceName != "" {
		secretName = fmt.Sprintf("%s-%s", req.ServiceName, req.SecretName)
	}

	return fmt.Sprintf(
		"projects/%s/secrets/%s/versions/latest",
		projectID,
		secretName,
	)
}

func (g *GCPProvider) extractSecretValue(secretString string, req secrets.Request) ([]byte, error) {
	if field, exists := req.SecretLabels["gcp_field"]; exists {
		return g.extractSecretValueByField(secretString, field)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(secretString), &data); err == nil {
		defaultFields := []string{"value", "password", "secret", "data"}

		for _, field := range defaultFields {
			if value, ok := data[field]; ok {
				return []byte(fmt.Sprintf("%v", value)), nil
			}
		}
	}

	return []byte(secretString), nil
}

func (g *GCPProvider) extractSecretValueByField(secretString, field string) ([]byte, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(secretString), &data); err == nil {
		if value, ok := data[field]; ok {
			return []byte(fmt.Sprintf("%v", value)), nil
		}
		return nil, fmt.Errorf("field %s not found in secret", field)
	}

	if field != "value" {
		return nil, fmt.Errorf("field %s not found in non-JSON secret", field)
	}

	return []byte(secretString), nil
}

func (g *GCPProvider) SupportsRotation() bool {
	return true
}

func (g *GCPProvider) CheckSecretChanged(ctx context.Context, secretInfo *SecretInfo) (bool, error) {
	if g.client == nil {
		return false, fmt.Errorf("gcp provider not initialized")
	}

	secretVersionName := secretInfo.SecretPath
	if !strings.Contains(secretVersionName, "/versions/") {
		secretVersionName = fmt.Sprintf("%s/versions/latest", secretVersionName)
	}

	secretRequest := &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretVersionName,
	}

	result, err := g.client.AccessSecretVersion(ctx, secretRequest)
	if err != nil {
		return false, fmt.Errorf("failed to access secret version: %w", err)
	}

	if result.Payload == nil || result.Payload.Data == nil {
		return false, fmt.Errorf("secret %s has no payload data", secretVersionName)
	}

	hash := sha256.Sum256(result.Payload.Data)
	currentHash := hex.EncodeToString(hash[:])

	return currentHash != secretInfo.LastHash, nil
}

func (g *GCPProvider) Close() error {
	if g.client != nil {
		return g.client.Close()
	}
	return nil
}
