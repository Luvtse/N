package services

import (
"context"
"errors"
"fmt"
"time"

"nidaw-backend/internal/shared/eventbus"
"nidaw-backend/internal/shared/integrations/payments"
)

// PaymentService is the thin application-level wrapper around the pluggable
// payments.PaymentGateway interface. Provider SDKs are never touched here;
// rail selection happens in payments.NewGateway (telebirr | chapa | mpesa).
// Money convention: all gateway amounts are ETB minor units (santim, 2 dp).
type PaymentService struct {
gateway payments.PaymentGateway
bus     eventbus.EventBus
}

func NewPaymentService(gateway payments.PaymentGateway, bus eventbus.EventBus) *PaymentService {
return &PaymentService{
gateway: gateway,
bus:     bus,
}
}

type PaymentRequest struct {
UserID    string
Phone     string // required for STK-push rails (mpesa/telebirr)
Reference string // idempotency key; generated when empty
// Amount in major ETB units; converted to santim at the gateway boundary.
Amount      float64
Currency    string
Description string
ReturnURL   string
CallbackURL string
Metadata    map[string]string
}

type PaymentResponse struct {
PaymentID   string // our reference (reconciliation key)
Status      string
CheckoutURL string // hosted checkout (chapa)
DeepLinkURL string // telebirr app deep link
QRCodeURL   string // telebirr QR
}

var (
ErrInvalidPaymentRequest = errors.New("invalid payment request")
)

func (p *PaymentService) ProcessPayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
if req == nil {
return nil, fmt.Errorf("%w: request is nil", ErrInvalidPaymentRequest)
}
if req.UserID == "" {
return nil, fmt.Errorf("%w: user id is required", ErrInvalidPaymentRequest)
}
if req.Amount <= 0 {
return nil, fmt.Errorf("%w: amount must be positive", ErrInvalidPaymentRequest)
}
if p.gateway == nil {
return nil, errors.New("payment gateway not configured")
}
ref := req.Reference
if ref == "" {
ref = fmt.Sprintf("pay_%d_%s", time.Now().UnixNano(), req.UserID)
}

gwReq := &payments.CheckoutRequest{
AmountETBSantim: int64(req.Amount*100 + 0.5), // convert to santim
Currency:        orDefaultStr(req.Currency, "ETB"),
Phone:           req.Phone,
Reference:       ref,
Description:     req.Description,
ReturnURL:       req.ReturnURL,
CallbackURL:     req.CallbackURL,
Metadata:        req.Metadata,
}
if gwReq.Metadata == nil {
gwReq.Metadata = map[string]string{}
}
gwReq.Metadata["user_id"] = req.UserID

checkout, err := p.gateway.CreateCheckout(ctx, gwReq)
if err != nil {
return nil, err
}

event := eventbus.Event{
Type: "payment.initiated",
Payload: map[string]interface{}{
"reference": checkout.Reference,
"user_id":   req.UserID,
"amount":    req.Amount,
"currency":  gwReq.Currency,
"status":    checkout.Status,
"rail":      p.gateway.Name(),
},
Timestamp: time.Now().Unix(),
}
if err := p.bus.Publish(ctx, "payments", event); err != nil {
// Checkout itself succeeded at the rail; log loudly but keep data flowing
// back to the caller so reconciliation can recover.
fmt.Printf("Warning: failed to publish payment.initiated event: %v\n", err)
}

return &PaymentResponse{
PaymentID:   checkout.Reference,
Status:      checkout.Status,
CheckoutURL: checkout.CheckoutURL,
DeepLinkURL: checkout.DeepLinkURL,
QRCodeURL:   checkout.QRCodeURL,
}, nil
}

// ConfirmPayment performs the authoritative verification against the rail —
// golden rule: never trust the webhook alone.
func (p *PaymentService) ConfirmPayment(ctx context.Context, reference string) (*payments.PaymentStatus, error) {
if p.gateway == nil {
return nil, errors.New("payment gateway not configured")
}
if reference == "" {
return nil, fmt.Errorf("%w: reference is required", ErrInvalidPaymentRequest)
}
status, err := p.gateway.VerifyPayment(ctx, reference)
if err != nil {
return nil, err
}
if status.Status == payments.StatusSucceeded {
event := eventbus.Event{
Type: "payment.succeeded",
Payload: map[string]interface{}{
"reference":      reference,
"provider_tx_id": status.ProviderTxID,
"amount_santim":  status.AmountSantim,
"rail":           p.gateway.Name(),
},
Timestamp: time.Now().Unix(),
}
if err := p.bus.Publish(ctx, "payments", event); err != nil {
fmt.Printf("Warning: failed to publish payment.succeeded event: %v\n", err)
}
}
return status, nil
}

func (p *PaymentService) RefundPayment(ctx context.Context, reference string, amount float64) error {
if reference == "" {
return fmt.Errorf("%w: payment reference is required", ErrInvalidPaymentRequest)
}
if amount < 0 {
return fmt.Errorf("%w: refund amount cannot be negative", ErrInvalidPaymentRequest)
}
if p.gateway == nil {
return errors.New("payment gateway not configured")
}
// amount == 0 means full refund; gateway handles the semantics.
if err := p.gateway.RefundPayment(ctx, reference, int64(amount*100+0.5)); err != nil {
return err
}
event := eventbus.Event{
Type: "payment.refunded",
Payload: map[string]interface{}{
"reference": reference,
"amount":    amount,
},
Timestamp: time.Now().Unix(),
}
return p.bus.Publish(ctx, "payments", event)
}

func orDefaultStr(v, def string) string {
if v == "" {
return def
}
return v
}
