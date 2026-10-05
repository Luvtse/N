package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PredictiveBI provides AI-powered business intelligence
type PredictiveBI struct {
	pinotDB   *sql.DB
	warehouse *sql.DB  // Snowflake/BigQuery
	mlService *MLService
}

type DashboardMetric struct {
	Name       string      `json:"name"`
	Value      interface{} `json:"value"`
	Change     float64     `json:"change"`      // % change vs previous period
	Trend      string      `json:"trend"`       // up, down, stable
	Confidence float64     `json:"confidence"`  // prediction confidence
}

type Prediction struct {
	Metric       string                 `json:"metric"`
	Timeframe    string                 `json:"timeframe"`
	Predicted    float64                `json:"predicted"`
	LowerBound   float64                `json:"lower_bound"`
	UpperBound   float64                `json:"upper_bound"`
	Confidence   float64                `json:"confidence"`
	Contributors []PredictionContributor `json:"contributors"`
}

type PredictionContributor struct {
	Factor  string  `json:"factor"`
	Impact  float64 `json:"impact"`
	Weight  float64 `json:"weight"`
}

type Anomaly struct {
	Metric      string    `json:"metric"`
	Value       float64   `json:"value"`
	Expected    float64   `json:"expected"`
	Deviation   float64   `json:"deviation"` // standard deviations
	Severity    string    `json:"severity"`  // low, medium, high, critical
	StartTime   time.Time `json:"start_time"`
	Description string    `json:"description"`
	Recommendation string `json:"recommendation"`
}

func NewPredictiveBI(pinotDB, warehouse *sql.DB, ml *MLService) *PredictiveBI {
	return &PredictiveBI{
		pinotDB:   pinotDB,
		warehouse: warehouse,
		mlService: ml,
	}
}

// GetRealTimeMetrics returns live metrics from Pinot
func (p *PredictiveBI) GetRealTimeMetrics(ctx context.Context, city string) ([]DashboardMetric, error) {
	metrics := []DashboardMetric{}
	
	// Active rides right now
	var activeRides int
	err := p.pinotDB.QueryRowContext(ctx, `
		SELECT COUNT(*) as active_rides
		FROM rides_realtime
		WHERE status IN ('requested', 'matched', 'in_progress')
		AND city = $1
		AND event_time > now() - INTERVAL '1 hour'
	`, city).Scan(&activeRides)
	if err != nil {
		return nil, err
	}
	
	// Compare to previous hour
	var prevRides int
	p.pinotDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM rides_realtime
		WHERE status IN ('requested', 'matched', 'in_progress')
		AND city = $1
		AND event_time BETWEEN now() - INTERVAL '2 hour' AND now() - INTERVAL '1 hour'
	`, city).Scan(&prevRides)
	
	change := 0.0
	if prevRides > 0 {
		change = (float64(activeRides) - float64(prevRides)) / float64(prevRides) * 100
	}
	
	metrics = append(metrics, DashboardMetric{
		Name:   "active_rides",
		Value:  activeRides,
		Change: change,
		Trend:  getTrend(change),
	})
	
	// Revenue last hour
	var revenue float64
	p.pinotDB.QueryRowContext(ctx, `
		SELECT SUM(fare_amount) as revenue
		FROM rides_realtime
		WHERE status = 'completed'
		AND city = $1
		AND event_time > now() - INTERVAL '1 hour'
	`, city).Scan(&revenue)
	
	metrics = append(metrics, DashboardMetric{
		Name:  "hourly_revenue",
		Value: revenue,
	})
	
	// Average ETA
	var avgETA float64
	p.pinotDB.QueryRowContext(ctx, `
		SELECT AVG(duration_minutes) as avg_eta
		FROM rides_realtime
		WHERE city = $1
		AND event_time > now() - INTERVAL '30 minutes'
	`, city).Scan(&avgETA)
	
	metrics = append(metrics, DashboardMetric{
		Name:  "avg_eta_minutes",
		Value: avgETA,
	})
	
	// Driver utilization
	var utilization float64
	p.pinotDB.QueryRowContext(ctx, `
		SELECT 
			COUNT(DISTINCT CASE WHEN status = 'in_progress' THEN driver_id END) * 100.0 /
			NULLIF(COUNT(DISTINCT driver_id), 0) as utilization
		FROM rides_realtime
		WHERE city = $1
		AND event_time > now() - INTERVAL '1 hour'
	`, city).Scan(&utilization)
	
	metrics = append(metrics, DashboardMetric{
		Name:  "driver_utilization",
		Value: utilization,
	})
	
	return metrics, nil
}

// PredictDemand forecasts demand for next 24 hours
func (p *PredictiveBI) PredictDemand(ctx context.Context, city string) (*Prediction, error) {
	// Get historical features
	features, err := p.getDemandFeatures(ctx, city)
	if err != nil {
		return nil, err
	}
	
	// Call ML service for prediction
	prediction, err := p.mlService.PredictDemand(ctx, features)
	if err != nil {
		return nil, err
	}
	
	return prediction, nil
}

// PredictRevenue forecasts revenue for next 7 days
func (p *PredictiveBI) PredictRevenue(ctx context.Context, city string, days int) (*Prediction, error) {
	features, err := p.getRevenueFeatures(ctx, city, days)
	if err != nil {
		return nil, err
	}
	
	prediction, err := p.mlService.PredictRevenue(ctx, features)
	if err != nil {
		return nil, err
	}
	
	return prediction, nil
}

// DetectAnomalies uses ML to find unusual patterns
func (p *PredictiveBI) DetectAnomalies(ctx context.Context, city string) ([]Anomaly, error) {
	anomalies := []Anomaly{}
	
	// Check ride volume anomaly
	rideVolume, err := p.getCurrentRideVolume(ctx, city)
	if err != nil {
		return nil, err
	}
	
	expectedVolume, stddev, err := p.getExpectedRideVolume(ctx, city)
	if err != nil {
		return nil, err
	}
	
	deviation := (rideVolume - expectedVolume) / stddev
	
	if deviation > 3 || deviation < -3 {
		severity := "medium"
		if deviation > 5 || deviation < -5 {
			severity = "high"
		}
		if deviation > 7 || deviation < -7 {
			severity = "critical"
		}
		
		anomalies = append(anomalies, Anomaly{
			Metric:    "ride_volume",
			Value:     rideVolume,
			Expected:  expectedVolume,
			Deviation: deviation,
			Severity:  severity,
			StartTime: time.Now(),
			Description: fmt.Sprintf("Ride volume is %.1f standard deviations from expected", deviation),
			Recommendation: p.getAnomalyRecommendation("ride_volume", deviation),
		})
	}
	
	// Check cancellation rate anomaly
	cancelRate, err := p.getCurrentCancellationRate(ctx, city)
	if err != nil {
		return nil, err
	}
	
	expectedCancelRate, cancelStddev, err := p.getExpectedCancellationRate(ctx, city)
	if err != nil {
		return nil, err
	}
	
	cancelDeviation := (cancelRate - expectedCancelRate) / cancelStddev
	
	if cancelDeviation > 2 {
		anomalies = append(anomalies, Anomaly{
			Metric:    "cancellation_rate",
			Value:     cancelRate,
			Expected:  expectedCancelRate,
			Deviation: cancelDeviation,
			Severity:  "high",
			StartTime: time.Now(),
			Description: fmt.Sprintf("Cancellation rate is %.1f%%, expected %.1f%%", cancelRate, expectedCancelRate),
			Recommendation: "Investigate driver availability and rider experience. Consider surge pricing or driver incentives.",
		})
	}
	
	return anomalies, nil
}

// GetExecutiveInsights generates AI-powered business insights
func (p *PredictiveBI) GetExecutiveInsights(ctx context.Context) ([]string, error) {
	insights := []string{}
	
	// Top performing cities
	rows, err := p.pinotDB.QueryContext(ctx, `
		SELECT city, 
		       SUM(fare_amount) as revenue,
		       COUNT(*) as rides,
		       AVG(rating) as avg_rating
		FROM rides_realtime
		WHERE event_time > now() - INTERVAL '7 days'
		GROUP BY city
		ORDER BY revenue DESC
		LIMIT 5
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var topCity string
	var topRevenue float64
	if rows.Next() {
		rows.Scan(&topCity, &topRevenue, nil, nil)
		insights = append(insights, fmt.Sprintf("🏆 %s is your top-performing city with $%.0f revenue this week", topCity, topRevenue))
	}
	
	// Growth opportunities
	rows2, err := p.warehouse.QueryContext(ctx, `
		SELECT city, growth_rate, market_potential
		FROM city_growth_analysis
		WHERE growth_rate > 0.2 AND market_potential = 'high'
		ORDER BY growth_rate DESC
		LIMIT 3
	`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var city string
			var growth, potential float64
			rows2.Scan(&city, &growth, &potential)
			insights = append(insights, fmt.Sprintf("📈 %s shows %.0f%% growth with high market potential - consider expansion", city, growth*100))
		}
	}
	
	// Predictive insights
	prediction, _ := p.PredictRevenue(ctx, "all", 7)
	if prediction != nil {
		insights = append(insights, fmt.Sprintf("🔮 Revenue predicted to be $%.0f next week (±%.0f%% confidence)", 
			prediction.Predicted, (1-prediction.Confidence)*100))
	}
	
	return insights, nil
}

func (p *PredictiveBI) getCurrentRideVolume(ctx context.Context, city string) (float64, error) {
	var volume float64
	err := p.pinotDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM rides_realtime
		WHERE city = $1
		AND event_time > now() - INTERVAL '1 hour'
	`, city).Scan(&volume)
	return volume, err
}

func (p *PredictiveBI) getExpectedRideVolume(ctx context.Context, city string) (mean, stddev float64, err error) {
	err = p.warehouse.QueryRowContext(ctx, `
		SELECT AVG(volume), STDDEV(volume)
		FROM historical_hourly_volumes
		WHERE city = $1
		AND hour_of_day = EXTRACT(HOUR FROM NOW())
		AND day_of_week = EXTRACT(DOW FROM NOW())
		AND date > CURRENT_DATE - INTERVAL '90 days'
	`, city).Scan(&mean, &stddev)
	return
}

func (p *PredictiveBI) getCurrentCancellationRate(ctx context.Context, city string) (float64, error) {
	var rate float64
	err := p.pinotDB.QueryRowContext(ctx, `
		SELECT 
			COUNT(CASE WHEN status = 'cancelled' THEN 1 END) * 100.0 / NULLIF(COUNT(*), 0)
		FROM rides_realtime
		WHERE city = $1
		AND event_time > now() - INTERVAL '2 hours'
	`, city).Scan(&rate)
	return rate, err
}

func (p *PredictiveBI) getExpectedCancellationRate(ctx context.Context, city string) (mean, stddev float64, err error) {
	err = p.warehouse.QueryRowContext(ctx, `
		SELECT AVG(cancel_rate), STDDEV(cancel_rate)
		FROM historical_cancel_rates
		WHERE city = $1
		AND date > CURRENT_DATE - INTERVAL '30 days'
	`, city).Scan(&mean, &stddev)
	return
}

func (p *PredictiveBI) getAnomalyRecommendation(metric string, deviation float64) string {
	if deviation > 0 {
		return "Unusually high demand detected. Consider activating surge pricing and incentivizing drivers."
	}
	return "Unusually low demand detected. Check for service issues, competitor activity, or external events."
}

func getTrend(change float64) string {
	if change > 5 {
		return "up"
	} else if change < -5 {
		return "down"
	}
	return "stable"
}