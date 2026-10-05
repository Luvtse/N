package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/logix/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"github.com/google/uuid"
)

type CustomsService struct {
	db           *database.Postgres
	bus          eventbus.EventBus
	customsAPI   CustomsAPI
	tariffDB     TariffDatabase
}

func NewCustomsService(db *database.Postgres, bus eventbus.EventBus, api CustomsAPI, tariff TariffDatabase) *CustomsService {
	return &CustomsService{
		db:         db,
		bus:        bus,
		customsAPI: api,
		tariffDB:   tariff,
	}
}

type CustomsAPI interface {
	SubmitDeclaration(ctx context.Context, declaration *CustomsDeclaration) (string, error)
	GetDeclarationStatus(ctx context.Context, declarationID string) (*CustomsStatus, error)
}

type TariffDatabase interface {
	GetTariffRate(ctx context.Context, hsCode, originCountry, destCountry string) (float64, error)
	GetDutyExemptions(ctx context.Context, hsCode, originCountry, destCountry string) ([]string, error)
}

type CustomsDeclaration struct {
	ID                uuid.UUID           `json:"id"`
	ShipmentID        uuid.UUID           `json:"shipment_id"`
	ImporterID        uuid.UUID           `json:"importer_id"`
	ExporterID        uuid.UUID           `json:"exporter_id"`
	OriginCountry     string              `json:"origin_country"`
	DestinationCountry string             `json:"destination_country"`
	Items             []CustomsItem       `json:"items"`
	TotalValue        float64             `json:"total_value"`
	Currency          string              `json:"currency"`
	Incoterm          string              `json:"incoterm"` // EXW, FOB, CIF, DDP, etc.
	Documents         []CustomsDocument   `json:"documents"`
}

type CustomsItem struct {
	HSCode          string  `json:"hs_code"`
	Description     string  `json:"description"`
	Quantity        int     `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	TotalValue      float64 `json:"total_value"`
	WeightKg        float64 `json:"weight_kg"`
	OriginCountry   string  `json:"origin_country"`
}

type CustomsDocument struct {
	Type        string `json:"type"` // commercial_invoice, packing_list, bill_of_lading, certificate_of_origin
	URL         string `json:"url"`
	IssueDate   time.Time `json:"issue_date"`
}

type CustomsStatus struct {
	DeclarationID  string  `json:"declaration_id"`
	Status         string  `json:"status"`
	DutiesOwed     float64 `json:"duties_owed"`
	TaxesOwed      float64 `json:"taxes_owed"`
	ClearanceDate  *time.Time `json:"clearance_date"`
	HoldReason     string  `json:"hold_reason"`
}

func (s *CustomsService) PrepareDeclaration(ctx context.Context, shipment *entities.Shipment) (*CustomsDeclaration, error) {
	// 1. Validate required data
	if shipment.HSCode == "" {
		return nil, errors.New("HS code is required for customs clearance")
	}
	if shipment.OriginCountry == shipment.DestinationCountry {
		return nil, errors.New("domestic shipments do not require customs")
	}

	// 2. Build declaration
	declaration := &CustomsDeclaration{
		ID:                 uuid.New(),
		ShipmentID:         shipment.ID,
		ImporterID:         shipment.ConsigneeID,
		ExporterID:         shipment.ShipperID,
		OriginCountry:      shipment.OriginCountry,
		DestinationCountry: shipment.DestinationCountry,
		TotalValue:         shipment.DeclaredValue,
		Currency:           shipment.Currency,
		Incoterm:           "CIF", // Default
		Items: []CustomsItem{
			{
				HSCode:        shipment.HSCode,
				Description:   shipment.CargoDescription,
				Quantity:      shipment.PackageCount,
				UnitPrice:     shipment.DeclaredValue / float64(shipment.PackageCount),
				TotalValue:    shipment.DeclaredValue,
				WeightKg:      shipment.WeightKg,
				OriginCountry: shipment.OriginCountry,
			},
		},
	}

	// 3. Calculate duties and taxes
	dutyRate, err := s.tariffDB.GetTariffRate(ctx, shipment.HSCode, shipment.OriginCountry, shipment.DestinationCountry)
	if err != nil {
		return nil, fmt.Errorf("failed to get tariff rate: %w", err)
	}

	duties := shipment.DeclaredValue * (dutyRate / 100.0)
	
	// Check for exemptions
	exemptions, _ := s.tariffDB.GetDutyExemptions(ctx, shipment.HSCode, shipment.OriginCountry, shipment.DestinationCountry)
	if len(exemptions) > 0 {
		// Apply free trade agreement reductions
		duties *= 0.5 // 50% reduction under FTA
	}

	// 4. Save declaration to database
	declarationJSON, _ := json.Marshal(declaration)
	_, err = s.db.Exec(ctx, `
		INSERT INTO customs_declarations (id, shipment_id, declaration_data, calculated_duties, status)
		VALUES ($1, $2, $3, $4, 'prepared')
	`, declaration.ID, shipment.ID, declarationJSON, duties)
	if err != nil {
		return nil, err
	}

	return declaration, nil
}

func (s *CustomsService) SubmitDeclaration(ctx context.Context, declaration *CustomsDeclaration) error {
	// 1. Submit to customs authority API
	declarationID, err := s.customsAPI.SubmitDeclaration(ctx, declaration)
	if err != nil {
		return err
	}

	// 2. Update database
	_, err = s.db.Exec(ctx, `
		UPDATE customs_declarations
		SET external_declaration_id = $1, status = 'submitted', submitted_at = $2
		WHERE id = $3
	`, declarationID, time.Now(), declaration.ID)
	if err != nil {
		return err
	}

	// 3. Update shipment status
	_, err = s.db.Exec(ctx, `
		UPDATE shipments
		SET customs_status = 'submitted', status = 'at_customs'
		WHERE id = $1
	`, declaration.ShipmentID)
	if err != nil {
		return err
	}

	// 4. Publish event
	event := eventbus.Event{
		Type: "logix.customs.submitted",
		Payload: map[string]interface{}{
			"shipment_id":     declaration.ShipmentID,
			"declaration_id":  declaration.ID,
			"external_id":     declarationID,
		},
		Timestamp: time.Now().Unix(),
	}
	s.bus.Publish(ctx, "logix.customs", event)

	return nil
}

func (s *CustomsService) PollDeclarationStatus(ctx context.Context, declarationID uuid.UUID) error {
	// Get external declaration ID
	var externalID string
	var shipmentID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT external_declaration_id, shipment_id
		FROM customs_declarations
		WHERE id = $1
	`, declarationID).Scan(&externalID, &shipmentID)
	if err != nil {
		return err
	}

	// Poll customs API
	status, err := s.customsAPI.GetDeclarationStatus(ctx, externalID)
	if err != nil {
		return err
	}

	// Update local status
	newStatus := mapStatus(status.Status)
	_, err = s.db.Exec(ctx, `
		UPDATE customs_declarations
		SET status = $1, duties_owed = $2, taxes_owed = $3, clearance_date = $4
		WHERE id = $5
	`, newStatus, status.DutiesOwed, status.TaxesOwed, status.ClearanceDate, declarationID)
	if err != nil {
		return err
	}

	// If cleared, update shipment
	if newStatus == entities.CustomsCleared {
		_, err = s.db.Exec(ctx, `
			UPDATE shipments
			SET customs_status = 'cleared', customs_clearance_date = $1, status = 'customs_cleared'
			WHERE id = $2
		`, status.ClearanceDate, shipmentID)
		if err != nil {
			return err
		}

		event := eventbus.Event{
			Type: "logix.customs.cleared",
			Payload: map[string]interface{}{
				"shipment_id":    shipmentID,
				"declaration_id": declarationID,
				"duties_owed":    status.DutiesOwed,
				"taxes_owed":     status.TaxesOwed,
			},
			Timestamp: time.Now().Unix(),
		}
		s.bus.Publish(ctx, "logix.customs", event)
	}

	return nil
}

func mapStatus(apiStatus string) entities.CustomsStatus {
	switch apiStatus {
	case "pending":
		return entities.CustomsPending
	case "under_review":
		return entities.CustomsUnderReview
	case "cleared":
		return entities.CustomsCleared
	case "held":
		return entities.CustomsHeld
	case "rejected":
		return entities.CustomsRejected
	default:
		return entities.CustomsPending
	}
}