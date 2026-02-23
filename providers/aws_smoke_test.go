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

	// Create Secret
	_, err = provider.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(secretName),
		SecretString: aws.String(secretValue),
	})
	if err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	// Get Secret
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

	// Delete Secret (force delete for LocalStack)
	_, err = provider.client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(secretName),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if err != nil {
		t.Fatalf("failed to delete secret: %v", err)
	}

	// Verify Deletion
	_, err = provider.GetSecret(ctx, req)
	if err == nil {
		t.Fatalf("expected error after deletion, but secret was still retrievable")
	}
}
