package notifications

// NotificationConfig carries provider credentials/flags for the
// MultiProviderNotificationService. Empty fields disable that provider.
type NotificationConfig struct {
	// Twilio (primary SMS)
	TwilioAccountSID string
	TwilioAuthToken  string
	TwilioFromNumber string

	// SendGrid (primary email)
	SendGridAPIKey string

	// AWS SNS/SES (fallbacks). Setting the region enables both.
	AWSRegion string
}
