package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/google/uuid"
)

var (
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
)

// EnvelopeEncryption implements AWS KMS envelope encryption pattern
// Each record gets its own unique data key, encrypted by KMS master key
type EnvelopeEncryption struct {
	kmsClient *kms.Client
	keyID     string
	cache     *DataKeyCache
}

type EncryptedData struct {
	EncryptedDataKey []byte `json:"encrypted_data_key"`
	Ciphertext       []byte `json:"ciphertext"`
	IV               []byte `json:"iv"`
	KeyID            string `json:"key_id"`
	Algorithm        string `json:"algorithm"`
	Version          int    `json:"version"`
}

type DataKeyCache struct {
	keys map[string]*CachedDataKey
}

type CachedDataKey struct {
	Plaintext  []byte
	ExpiresAt  time.Time
}

func NewEnvelopeEncryption(kmsClient *kms.Client, keyID string) *EnvelopeEncryption {
	return &EnvelopeEncryption{
		kmsClient: kmsClient,
		keyID:     keyID,
		cache:     &DataKeyCache{keys: make(map[string]*CachedDataKey)},
	}
}

// Encrypt encrypts data using envelope encryption
func (e *EnvelopeEncryption) Encrypt(ctx context.Context, plaintext []byte) (*EncryptedData, error) {
	// 1. Generate data key via KMS
	output, err := e.kmsClient.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   &e.keyID,
		KeySpec: "AES_256",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	// 2. Encrypt plaintext with data key using AES-256-GCM
	block, err := aes.NewCipher(output.Plaintext)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, nil)

	// 3. Clear plaintext data key from memory
	for i := range output.Plaintext {
		output.Plaintext[i] = 0
	}

	return &EncryptedData{
		EncryptedDataKey: output.CiphertextBlob,
		Ciphertext:       ciphertext,
		IV:               iv,
		KeyID:            e.keyID,
		Algorithm:        "AES-256-GCM",
		Version:          1,
	}, nil
}

// Decrypt decrypts data encrypted with envelope encryption
func (e *EnvelopeEncryption) Decrypt(ctx context.Context, data *EncryptedData) ([]byte, error) {
	// 1. Decrypt data key via KMS
	output, err := e.kmsClient.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: data.EncryptedDataKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	// 2. Decrypt ciphertext with data key
	block, err := aes.NewCipher(output.Plaintext)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, data.IV, data.Ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	// 3. Clear plaintext data key
	for i := range output.Plaintext {
		output.Plaintext[i] = 0
	}

	return plaintext, nil
}

// FieldLevelEncryption handles PII field-level encryption
type FieldLevelEncryption struct {
	envelope *EnvelopeEncryption
}

func NewFieldLevelEncryption(envelope *EnvelopeEncryption) *FieldLevelEncryption {
	return &FieldLevelEncryption{envelope: envelope}
}

// EncryptPII encrypts specific PII fields in a struct
func (f *FieldLevelEncryption) EncryptPII(ctx context.Context, data map[string]interface{}, piiFields []string) (map[string]interface{}, error) {
	encrypted := make(map[string]interface{})
	
	for key, value := range data {
		if contains(piiFields, key) {
			// Convert to bytes and encrypt
			valueBytes := []byte(fmt.Sprintf("%v", value))
			enc, err := f.envelope.Encrypt(ctx, valueBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt field %s: %w", key, err)
			}
			encrypted[key] = enc
		} else {
			encrypted[key] = value
		}
	}
	
	return encrypted, nil
}

// DecryptPII decrypts PII fields
func (f *FieldLevelEncryption) DecryptPII(ctx context.Context, data map[string]interface{}, piiFields []string) (map[string]interface{}, error) {
	decrypted := make(map[string]interface{})
	
	for key, value := range data {
		if contains(piiFields, key) {
			if enc, ok := value.(*EncryptedData); ok {
				plaintext, err := f.envelope.Decrypt(ctx, enc)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt field %s: %w", key, err)
				}
				decrypted[key] = string(plaintext)
			}
		} else {
			decrypted[key] = value
		}
	}
	
	return decrypted, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}