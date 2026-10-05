
### `frontend/README.md`

```markdown
# NIDAW Frontend

Flutter-based super-app for the NIDAW platform.

## 🏗️ Architecture

- **Pattern**: Clean Architecture with BLoC
- **State Management**: flutter_bloc
- **Navigation**: go_router
- **DI**: get_it
- **Networking**: Dio + WebSocket

## 📁 Project Structure

frontend/
├── lib/
│ ├── core/ # Core utilities
│ │ ├── di/ # Dependency injection
│ │ ├── network/ # API & WebSocket
│ │ ├── router/ # Navigation
│ │ └── theme/ # Theming
│ ├── features/ # Feature modules
│ │ ├── auth/ # Authentication
│ │ ├── nidus/ # Rides
│ │ ├── haven/ # Hotels
│ │ ├── vorax/ # Food
│ │ └── profile/ # User profile
│ └── main.dart # App entry point
├── android/ # Android platform
├── ios/ # iOS platform
└── web/ # Web platform


## 🚀 Getting Started

### Prerequisites

- Flutter 3.16+
- Dart 3.2+
- Android Studio / Xcode

### Setup

1. Install dependencies:
```bash
flutter pub get

2. Configure environment:

# Create .env file
cat > .env << EOF
API_BASE_URL=http://localhost:8080
WS_BASE_URL=ws://localhost:8080/ws
EOF


3. Run the app:

# Web
flutter run -d chrome

# Android
flutter run -d android

# iOS
flutter run -d ios

🧪 Testing

# Run all tests
flutter test

# Run with coverage
flutter test --coverage

# Run specific test
flutter test test/features/auth/

📦 Building

# Android APK
flutter build apk --release

# Android App Bundle
flutter build appbundle --release

# iOS
flutter build ios --release

# Web
flutter build web --release

Platform	Status	Min Version
Android	         ✅	API 23+
iOS	         ✅	iOS 13+
Web	         ✅	Chrome 80+

🔒 Security
Secure token storage
Certificate pinning
Biometric authentication
Jailbreak detection

NIDAW Frontend - Built with Flutter