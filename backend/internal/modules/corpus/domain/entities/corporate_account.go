package entities

import (
	"time"
	"github.com/google/uuid"
)

type CorporateAccount struct {
	ID              uuid.UUID         `json:"id" db:"id"`
	CompanyName     string            `json:"company_name" db:"company_name"`
	TaxID           string            `json:"tax_id" db:"tax_id"`
	BillingEmail    string            `json:"billing_email" db:"billing_email"`
	AccountManager  string            `json:"account_manager" db:"account_manager"`
	Tier            AccountTier       `json:"tier" db:"tier"`
	CreditLimit     float64           `json:"credit_limit" db:"credit_limit"`
	CurrentBalance  float64           `json:"current_balance" db:"current_balance"`
	Currency        string            `json:"currency" db:"currency"`
	EmployeeCount   int               `json:"employee_count" db:"employee_count"`
	EnabledServices []ServiceType     `json:"enabled_services" db:"enabled_services"`
	Status          AccountStatus     `json:"status" db:"status"`
	CreatedAt       time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at" db:"updated_at"`
}

type AccountTier string
const (
	TierStartup  AccountTier = "startup"
	TierBusiness AccountTier = "business"
	TierEnterprise AccountTier = "enterprise"
)

type ServiceType string
const (
	ServiceRides   ServiceType = "rides"
	ServiceHotels  ServiceType = "hotels"
	ServiceFood    ServiceType = "food"
	ServiceFreight ServiceType = "freight"
)

type AccountStatus string
const (
	StatusActive    AccountStatus = "active"
	StatusSuspended AccountStatus = "suspended"
	StatusPending   AccountStatus = "pending"
)

type Employee struct {
	ID             uuid.UUID   `json:"id" db:"id"`
	CorporateID    uuid.UUID   `json:"corporate_id" db:"corporate_id"`
	UserID         uuid.UUID   `json:"user_id" db:"user_id"`
	EmployeeID     string      `json:"employee_id" db:"employee_id"` // HR system ID
	Department     string      `json:"department" db:"department"`
	Role           string      `json:"role" db:"role"`
	ManagerID      *uuid.UUID  `json:"manager_id" db:"manager_id"`
	SpendingLimit  float64     `json:"spending_limit" db:"spending_limit"`
	ApprovalLevel  int         `json:"approval_level" db:"approval_level"`
	Status         string      `json:"status" db:"status"`
	CreatedAt      time.Time   `json:"created_at" db:"created_at"`
}

type TravelPolicy struct {
	ID              uuid.UUID         `json:"id" db:"id"`
	CorporateID     uuid.UUID         `json:"corporate_id" db:"corporate_id"`
	Name            string            `json:"name" db:"name"`
	Version         int               `json:"version" db:"version"`
	RidePolicy      RidePolicy        `json:"ride_policy" db:"ride_policy"`
	HotelPolicy     HotelPolicy       `json:"hotel_policy" db:"hotel_policy"`
	FoodPolicy      FoodPolicy        `json:"food_policy" db:"food_policy"`
	FreightPolicy   FreightPolicy     `json:"freight_policy" db:"freight_policy"`
	ApprovalRules   []ApprovalRule    `json:"approval_rules" db:"approval_rules"`
	IsActive        bool              `json:"is_active" db:"is_active"`
	EffectiveFrom   time.Time         `json:"effective_from" db:"effective_from"`
	CreatedAt       time.Time         `json:"created_at" db:"created_at"`
}

type RidePolicy struct {
	MaxFarePerRide      float64   `json:"max_fare_per_ride"`
	AllowedRideTypes    []string  `json:"allowed_ride_types"`
	BlockedHours        []int     `json:"blocked_hours"` // e.g., [22, 23, 0, 1, 2, 3, 4]
	WeekendRestriction  bool      `json:"weekend_restriction"`
	RequiresApproval    float64   `json:"requires_approval_above"` // amount threshold
}

type HotelPolicy struct {
	MaxPricePerNight    float64   `json:"max_price_per_night"`
	MaxStarRating       int       `json:"max_star_rating"`
	AllowedChains       []string  `json:"allowed_chains"`
	BlockedChains       []string  `json:"blocked_chains"`
	AdvanceBookingDays  int       `json:"advance_booking_days"`
}

type FoodPolicy struct {
	MaxOrderAmount      float64   `json:"max_order_amount"`
	AllowedCuisines     []string  `json:"allowed_cuisines"`
	BlockedRestaurants  []uuid.UUID `json:"blocked_restaurants"`
	DailyLimit          float64   `json:"daily_limit"`
}

type FreightPolicy struct {
	MaxShipmentValue    float64   `json:"max_shipment_value"`
	AllowedCarriers     []uuid.UUID `json:"allowed_carriers"`
	RequiresInsurance   float64   `json:"requires_insurance_above"`
	CustomsPreApproval  bool      `json:"customs_pre_approval"`
}

type ApprovalRule struct {
	ID              uuid.UUID   `json:"id"`
	ConditionType   string      `json:"condition_type"` // "amount", "destination", "duration"
	ConditionValue  interface{} `json:"condition_value"`
	ApproverLevel   int         `json:"approver_level"`
	AutoApproveBelow float64    `json:"auto_approve_below"`
}

type ExpenseReport struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	EmployeeID      uuid.UUID       `json:"employee_id" db:"employee_id"`
	CorporateID     uuid.UUID       `json:"corporate_id" db:"corporate_id"`
	Title           string          `json:"title" db:"title"`
	Purpose         string          `json:"purpose" db:"purpose"`
	PeriodStart     time.Time       `json:"period_start" db:"period_start"`
	PeriodEnd       time.Time       `json:"period_end" db:"period_end"`
	Items           []ExpenseItem   `json:"items" db:"items"`
	TotalAmount     float64         `json:"total_amount" db:"total_amount"`
	Currency        string          `json:"currency" db:"currency"`
	Status          string          `json:"status" db:"status"` // draft, submitted, approved, rejected, reimbursed
	ApproverID      *uuid.UUID      `json:"approver_id" db:"approver_id"`
	ApprovalNotes   string          `json:"approval_notes" db:"approval_notes"`
	ReceiptsAttached int            `json:"receipts_attached" db:"receipts_attached"`
	SubmittedAt     *time.Time      `json:"submitted_at" db:"submitted_at"`
	ApprovedAt      *time.Time      `json:"approved_at" db:"approved_at"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

type ExpenseItem struct {
	ID              uuid.UUID   `json:"id"`
	ExpenseReportID uuid.UUID   `json:"expense_report_id"`
	Category        string      `json:"category"` // ride, hotel, food, freight, misc
	Description     string      `json:"description"`
	Amount          float64     `json:"amount"`
	Currency        string      `json:"currency"`
	TransactionDate time.Time   `json:"transaction_date"`
	RelatedBookingID *uuid.UUID `json:"related_booking_id"`
	ReceiptURL      string      `json:"receipt_url"`
	IsPolicyCompliant bool      `json:"is_policy_compliant"`
	PolicyViolation   string    `json:"policy_violation"`
}