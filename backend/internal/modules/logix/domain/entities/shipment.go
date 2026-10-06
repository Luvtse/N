package entities

import (
	"github.com/google/uuid"
	"time"
)

type Shipment struct {
	ID           uuid.UUID      `json:"id" db:"id"`
	ShipperID    uuid.UUID      `json:"shipper_id" db:"shipper_id"`
	ConsigneeID  uuid.UUID      `json:"consignee_id" db:"consignee_id"`
	CarrierID    *uuid.UUID     `json:"carrier_id" db:"carrier_id"`
	ShipmentType ShipmentType   `json:"shipment_type" db:"shipment_type"`
	Status       ShipmentStatus `json:"status" db:"status"`

	// Origin & Destination
	OriginAddress      Address `json:"origin_address" db:"origin_address"`
	DestinationAddress Address `json:"destination_address" db:"destination_address"`
	OriginCountry      string  `json:"origin_country" db:"origin_country"`
	DestinationCountry string  `json:"destination_country" db:"destination_country"`

	// Cargo Details
	CargoDescription string  `json:"cargo_description" db:"cargo_description"`
	HSCode           string  `json:"hs_code" db:"hs_code"` // Harmonized System code for customs
	WeightKg         float64 `json:"weight_kg" db:"weight_kg"`
	VolumeCbm        float64 `json:"volume_cbm" db:"volume_cbm"` // cubic meters
	PackageCount     int     `json:"package_count" db:"package_count"`
	DeclaredValue    float64 `json:"declared_value" db:"declared_value"`
	Currency         string  `json:"currency" db:"currency"`

	// Special Requirements
	RequiresTemperature bool       `json:"requires_temperature" db:"requires_temperature"`
	TemperatureRange    *TempRange `json:"temperature_range" db:"temperature_range"`
	RequiresHazmat      bool       `json:"requires_hazmat" db:"requires_hazmat"`
	HazmatClass         string     `json:"hazmat_class" db:"hazmat_class"`
	RequiresInsurance   bool       `json:"requires_insurance" db:"requires_insurance"`

	// Timeline
	PickupDate     time.Time  `json:"pickup_date" db:"pickup_date"`
	DeliveryDate   time.Time  `json:"delivery_date" db:"delivery_date"`
	ActualPickup   *time.Time `json:"actual_pickup" db:"actual_pickup"`
	ActualDelivery *time.Time `json:"actual_delivery" db:"actual_delivery"`

	// Financials
	FreightCost   float64 `json:"freight_cost" db:"freight_cost"`
	InsuranceCost float64 `json:"insurance_cost" db:"insurance_cost"`
	CustomsDuties float64 `json:"customs_duties" db:"customs_duties"`
	TotalCost     float64 `json:"total_cost" db:"total_cost"`

	// Tracking
	TrackingNumber  string `json:"tracking_number" db:"tracking_number"`
	ContainerNumber string `json:"container_number" db:"container_number"`
	BillOfLading    string `json:"bill_of_lading" db:"bill_of_lading"`

	// Customs
	CustomsStatus        CustomsStatus `json:"customs_status" db:"customs_status"`
	CustomsClearanceDate *time.Time    `json:"customs_clearance_date" db:"customs_clearance_date"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type ShipmentType string

const (
	ShipmentFTL       ShipmentType = "ftl" // Full Truckload
	ShipmentLTL       ShipmentType = "ltl" // Less Than Truckload
	ShipmentContainer ShipmentType = "container"
	ShipmentAir       ShipmentType = "air"
	ShipmentRail      ShipmentType = "rail"
	ShipmentOcean     ShipmentType = "ocean"
)

type ShipmentStatus string

const (
	StatusDraft          ShipmentStatus = "draft"
	StatusQuoteRequested ShipmentStatus = "quote_requested"
	StatusQuoted         ShipmentStatus = "quoted"
	StatusBooked         ShipmentStatus = "booked"
	StatusPickedUp       ShipmentStatus = "picked_up"
	StatusInTransit      ShipmentStatus = "in_transit"
	StatusAtCustoms      ShipmentStatus = "at_customs"
	StatusCustomsCleared ShipmentStatus = "customs_cleared"
	StatusOutForDelivery ShipmentStatus = "out_for_delivery"
	StatusDelivered      ShipmentStatus = "delivered"
	StatusCancelled      ShipmentStatus = "cancelled"
)

type CustomsStatus string

const (
	CustomsPending     CustomsStatus = "pending"
	CustomsSubmitted   CustomsStatus = "submitted"
	CustomsUnderReview CustomsStatus = "under_review"
	CustomsCleared     CustomsStatus = "cleared"
	CustomsHeld        CustomsStatus = "held"
	CustomsRejected    CustomsStatus = "rejected"
)

type Address struct {
	Street       string  `json:"street"`
	City         string  `json:"city"`
	State        string  `json:"state"`
	PostalCode   string  `json:"postal_code"`
	Country      string  `json:"country"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	ContactName  string  `json:"contact_name"`
	ContactPhone string  `json:"contact_phone"`
}

type TempRange struct {
	MinCelsius float64 `json:"min_celsius"`
	MaxCelsius float64 `json:"max_celsius"`
}

type Load struct {
	ID            uuid.UUID `json:"id" db:"id"`
	ShipperID     uuid.UUID `json:"shipper_id" db:"shipper_id"`
	Origin        Address   `json:"origin" db:"origin"`
	Destination   Address   `json:"destination" db:"destination"`
	WeightKg      float64   `json:"weight_kg" db:"weight_kg"`
	EquipmentType string    `json:"equipment_type" db:"equipment_type"` // dry_van, reefer, flatbed
	PickupDate    time.Time `json:"pickup_date" db:"pickup_date"`
	Rate          float64   `json:"rate" db:"rate"`
	Currency      string    `json:"currency" db:"currency"`
	Status        string    `json:"status" db:"status"` // open, matched, in_transit, delivered
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

type Carrier struct {
	ID              uuid.UUID `json:"id" db:"id"`
	CompanyName     string    `json:"company_name" db:"company_name"`
	MCNumber        string    `json:"mc_number" db:"mc_number"` // Motor Carrier number
	DOTNumber       string    `json:"dot_number" db:"dot_number"`
	FleetSize       int       `json:"fleet_size" db:"fleet_size"`
	EquipmentTypes  []string  `json:"equipment_types" db:"equipment_types"`
	ServiceAreas    []string  `json:"service_areas" db:"service_areas"` // country/region codes
	Rating          float64   `json:"rating" db:"rating"`
	OnTimeRate      float64   `json:"on_time_rate" db:"on_time_rate"`
	InsuranceAmount float64   `json:"insurance_amount" db:"insurance_amount"`
	Status          string    `json:"status" db:"status"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

type TrackingEvent struct {
	ID          uuid.UUID `json:"id" db:"id"`
	ShipmentID  uuid.UUID `json:"shipment_id" db:"shipment_id"`
	EventType   string    `json:"event_type" db:"event_type"`
	Status      string    `json:"status" db:"status"`
	Location    Address   `json:"location" db:"location"`
	Latitude    float64   `json:"latitude" db:"latitude"`
	Longitude   float64   `json:"longitude" db:"longitude"`
	Description string    `json:"description" db:"description"`
	RecordedAt  time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy  string    `json:"recorded_by" db:"recorded_by"` // system, driver, customs
}
