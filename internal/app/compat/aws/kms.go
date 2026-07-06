package aws

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"hash"
	"net/http"
)

type KMSAdapter struct {
	macKey []byte
}

func NewKMSAdapter() *KMSAdapter {
	return &KMSAdapter{macKey: []byte("homeport-vault-transit-compat-key")}
}

func (KMSAdapter) Provider() string { return "aws" }
func (KMSAdapter) Service() string  { return "kms" }
func (KMSAdapter) Routes() []string { return []string{"POST /compat/aws/kms"} }
func (KMSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_KMS":     "http://homeport:8080/api/v1/compat/aws/kms",
		"HOMEPORT_COMPAT_BACKEND":  "vault-transit",
		"HOMEPORT_COMPAT_PROTOCOL": "kms",
	}
}
func (KMSAdapter) ConformanceChecks() []string {
	return []string{"encrypt", "decrypt", "generate-mac", "verify-mac", "sign", "verify"}
}

func (a *KMSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	switch action {
	case "Encrypt":
		plain, err := decodeBlob(body["Plaintext"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		writeJSON(w, http.StatusOK, map[string]any{
			"CiphertextBlob": encodeBlob(append([]byte("homeport:"), plain...)),
			"KeyId":          keyID,
		})
	case "Decrypt":
		ciphertext, err := decodeBlob(body["CiphertextBlob"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		plain := ciphertext
		if len(ciphertext) >= len("homeport:") && string(ciphertext[:len("homeport:")]) == "homeport:" {
			plain = ciphertext[len("homeport:"):]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"Plaintext": encodeBlob(plain),
			"KeyId":     stringValue(body["KeyId"]),
		})
	case "GenerateMac":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		algorithm := kmsAlgorithm(body["MacAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":        stringValue(body["KeyId"]),
			"Mac":          encodeBlob(a.mac(message, algorithm)),
			"MacAlgorithm": algorithm,
		})
	case "VerifyMac":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		macValue, err := decodeBlob(body["Mac"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		algorithm := kmsAlgorithm(body["MacAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":        stringValue(body["KeyId"]),
			"MacValid":     hmac.Equal(macValue, a.mac(message, algorithm)),
			"MacAlgorithm": algorithm,
		})
	case "Sign":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		algorithm := kmsAlgorithm(body["SigningAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":            stringValue(body["KeyId"]),
			"Signature":        encodeBlob(a.mac(message, algorithm)),
			"SigningAlgorithm": algorithm,
		})
	case "Verify":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		signature, err := decodeBlob(body["Signature"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		algorithm := kmsAlgorithm(body["SigningAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":            stringValue(body["KeyId"]),
			"SignatureValid":   hmac.Equal(signature, a.mac(message, algorithm)),
			"SigningAlgorithm": algorithm,
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported KMS action: " + action})
	}
}

func (a *KMSAdapter) mac(message []byte, algorithm string) []byte {
	h := hmac.New(kmsHash(algorithm), a.macKey)
	_, _ = h.Write(message)
	return h.Sum(nil)
}

func kmsHash(algorithm string) func() hash.Hash {
	switch algorithm {
	case "HMAC_SHA_384", "RSASSA_PSS_SHA_384", "RSASSA_PKCS1_V1_5_SHA_384", "ECDSA_SHA_384":
		return sha512.New384
	case "HMAC_SHA_512", "RSASSA_PSS_SHA_512", "RSASSA_PKCS1_V1_5_SHA_512", "ECDSA_SHA_512":
		return sha512.New
	default:
		return sha256.New
	}
}

func kmsAlgorithm(value any) string {
	if algorithm := stringValue(value); algorithm != "" {
		return algorithm
	}
	return "HMAC_SHA_256"
}

func decodeBlob(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return base64.StdEncoding.DecodeString(v)
	default:
		return nil, base64.CorruptInputError(0)
	}
}

func encodeBlob(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}
