package compat_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
)

func TestSecretsManagerCompatibilityAdapterWithAWSSDK(t *testing.T) {
	adapter := compataws.NewSecretsAdapter()
	adapter.PutSecret("app/db", "postgres://user:pass@db/app")
	server := httptest.NewServer(adapter)
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	got, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("GetSecretValue() error = %v", err)
	}
	if *got.SecretString != "postgres://user:pass@db/app" {
		t.Fatalf("SecretString = %q", *got.SecretString)
	}

	desc, err := client.DescribeSecret(context.Background(), &secretsmanager.DescribeSecretInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("DescribeSecret() error = %v", err)
	}
	if *desc.Name != "app/db" {
		t.Fatalf("DescribeSecret name = %q", *desc.Name)
	}

	list, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{})
	if err != nil {
		t.Fatalf("ListSecrets() error = %v", err)
	}
	if len(list.SecretList) != 1 || *list.SecretList[0].Name != "app/db" {
		t.Fatalf("ListSecrets() = %#v", list.SecretList)
	}
}
