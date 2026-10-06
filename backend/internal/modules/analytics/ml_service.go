package analytics

import (
"bytes"
"context"
"encoding/json"
"fmt"
"net/http"
"time"
)

// MLService is the client for the Neuron AI platform inference endpoints.
// It converts feature vectors into predictions consumed by PredictiveBI.
type MLService struct {
baseURL    string
apiKey     string
httpClient *http.Client
timeout    time.Duration
}

// NewMLService creates an ML inference client pointed at the model-serving
// gateway (TorchServe/Ray). baseURL must not include a trailing slash.
func NewMLService(baseURL, apiKey string, timeout time.Duration) *MLService {
if timeout == 0 {
timeout = 5 * time.Second
}
return &MLService{
baseURL:    baseURL,
apiKey:     apiKey,
httpClient: &http.Client{Timeout: timeout},
timeout:    timeout,
}
}

// predictResponse mirrors the shared inference envelope used by TorchServe
// and Ray deployments in ml/serving/.
type predictResponse struct {
Predicted    float64                 `json:"predicted"`
LowerBound   float64                 `json:"lower_bound"`
UpperBound   float64                 `json:"upper_bound"`
Confidence   float64                 `json:"confidence"`
Contributors []PredictionContributor `json:"contributors"`
}

func (m *MLService) post(ctx context.Context, endpoint string, body interface{}, out *predictResponse) error {
payload, err := json.Marshal(body)
if err != nil {
return fmt.Errorf("failed to encode ml request: %w", err)
}
req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+endpoint, bytes.NewReader(payload))
if err != nil {
return err
}
req.Header.Set("Content-Type", "application/json")
if m.apiKey != "" {
req.Header.Set("Authorization", "Bearer "+m.apiKey)
}
resp, err := m.httpClient.Do(req)
if err != nil {
return fmt.Errorf("ml request failed: %w", err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusOK {
return fmt.Errorf("ml service returned status %d", resp.StatusCode)
}
return json.NewDecoder(resp.Body).Decode(out)
}

// PredictDemand calls the demand-forecasting model with historical features.
func (m *MLService) PredictDemand(ctx context.Context, features map[string]interface{}) (*Prediction, error) {
out := &predictResponse{}
if err := m.post(ctx, "/v1/models/demand_forecasting:predict", features, out); err != nil {
return nil, err
}
return &Prediction{
Metric:       "demand",
Timeframe:    "next_24h",
Predicted:    out.Predicted,
LowerBound:   out.LowerBound,
UpperBound:   out.UpperBound,
Confidence:   out.Confidence,
Contributors: out.Contributors,
}, nil
}

// PredictRevenue calls the revenue-forecasting model.
func (m *MLService) PredictRevenue(ctx context.Context, features map[string]interface{}) (*Prediction, error) {
out := &predictResponse{}
if err := m.post(ctx, "/v1/models/revenue_forecasting:predict", features, out); err != nil {
return nil, err
}
return &Prediction{
Metric:       "revenue",
Timeframe:    "next_7d",
Predicted:    out.Predicted,
LowerBound:   out.LowerBound,
UpperBound:   out.UpperBound,
Confidence:   out.Confidence,
Contributors: out.Contributors,
}, nil
}
