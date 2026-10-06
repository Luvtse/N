# Feature Flags Documentation

This directory contains the feature flag configuration for the NIDAW platform.

## 📖 Overview

Feature flags allow us to:
- ✅ Safely roll out new features to subsets of users
- ✅ Conduct A/B tests
- ✅ Quickly disable features in case of issues
- ✅ Manage experimental features
- ✅ Control operational modes (maintenance, read-only)

## 📁 Structure

feature-flags/
├── README.md ← You are here
├── flags.json ← Feature flag definitions
└── lifecycle.md ← Feature flag lifecycle policy


## 🏷️ Flag Categories

| Category | Description | Examples |
|----------|-------------|----------|
| **core** | Core platform services | nidus_rides, haven_hotels |
| **experimental** | Experimental features | autonomous_vehicles, ar_navigation |
| **ledger** | Internal settlement features | topup, escrow, disputes (Phase D) |
| **ai_ml** | AI/ML powered features | dynamic_pricing, recommendations |
| **ui** | UI/UX features | dark_mode, multi_language |
| **operational** | Operational controls | maintenance_mode, read_only_mode |

## 🎯 Flag Properties

### Required Properties

```json
{
  "enabled": true,                    // Is the flag enabled?
  "rolloutPercentage": 100,           // % of users who see the feature
  "targeting": {                      // Who sees the feature?
    "environments": ["production"],
    "regions": ["US", "EU"],
    "userSegments": ["beta_testers"]
  },
  "metadata": {                       // Additional information
    "owner": "team@nidaw.com",
    "created": "2026-01-01T00:00:00Z",
    "expires": "2026-12-31T23:59:59Z",
    "description": "Feature description",
    "category": "experimental",
    "abTestId": "test-id-123"
  }
}

🚀 Usage
Backend (Go)

package main

import (
    "context"
    "nidaw-backend/internal/shared/featureflags"
)

func main() {
    // Load feature flags
    ff, err := featureflags.Load("config/feature-flags/flags.json")
    if err != nil {
        log.Fatal(err)
    }
    
    // Check if feature is enabled for user
    ctx := context.Background()
    user := &User{ID: "user-123", Region: "US", Segment: "beta_testers"}
    
    if ff.IsEnabled(ctx, "autonomous_vehicles", user) {
        // Show autonomous vehicle option
        showAutonomousVehicles()
    }
}

Frontend (Flutter)

import 'package:nidaw_app/core/feature_flags/feature_flags.dart';

void main() async {
  // Load feature flags
  final ff = await FeatureFlags.load('assets/flags.json');
  
  // Check if feature is enabled
  final user = User(id: 'user-123', region: 'US');
  
  if (ff.isEnabled('ar_navigation', user)) {
    // Show AR navigation button
    showARNavigationButton();
  }
}

🔄 Updating Flags
Manual Update
Edit flags.json
Run validation: ./scripts/validate-flags.sh
Commit changes
Deploy via CI/CD
Automated Update

# Enable a feature flag
./scripts/feature-flags/enable.sh autonomous_vehicles --percentage 50

# Disable a feature flag
./scripts/feature-flags/disable.sh maintenance_mode

# Update rollout percentage
./scripts/feature-flags/update.sh new_homepage_design --percentage 75

📊 Monitoring
Metrics

feature_flag_evaluations_total{flag, result}
feature_flag_rollout_percentage{flag}
feature_flag_targeting_matches{flag, segment}

Dashboards
Feature Flags Overview: All flags and their status
A/B Test Results: Performance metrics for experiments
Rollout Progress: Gradual rollout tracking
Alerts
Flag evaluation errors > 1%
Flag not found errors
Expired flags still in use
🛡️ Best Practices
✅ Do
Always set an owner for each flag
Set expiration dates for experimental features
Use meaningful flag names
Document the purpose of each flag
Test flags in staging before production
Monitor flag evaluations
Clean up old flags regularly
❌ Don't
Leave flags enabled indefinitely
Use flags for permanent features
Create too many flags (flag sprawl)
Forget to clean up after A/B tests
Use flags without proper targeting
Ignore flag evaluation errors
📅 Lifecycle
See lifecycle.md for the complete feature flag lifecycle policy.
📞 Support
Team: platform-team@nidaw.com
Slack: #feature-flags
Documentation: https://docs.nidaw.com/feature-flags
Last Updated: July 2026
Owner: Platform Team
Review Cadence: Monthly