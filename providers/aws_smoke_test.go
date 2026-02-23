package providers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/docker/go-plugins-helpers/secrets"
)

func TestAWSSecretsManager_Smoke_Localstack(t *testing.T) {

	os.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
	os.Setenv("AWS_ACCESS_KEY_ID", "test")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	os.Setenv("AWS_REGION", "us-east-1")

	provider := &AWSProvider{}
	err := provider.Initialize(map[string]string{
		"AWS_REGION": "us-east-1",
	})
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()

	secretName := fmt.Sprintf("smoke-test-%d", time.Now().UnixNano())
	secretValue := "super-secret-value"

	_, err = provider.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(secretName),
		SecretString: aws.String(secretValue),
	})
	if err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	req := secrets.Request{
		SecretName: secretName,
	}

	value, err := provider.GetSecret(ctx, req)
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	if string(value) != secretValue {
		t.Fatalf("unexpected secret value: got %s", string(value))
	}
}
