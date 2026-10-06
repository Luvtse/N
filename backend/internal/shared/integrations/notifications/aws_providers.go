package notifications

import (
	"context"
	"errors"
)

// ErrAWSProviderNotImplemented signals that the AWS SNS/SES fallbacks are
// declared in configuration but not yet wired. The multi-provider service
// treats a failed provider as "try next", so this fails over safely rather
// than crashing or silently succeeding.
var ErrAWSProviderNotImplemented = errors.New("aws notification provider not implemented")

// SNSProvider is a placeholder for AWS SNS SMS fallback.
type SNSProvider struct {
	region string
}

func NewSNSProvider(region string) *SNSProvider {
	return &SNSProvider{region: region}
}

func (p *SNSProvider) Send(ctx context.Context, req *SMSRequest) error {
	return ErrAWSProviderNotImplemented
}

func (p *SNSProvider) Name() string { return "aws-sns" }

// SESProvider is a placeholder for AWS SES email fallback.
type SESProvider struct {
	region string
}

func NewSESProvider(region string) *SESProvider {
	return &SESProvider{region: region}
}

func (p *SESProvider) Send(ctx context.Context, req *EmailRequest) error {
	return ErrAWSProviderNotImplemented
}

func (p *SESProvider) Name() string { return "aws-ses" }
