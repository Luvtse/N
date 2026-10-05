package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DID (Decentralized Identifier) implementation
// Based on W3C DID Core specification

type DIDService struct {
	documentStore DIDDocumentStore
	verifiableCredentialStore VCStore
}

type DIDDocument struct {
	Context        []string               `json:"@context"`
	ID             string                 `json:"id"`
	Controller     []string               `json:"controller,omitempty"`
	VerificationMethod []VerificationMethod `json:"verificationMethod"`
	Authentication []string               `json:"authentication"`
	AssertionMethod []string              `json:"assertionMethod"`
	KeyAgreement   []string               `json:"keyAgreement,omitempty"`
	Service        []ServiceEndpoint      `json:"service,omitempty"`
	Created        time.Time              `json:"created"`
	Updated        time.Time              `json:"updated"`
}

type VerificationMethod struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Controller         string `json:"controller"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
}

type ServiceEndpoint struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

type VerifiableCredential struct {
	Context           []string               `json:"@context"`
	ID                string                 `json:"id"`
	Type              []string               `json:"type"`
	Issuer            string                 `json:"issuer"`
	IssuanceDate      time.Time              `json:"issuanceDate"`
	ExpirationDate    *time.Time             `json:"expirationDate,omitempty"`
	CredentialSubject map[string]interface{} `json:"credentialSubject"`
	Proof             *Proof                 `json:"proof,omitempty"`
}

type VerifiablePresentation struct {
	Context              []string               `json:"@context"`
	Type                 []string               `json:"type"`
	VerifiableCredential []VerifiableCredential `json:"verifiableCredential"`
	Holder               string                 `json:"holder"`
	Proof                *Proof                 `json:"proof,omitempty"`
}

type Proof struct {
	Type               string    `json:"type"`
	Created            time.Time `json:"created"`
	VerificationMethod string    `json:"verificationMethod"`
	ProofPurpose       string    `json:"proofPurpose"`
	ProofValue         string    `json:"proofValue"`
}

func NewDIDService(docStore DIDDocumentStore, vcStore VCStore) *DIDService {
	return &DIDService{
		documentStore:           docStore,
		verifiableCredentialStore: vcStore,
	}
}

// CreateDID creates a new decentralized identifier
func (s *DIDService) CreateDID(ctx context.Context, userID string) (*DIDDocument, ed25519.PrivateKey, error) {
	// Generate Ed25519 key pair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	
	// Create DID (did:nidaw:uuid format)
	did := fmt.Sprintf("did:nidaw:%s", uuid.New().String())
	keyID := fmt.Sprintf("%s#key-1", did)
	
	// Encode public key in multibase format
	pubKeyEncoded := "z" + base64.RawURLEncoding.EncodeToString(publicKey)
	
	doc := &DIDDocument{
		Context: []string{
			"https://www.w3.org/ns/did/v1",
			"https://w3id.org/security/suites/ed25519-2020/v1",
		},
		ID: did,
		VerificationMethod: []VerificationMethod{
			{
				ID:                 keyID,
				Type:               "Ed25519VerificationKey2020",
				Controller:         did,
				PublicKeyMultibase: pubKeyEncoded,
			},
		},
		Authentication:  []string{keyID},
		AssertionMethod: []string{keyID},
		Service: []ServiceEndpoint{
			{
				ID:              fmt.Sprintf("%s#nidaw-service", did),
				Type:            "NIDAWService",
				ServiceEndpoint: fmt.Sprintf("https://api.nidaw.com/users/%s", userID),
			},
		},
		Created: time.Now(),
		Updated: time.Now(),
	}
	
	// Store document
	if err := s.documentStore.Save(ctx, doc); err != nil {
		return nil, nil, err
	}
	
	return doc, privateKey, nil
}

// ResolveDID resolves a DID to its document
func (s *DIDService) ResolveDID(ctx context.Context, did string) (*DIDDocument, error) {
	return s.documentStore.Get(ctx, did)
}

// IssueVerifiableCredential issues a VC (e.g., driver license, KYC)
func (s *DIDService) IssueVerifiableCredential(
	ctx context.Context,
	subjectDID string,
	issuerDID string,
	issuerPrivateKey ed25519.PrivateKey,
	credentialType string,
	subjectData map[string]interface{},
	expiration *time.Time,
) (*VerifiableCredential, error) {
	vc := &VerifiableCredential{
		Context: []string{
			"https://www.w3.org/2018/credentials/v1",
			"https://www.w3.org/2018/credentials/examples/v1",
		},
		ID:           fmt.Sprintf("urn:uuid:%s", uuid.New().String()),
		Type:         []string{"VerifiableCredential", credentialType},
		Issuer:       issuerDID,
		IssuanceDate: time.Now(),
		ExpirationDate: expiration,
		CredentialSubject: map[string]interface{}{
			"id": subjectDID,
		},
	}
	
	// Merge subject data
	for k, v := range subjectData {
		vc.CredentialSubject[k] = v
	}
	
	// Sign the credential
	proof, err := s.signCredential(vc, issuerDID, issuerPrivateKey)
	if err != nil {
		return nil, err
	}
	vc.Proof = proof
	
	// Store VC
	if err := s.verifiableCredentialStore.Save(ctx, vc); err != nil {
		return nil, err
	}
	
	return vc, nil
}

// VerifyVerifiableCredential verifies a VC's signature and validity
func (s *DIDService) VerifyVerifiableCredential(ctx context.Context, vc *VerifiableCredential) (bool, error) {
	if vc.Proof == nil {
		return false, errors.New("no proof found")
	}
	
	// Check expiration
	if vc.ExpirationDate != nil && time.Now().After(*vc.ExpirationDate) {
		return false, errors.New("credential expired")
	}
	
	// Resolve issuer DID
	issuerDoc, err := s.ResolveDID(ctx, vc.Issuer)
	if err != nil {
		return false, fmt.Errorf("failed to resolve issuer DID: %w", err)
	}
	
	// Find verification method
	var verificationMethod *VerificationMethod
	for i := range issuerDoc.VerificationMethod {
		if issuerDoc.VerificationMethod[i].ID == vc.Proof.VerificationMethod {
			verificationMethod = &issuerDoc.VerificationMethod[i]
			break
		}
	}
	
	if verificationMethod == nil {
		return false, errors.New("verification method not found")
	}
	
	// Decode public key
	pubKeyBytes, err := base64.RawURLEncoding.DecodeString(verificationMethod.PublicKeyMultibase[1:])
	if err != nil {
		return false, err
	}
	
	publicKey := ed25519.PublicKey(pubKeyBytes)
	
	// Verify signature
	message, err := json.Marshal(vc)
	if err != nil {
		return false, err
	}
	
	signature, err := base64.StdEncoding.DecodeString(vc.Proof.ProofValue)
	if err != nil {
		return false, err
	}
	
	return ed25519.Verify(publicKey, message, signature), nil
}

// CreateVerifiablePresentation creates a VP from multiple VCs
func (s *DIDService) CreateVerifiablePresentation(
	ctx context.Context,
	holderDID string,
	holderPrivateKey ed25519.PrivateKey,
	credentials []VerifiableCredential,
) (*VerifiablePresentation, error) {
	vp := &VerifiablePresentation{
		Context:              []string{"https://www.w3.org/2018/credentials/v1"},
		Type:                 []string{"VerifiablePresentation"},
		VerifiableCredential: credentials,
		Holder:               holderDID,
	}
	
	// Sign the presentation
	proof, err := s.signPresentation(vp, holderDID, holderPrivateKey)
	if err != nil {
		return nil, err
	}
	vp.Proof = proof
	
	return vp, nil
}

func (s *DIDService) signCredential(vc *VerifiableCredential, issuerDID string, privateKey ed25519.PrivateKey) (*Proof, error) {
	// Create proof without signature
	proof := &Proof{
		Type:               "Ed25519Signature2020",
		Created:            time.Now(),
		VerificationMethod: fmt.Sprintf("%s#key-1", issuerDID),
		ProofPurpose:       "assertionMethod",
	}
	
	// Sign
	message, err := json.Marshal(vc)
	if err != nil {
		return nil, err
	}
	
	signature := ed25519.Sign(privateKey, message)
	proof.ProofValue = base64.StdEncoding.EncodeToString(signature)
	
	return proof, nil
}

func (s *DIDService) signPresentation(vp *VerifiablePresentation, holderDID string, privateKey ed25519.PrivateKey) (*Proof, error) {
	proof := &Proof{
		Type:               "Ed25519Signature2020",
		Created:            time.Now(),
		VerificationMethod: fmt.Sprintf("%s#key-1", holderDID),
		ProofPurpose:       "authentication",
	}
	
	message, err := json.Marshal(vp)
	if err != nil {
		return nil, err
	}
	
	signature := ed25519.Sign(privateKey, message)
	proof.ProofValue = base64.StdEncoding.EncodeToString(signature)
	
	return proof, nil
}

// Interfaces
type DIDDocumentStore interface {
	Save(ctx context.Context, doc *DIDDocument) error
	Get(ctx context.Context, did string) (*DIDDocument, error)
	Update(ctx context.Context, doc *DIDDocument) error
	Delete(ctx context.Context, did string) error
}

type VCStore interface {
	Save(ctx context.Context, vc *VerifiableCredential) error
	Get(ctx context.Context, id string) (*VerifiableCredential, error)
	GetBySubject(ctx context.Context, subjectDID string) ([]*VerifiableCredential, error)
	Revoke(ctx context.Context, id string) error
}