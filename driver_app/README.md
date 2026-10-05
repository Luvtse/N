# 🚗 NIDAW Driver App

Driver mobile application for the NIDAW platform. Drive, earn, and manage your driver profile.

## 🚀 Features

- **Real-time ride requests** - Receive and accept ride requests instantly
- **Navigation** - Built-in navigation to pickup and dropoff locations
- **Earnings tracking** - View daily, weekly, and monthly earnings
- **Document management** - Upload and manage driver documents
- **Profile management** - Update driver profile and vehicle information
- **Availability toggle** - Go online/offline with one tap
- **Real-time tracking** - Live location sharing with riders

## 📱 Platforms

- ✅ Android (API 23+)
- ✅ iOS (13.0+)
- ✅ Web (Chrome, Safari, Firefox)

## 🛠️ Development

### Prerequisites

- Flutter 3.16+
- Dart 3.2+
- Android Studio / Xcode
- Google Maps API Key

### Setup

```bash
# Install dependencies
flutter pub get

# Run on Chrome
flutter run -d chrome

# Run on Android
flutter run -d android

# Run on iOS
flutter run -d ios

Environment Variables
Create a .env file:

API_BASE_URL=http://localhost:8080
WS_BASE_URL=ws://localhost:8080/ws
GOOGLE_MAPS_API_KEY=your_api_key

Build

# Android APK
flutter build apk --release

# Android App Bundle
flutter build appbundle --release

# iOS
flutter build ios --release

# Web
flutter build web --release

📦 Deployment
Docker

docker build -t nidaw-driver-web .
docker run -p 80:80 nidaw-driver-web

App Stores
Google Play Store: Submit AAB file
Apple App Store: Submit IPA file
🔐 Security
JWT authentication
Secure token storage
HTTPS only
Biometric authentication (optional)
📞 Support
Email: driver-support@nidaw.com
Discord: https://discord.gg/nidaw
Documentation: https://docs.nidaw.com/driver
NIDAW Driver App - Drive and Earn 🚗💨