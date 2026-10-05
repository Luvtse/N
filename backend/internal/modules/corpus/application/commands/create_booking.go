package commands

import (
	"context"
	"errors"
	"time"

	"nidaw-backend/internal/modules/corpus/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"github.com/google/uuid"
)

type CreateCorporateBookingCommand struct {
	EmployeeID    uuid.UUID
	CorporateID   uuid.UUID
	ServiceType   entities.ServiceType
	BookingData   map[string]interface{}
	PaymentMethod string // "corporate_account", "personal_reimbursement", "split"
}

type CorporateBookingResult struct {
	BookingID       uuid.UUID
	Status          string
	PolicyCompliant bool
	Violations      []string
	RequiresApproval bool
	ApproverID      *uuid.UUID
	EstimatedAmount float64
}

type PolicyEngine struct {
	db *database.Postgres
}

func NewPolicyEngine(db *database.Postgres) *PolicyEngine {
	return &PolicyEngine{db: db}
}

func (p *PolicyEngine) EvaluateBooking(ctx context.Context, corpID uuid.UUID, employeeID uuid.UUID, serviceType entities.ServiceType, bookingData map[string]interface{}) (*entities.TravelPolicy, []string, error) {
	// Load active policy
	var policy entities.TravelPolicy
	err := p.db.QueryRow(ctx, `
		SELECT id, corporate_id, name, version, ride_policy, hotel_policy, food_policy, freight_policy, approval_rules, is_active
		FROM travel_policies
		WHERE corporate_id = $1 AND is_active = true
		ORDER BY effective_from DESC
		LIMIT 1
	`, corpID).Scan(&policy.ID, &policy.CorporateID, &policy.Name, &policy.Version,
		&policy.RidePolicy, &policy.HotelPolicy, &policy.FoodPolicy, &policy.FreightPolicy,
		&policy.ApprovalRules, &policy.IsActive)
	
	if err != nil {
		return nil, nil, err
	}

	violations := []string{}

	switch serviceType {
	case entities.ServiceRides:
		violations = p.evaluateRidePolicy(&policy.RidePolicy, bookingData)
	case entities.ServiceHotels:
		violations = p.evaluateHotelPolicy(&policy.HotelPolicy, bookingData)
	case entities.ServiceFood:
		violations = p.evaluateFoodPolicy(&policy.FoodPolicy, bookingData)
	case entities.ServiceFreight:
		violations = p.evaluateFreightPolicy(&policy.FreightPolicy, bookingData)
	}

	return &policy, violations, nil
}

func (p *PolicyEngine) evaluateRidePolicy(policy *entities.RidePolicy, data map[string]interface{}) []string {
	violations := []string{}
	
	fare, _ := data["fare"].(float64)
	rideType, _ := data["ride_type"].(string)
	requestedAt, _ := data["requested_at"].(time.Time)

	if fare > policy.MaxFarePerRide {
		violations = append(violations, "Fare exceeds maximum allowed per ride")
	}

	allowed := false
	for _, t := range policy.AllowedRideTypes {
		if t == rideType {
			allowed = true
			break
		}
	}
	if !allowed {
		violations = append(violations, "Ride type not allowed by policy")
	}

	for _, hour := range policy.BlockedHours {
		if requestedAt.Hour() == hour {
			violations = append(violations, "Rides not allowed during this hour")
			break
		}
	}

	if policy.WeekendRestriction {
		weekday := requestedAt.Weekday()
		if weekday == time.Saturday || weekday == time.Sunday {
			violations = append(violations, "Rides not allowed on weekends")
		}
	}

	return violations
}

func (p *PolicyEngine) evaluateHotelPolicy(policy *entities.HotelPolicy, data map[string]interface{}) []string {
	violations := []string{}
	
	pricePerNight, _ := data["price_per_night"].(float64)
	starRating, _ := data["star_rating"].(int)
	chain, _ := data["chain"].(string)

	if pricePerNight > policy.MaxPricePerNight {
		violations = append(violations, "Price per night exceeds policy limit")
	}
	if starRating > policy.MaxStarRating {
		violations = append(violations, "Hotel star rating exceeds policy limit")
	}
	for _, blocked := range policy.BlockedChains {
		if blocked == chain {
			violations = append(violations, "Hotel chain is blocked by policy")
			break
		}
	}

	return violations
}

func (p *PolicyEngine) evaluateFoodPolicy(policy *entities.FoodPolicy, data map[string]interface{}) []string {
	violations := []string{}
	
	amount, _ := data["amount"].(float64)
	restaurantID, _ := data["restaurant_id"].(uuid.UUID)

	if amount > policy.MaxOrderAmount {
		violations = append(violations, "Order amount exceeds policy limit")
	}
	for _, blocked := range policy.BlockedRestaurants {
		if blocked == restaurantID {
			violations = append(violations, "Restaurant is blocked by policy")
			break
		}
	}

	return violations
}

func (p *PolicyEngine) evaluateFreightPolicy(policy *entities.FreightPolicy, data map[string]interface{}) []string {
	violations := []string{}
	
	value, _ := data["shipment_value"].(float64)
	carrierID, _ := data["carrier_id"].(uuid.UUID)

	if value > policy.MaxShipmentValue {
		violations = append(violations, "Shipment value exceeds policy limit")
	}
	if len(policy.AllowedCarriers) > 0 {
		allowed := false
		for _, c := range policy.AllowedCarriers {
			if c == carrierID {
				allowed = true
				break
			}
		}
		if !allowed {
			violations = append(violations, "Carrier not in approved list")
		}
	}

	return violations
}

type CreateCorporateBookingHandler struct {
	db           *database.Postgres
	bus          eventbus.EventBus
	policyEngine *PolicyEngine
}

func NewCreateCorporateBookingHandler(db *database.Postgres, bus eventbus.EventBus) *CreateCorporateBookingHandler {
	return &CreateCorporateBookingHandler{
		db:           db,
		bus:          bus,
		policyEngine: NewPolicyEngine(db),
	}
}

func (h *CreateCorporateBookingHandler) Execute(ctx context.Context, cmd *CreateCorporateBookingCommand) (*CorporateBookingResult, error) {
	bookingID := uuid.New()

	// 1. Load employee and verify active status
	var employee entities.Employee
	err := h.db.QueryRow(ctx, `
		SELECT id, corporate_id, user_id, employee_id, department, role, spending_limit, approval_level
		FROM corporate_employees
		WHERE id = $1 AND corporate_id = $2 AND status = 'active'
	`, cmd.EmployeeID, cmd.CorporateID).Scan(
		&employee.ID, &employee.CorporateID, &employee.UserID, &employee.EmployeeID,
		&employee.Department, &employee.Role, &employee.SpendingLimit, &employee.ApprovalLevel,
	)
	if err != nil {
		return nil, errors.New("employee not found or inactive")
	}

	// 2. Evaluate policy compliance
	policy, violations, err := h.policyEngine.EvaluateBooking(ctx, cmd.CorporateID, cmd.EmployeeID, cmd.ServiceType, cmd.BookingData)
	if err != nil {
		return nil, err
	}

	// 3. Determine if approval is required
	requiresApproval := len(violations) > 0
	var approverID *uuid.UUID

	estimatedAmount, _ := cmd.BookingData["amount"].(float64)
	for _, rule := range policy.ApprovalRules {
		if rule.ConditionType == "amount" && estimatedAmount > rule.AutoApproveBelow {
			requiresApproval = true
			// Find approver at required level
			var approver uuid.UUID
			err := h.db.QueryRow(ctx, `
				SELECT id FROM corporate_employees
				WHERE corporate_id = $1 AND manager_id IS NOT NULL AND approval_level >= $2
				LIMIT 1
			`, cmd.CorporateID, rule.ApproverLevel).Scan(&approver)
			if err == nil {
				approverID = &approver
			}
			break
		}
	}

	// 4. Check employee spending limit
	if estimatedAmount > employee.SpendingLimit {
		requiresApproval = true
	}

	// 5. Create booking record
	status := "confirmed"
	if requiresApproval {
		status = "pending_approval"
	}

	_, err = h.db.Exec(ctx, `
		INSERT INTO corporate_bookings (id, employee_id, corporate_id, service_type, booking_data, 
			payment_method, status, policy_compliant, violations, estimated_amount, requires_approval, approver_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, bookingID, cmd.EmployeeID, cmd.CorporateID, cmd.ServiceType, cmd.BookingData,
		cmd.PaymentMethod, status, len(violations) == 0, violations, estimatedAmount,
		requiresApproval, approverID)
	if err != nil {
		return nil, err
	}

	// 6. Publish event
	event := eventbus.Event{
		Type: "corpus.booking.created",
		Payload: map[string]interface{}{
			"booking_id":        bookingID,
			"employee_id":       cmd.EmployeeID,
			"corporate_id":      cmd.CorporateID,
			"service_type":      cmd.ServiceType,
			"status":            status,
			"policy_compliant":  len(violations) == 0,
			"estimated_amount":  estimatedAmount,
		},
		Timestamp: time.Now().Unix(),
	}
	h.bus.Publish(ctx, "corpus.bookings", event)

	// 7. If auto-approved, forward to actual service (Nidus/Haven/Vorax/Logix)
	if !requiresApproval {
		h.forwardToService(ctx, cmd.ServiceType, bookingID, cmd.BookingData)
	}

	return &CorporateBookingResult{
		BookingID:        bookingID,
		Status:           status,
		PolicyCompliant:  len(violations) == 0,
		Violations:       violations,
		RequiresApproval: requiresApproval,
		ApproverID:       approverID,
		EstimatedAmount:  estimatedAmount,
	}, nil
}

func (h *CreateCorporateBookingHandler) forwardToService(ctx context.Context, serviceType entities.ServiceType, bookingID uuid.UUID, data map[string]interface{}) {
	// Publish event for downstream service to handle
	var topic string
	switch serviceType {
	case entities.ServiceRides:
		topic = "nidus.rides"
	case entities.ServiceHotels:
		topic = "haven.bookings"
	case entities.ServiceFood:
		topic = "vorax.orders"
	case entities.ServiceFreight:
		topic = "logix.shipments"
	}

	event := eventbus.Event{
		Type: "corpus.booking.approved",
		Payload: map[string]interface{}{
			"corporate_booking_id": bookingID,
			"booking_data":         data,
		},
		Timestamp: time.Now().Unix(),
	}
	h.bus.Publish(ctx, topic, event)
}