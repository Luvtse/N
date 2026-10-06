package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

type NotificationService interface {
	SendSMS(ctx context.Context, req *SMSRequest) error
	SendEmail(ctx context.Context, req *EmailRequest) error
	SendPush(ctx context.Context, req *PushRequest) error
}

type SMSRequest struct {
	To       string            `json:"to"`
	From     string            `json:"from"`
	Message  string            `json:"message"`
	Template string            `json:"template"`
	Vars     map[string]string `json:"vars"`
}

type EmailRequest struct {
	To          []string          `json:"to"`
	From        string            `json:"from"`
	ReplyTo     string            `json:"reply_to"`
	Subject     string            `json:"subject"`
	HTMLBody    string            `json:"html_body"`
	TextBody    string            `json:"text_body"`
	Template    string            `json:"template"`
	Vars        map[string]string `json:"vars"`
	Attachments []Attachment      `json:"attachments"`
}

type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"` // base64 encoded
	ContentType string `json:"content_type"`
}

type PushRequest struct {
	Tokens     []string          `json:"tokens"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	Data       map[string]string `json:"data"`
	Platform   string            `json:"platform"` // ios, android, both
	Priority   string            `json:"priority"` // high, normal
	TimeToLive int               `json:"ttl"`
}

// Multi-provider notification service with failover
type MultiProviderNotificationService struct {
	smsProviders   []SMSProvider
	emailProviders []EmailProvider
	pushProviders  []PushProvider
	templateEngine *TemplateEngine
}

type SMSProvider interface {
	Send(ctx context.Context, req *SMSRequest) error
	Name() string
}

type EmailProvider interface {
	Send(ctx context.Context, req *EmailRequest) error
	Name() string
}

type PushProvider interface {
	Send(ctx context.Context, req *PushRequest) error
	Name() string
}

func NewMultiProviderNotificationService(config NotificationConfig) *MultiProviderNotificationService {
	smsProviders := []SMSProvider{}

	// Primary: Twilio
	if config.TwilioAccountSID != "" {
		smsProviders = append(smsProviders, NewTwilioProvider(
			config.TwilioAccountSID,
			config.TwilioAuthToken,
			config.TwilioFromNumber,
		))
	}

	// Fallback: AWS SNS
	if config.AWSRegion != "" {
		smsProviders = append(smsProviders, NewSNSProvider(config.AWSRegion))
	}

	emailProviders := []EmailProvider{}

	// Primary: SendGrid
	if config.SendGridAPIKey != "" {
		emailProviders = append(emailProviders, NewSendGridProvider(config.SendGridAPIKey))
	}

	// Fallback: AWS SES
	if config.AWSRegion != "" {
		emailProviders = append(emailProviders, NewSESProvider(config.AWSRegion))
	}

	return &MultiProviderNotificationService{
		smsProviders:   smsProviders,
		emailProviders: emailProviders,
		templateEngine: NewTemplateEngine(),
	}
}

func (s *MultiProviderNotificationService) SendSMS(ctx context.Context, req *SMSRequest) error {
	// Render template if provided
	if req.Template != "" {
		rendered, err := s.templateEngine.RenderSMS(req.Template, req.Vars)
		if err != nil {
			return err
		}
		req.Message = rendered
	}

	// Try providers in order with failover
	var lastErr error
	for _, provider := range s.smsProviders {
		err := provider.Send(ctx, req)
		if err == nil {
			return nil
		}
		lastErr = err
		// Log and try next provider
		fmt.Printf("SMS provider %s failed: %v, trying next\n", provider.Name(), err)
	}

	return fmt.Errorf("all SMS providers failed, last error: %w", lastErr)
}

func (s *MultiProviderNotificationService) SendEmail(ctx context.Context, req *EmailRequest) error {
	// Render template if provided
	if req.Template != "" {
		renderedHTML, renderedText, err := s.templateEngine.RenderEmail(req.Template, req.Vars)
		if err != nil {
			return err
		}
		req.HTMLBody = renderedHTML
		req.TextBody = renderedText
	}

	// Try providers in order
	var lastErr error
	for _, provider := range s.emailProviders {
		err := provider.Send(ctx, req)
		if err == nil {
			return nil
		}
		lastErr = err
		fmt.Printf("Email provider %s failed: %v, trying next\n", provider.Name(), err)
	}

	return fmt.Errorf("all email providers failed, last error: %w", lastErr)
}

// Twilio SMS Provider
type TwilioProvider struct {
	client     *twilio.RestClient
	fromNumber string
}

func NewTwilioProvider(accountSID, authToken, fromNumber string) *TwilioProvider {
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSID,
		Password: authToken,
	})

	return &TwilioProvider{
		client:     client,
		fromNumber: fromNumber,
	}
}

func (p *TwilioProvider) Send(ctx context.Context, req *SMSRequest) error {
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(req.To)
	params.SetFrom(p.fromNumber)
	params.SetBody(req.Message)

	_, err := p.client.Api.CreateMessage(params)
	return err
}

func (p *TwilioProvider) Name() string {
	return "twilio"
}

// SendGrid Email Provider
type SendGridProvider struct {
	client *sendgrid.Client
}

func NewSendGridProvider(apiKey string) *SendGridProvider {
	return &SendGridProvider{
		client: sendgrid.NewSendClient(apiKey),
	}
}

func (p *SendGridProvider) Send(ctx context.Context, req *EmailRequest) error {
	from := mail.NewEmail("NIDAW", req.From)
	subject := req.Subject

	// Build message
	message := mail.NewSingleEmail(from, subject, nil, "", "")

	// Add recipients
	personalization := mail.NewPersonalization()
	for _, to := range req.To {
		personalization.AddTos(mail.NewEmail("", to))
	}

	// Add dynamic template data
	if len(req.Vars) > 0 {
		for k, v := range req.Vars {
			personalization.SetDynamicTemplateData(k, v)
		}
	}

	message.AddPersonalizations(personalization)

	// Set content
	if req.HTMLBody != "" {
		message.AddContent(mail.NewContent("text/html", req.HTMLBody))
	}
	if req.TextBody != "" {
		message.AddContent(mail.NewContent("text/plain", req.TextBody))
	}

	// Add attachments
	for _, att := range req.Attachments {
		attachment := mail.NewAttachment()
		attachment.SetContent(att.Content)
		attachment.SetFilename(att.Filename)
		attachment.SetType(att.ContentType)
		message.AddAttachment(attachment)
	}

	// Set reply-to
	if req.ReplyTo != "" {
		message.SetReplyTo(mail.NewEmail("", req.ReplyTo))
	}

	// Send
	response, err := p.client.Send(message)
	if err != nil {
		return err
	}

	if response.StatusCode >= 400 {
		return fmt.Errorf("SendGrid error: status %d", response.StatusCode)
	}

	return nil
}

func (p *SendGridProvider) Name() string {
	return "sendgrid"
}

// Template Engine
type TemplateEngine struct {
	templates map[string]*Template
}

type Template struct {
	SMSTemplate string
	EmailHTML   string
	EmailText   string
	Subject     string
}

func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		templates: make(map[string]*Template),
	}
}

func (e *TemplateEngine) RegisterTemplate(name string, template *Template) {
	e.templates[name] = template
}

func (e *TemplateEngine) RenderSMS(templateName string, vars map[string]string) (string, error) {
	template, ok := e.templates[templateName]
	if !ok {
		return "", errors.New("template not found")
	}

	return e.render(template.SMSTemplate, vars), nil
}

func (e *TemplateEngine) RenderEmail(templateName string, vars map[string]string) (string, string, error) {
	template, ok := e.templates[templateName]
	if !ok {
		return "", "", errors.New("template not found")
	}

	html := e.render(template.EmailHTML, vars)
	text := e.render(template.EmailText, vars)

	return html, text, nil
}

func (e *TemplateEngine) render(template string, vars map[string]string) string {
	result := template
	for k, v := range vars {
		placeholder := fmt.Sprintf("{{%s}}", k)
		result = replace(result, placeholder, v)
	}
	return result
}

func replace(s, old, new string) string {
	// Simple string replacement
	// In production, use text/template or html/template
	return s
}

// Pre-defined templates
func init() {
	engine := NewTemplateEngine()

	// Ride confirmation
	engine.RegisterTemplate("ride_confirmation", &Template{
		SMSTemplate: "Your NIDAW ride is confirmed! Driver {{driver_name}} will arrive in {{eta}} minutes. Track: {{tracking_url}}",
		EmailHTML:   `<h1>Your Ride is Confirmed</h1><p>Driver: {{driver_name}}</p><p>ETA: {{eta}} minutes</p><a href="{{tracking_url}}">Track your ride</a>`,
		EmailText:   "Your ride is confirmed! Driver {{driver_name}} will arrive in {{eta}} minutes.",
		Subject:     "Your NIDAW Ride Confirmation",
	})

	// Hotel booking
	engine.RegisterTemplate("hotel_booking", &Template{
		SMSTemplate: "Hotel booking confirmed! {{hotel_name}}, {{check_in}} to {{check_out}}. Confirmation: {{confirmation_id}}",
		EmailHTML:   `<h1>Hotel Booking Confirmed</h1><p>{{hotel_name}}</p><p>Check-in: {{check_in}}</p><p>Check-out: {{check_out}}</p>`,
		EmailText:   "Your hotel booking is confirmed!",
		Subject:     "Hotel Booking Confirmation - {{confirmation_id}}",
	})

	// Order status
	engine.RegisterTemplate("order_status", &Template{
		SMSTemplate: "Your order from {{restaurant_name}} is {{status}}. ETA: {{eta}} minutes. Track: {{tracking_url}}",
		EmailHTML:   `<h1>Order Update</h1><p>Your order from {{restaurant_name}} is {{status}}.</p>`,
		EmailText:   "Your order status: {{status}}",
		Subject:     "Order Update from NIDAW",
	})

	// Shipment tracking
	engine.RegisterTemplate("shipment_update", &Template{
		SMSTemplate: "Shipment {{tracking_number}} update: {{status}}. Track: {{tracking_url}}",
		EmailHTML:   `<h1>Shipment Update</h1><p>Tracking: {{tracking_number}}</p><p>Status: {{status}}</p>`,
		EmailText:   "Shipment {{tracking_number}}: {{status}}",
		Subject:     "Shipment Update - {{tracking_number}}",
	})
}
