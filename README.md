# NIDAW - AI-Native Mobility Super-App

NIDAW seamlessly integrates transportation, accommodation, nourishment, logistics, and business services into a single self-optimizing ecosystem.

## 🚀 Features

- **Nidus**: On-demand ride-hailing with AI-powered matching
- **Haven**: Hotel and accommodation booking
- **Vorax**: Food delivery with real-time tracking
- **Logix**: Freight and logistics management
- **Corpus**: Corporate travel management

## 🏗️ Architecture

- **Backend**: Go modular monolith with CQRS + Event Sourcing
- **Frontend**: Flutter super-app with BLoC pattern
- **Database**: PostgreSQL + TimescaleDB + Redis
- **Messaging**: Apache Kafka event mesh
- **AI/ML**: Feast feature store + MLflow + TorchServe

## 📋 Prerequisites

- Go 1.21+
- Flutter 3.16+
- Docker & Docker Compose
- PostgreSQL 15+
- Redis 7+
- Apache Kafka 3.5+

## 🚀 Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/your-org/nidaw.git
cd nidaw

## 2. Setup environment

cp backend/.env.example backend/.env
# Edit backend/.env with your configuration

## 3. Start infrastructure

docker-compose up -d

## 4. Run backend

cd backend
go run cmd/server/main.go

## 5. Run frontend

cd frontend
flutter pub get
flutter run

📚 Documentation
Architecture
API Documentation
Development Guide
Deployment Guide

🧪 Testing

# Backend tests
cd backend
go test ./...

# Frontend tests
cd frontend
flutter test


📦 Deployment
See Deployment Guide for production deployment instructions.
🤝 Contributing
Please read CONTRIBUTING.md for details on our code of conduct.
📄 License
Proprietary - All rights reserved.
📞 Support
Email: support@nidaw.com
Discord: discord.gg/nidaw
Documentation: docs.nidaw.com

