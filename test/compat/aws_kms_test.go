package compat_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
)

func TestKMSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	plain := []byte("deploy without AWS KMS")
	encrypted, err := client.Encrypt(context.Background(), &kms.EncryptInput{
		KeyId:     aws.String("alias/homeport"),
		Plaintext: plain,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Equal(encrypted.CiphertextBlob, plain) {
		t.Fatal("CiphertextBlob should not equal plaintext")
	}

	decrypted, err := client.Decrypt(context.Background(), &kms.DecryptInput{
		CiphertextBlob: encrypted.CiphertextBlob,
		KeyId:          aws.String("alias/homeport"),
	})
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(decrypted.Plaintext, plain) {
		t.Fatalf("Plaintext = %q, want %q", decrypted.Plaintext, plain)
	}

	mac, err := client.GenerateMac(context.Background(), &kms.GenerateMacInput{
		KeyId:        aws.String("alias/homeport-hmac"),
		Message:      []byte("message"),
		MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
	})
	if err != nil {
		t.Fatalf("GenerateMac() error = %v", err)
	}
	verified, err := client.VerifyMac(context.Background(), &kms.VerifyMacInput{
		KeyId:        aws.String("alias/homeport-hmac"),
		Message:      []byte("message"),
		Mac:          mac.Mac,
		MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
	})
	if err != nil {
		t.Fatalf("VerifyMac() error = %v", err)
	}
	if !verified.MacValid {
		t.Fatal("MacValid = false, want true")
	}
}
