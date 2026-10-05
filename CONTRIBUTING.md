# Contributing to NIDAW

Thank you for your interest in contributing to NIDAW! This document provides guidelines and information for contributors.

## 📋 Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Pull Request Process](#pull-request-process)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [Security](#security)
- [Getting Help](#getting-help)

## 🤝 Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## 🚀 Getting Started

### Prerequisites

- **Go** 1.21+
- **Flutter** 3.16+
- **Python** 3.11+
- **Node.js** 20+
- **Docker** 24.0+
- **Terraform** 1.5+
- **kubectl** 1.28+

### Setup

1. **Fork the repository**
   ```bash
   git clone https://github.com/YOUR_USERNAME/nidaw.git
   cd nidaw

2. Install dependencies

   make setup

3. Start development environment

make dev

Verify everything works

make test

🔄 Development Workflow
Branch Naming Convention

feature/NID-{ticket}-{description}
bugfix/NID-{ticket}-{description}
hotfix/NID-{ticket}-{description}
release/v{version}

Examples:
feature/NID-123-add-driver-matching
bugfix/NID-456-fix-eta-calculation
hotfix/NID-789-critical-payment-issue
Commit Message Format
We follow Conventional Commits:

<type>(<scope>): <subject>

<body>

<footer>

Types:
feat: New feature
fix: Bug fix
docs: Documentation
style: Code style (formatting, etc.)
refactor: Code refactoring
perf: Performance improvement
test: Adding tests
chore: Maintenance tasks
ci: CI/CD changes
build: Build system changes

Examples:

feat(nidus): add multi-factor driver matching algorithm

Implemented a scoring system for driver matching:
- Distance (40% weight)
- Rating (25% weight)
- Acceptance rate (20% weight)
- Completion rate (10% weight)
- Time since last ride (5% weight)

Closes #123

📝 Coding Standards
Go (Backend)
Follow Effective Go
Use gofmt and goimports
Run golangci-lint before committing
Maximum function length: 50 lines
Maximum cyclomatic complexity: 15

Example:


// CreateRide creates a new ride request.
//
// Parameters:
//   - ctx: Request context
//   - req: Ride request data
//
// Returns:
//   - *Ride: Created ride entity
//   - error: Validation or database error
func (s *Service) CreateRide(ctx context.Context, req *Request) (*Ride, error) {
    // Implementation
}

Flutter/Dart (Frontend)
Follow Flutter Style Guide
Use dart format
Run flutter analyze before committing
Maximum widget build method: 100 lines
Extract widgets > 50 lines

Example:

class RideRequestPage extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(),
      body: Column(
        children: [
          _buildMap(),
          _buildLocationInputs(),
        ],
      ),
    );
  }
}

Python (ML/AI)
Follow PEP 8
Use black for formatting
Use isort for imports
Run flake8 and mypy before committing
Maximum line length: 100 characters

Example:

def calculate_eta(
    pickup_lat: float,
    pickup_lng: float,
    dropoff_lat: float,
    dropoff_lng: float,
) -> int:
    """Calculate estimated time of arrival.
    
    Args:
        pickup_lat: Pickup latitude
        pickup_lng: Pickup longitude
        dropoff_lat: Dropoff latitude
        dropoff_lng: Dropoff longitude
    
    Returns:
        Estimated time in minutes
    """
    # Implementation

Terraform
Follow Terraform Style Conventions
Run terraform fmt before committing
Use modules for reusable components
Always use variables for configurable values
SQL
Use lowercase for SQL keywords
Use meaningful table and column names
Always include indexes for foreign keys
Write comments for complex queries
🔀 Pull Request Process
Before Submitting

1. Update your branch

   git fetch origin
   git rebase origin/develop

2. Run all tests

   make test

3. Check code quality

   make lint

4. Update documentation
Update README if needed
Add/update API documentation
Update CHANGELOG


PR Template

## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review performed
- [ ] Documentation updated
- [ ] No new warnings
- [ ] Tests pass locally
- [ ] Changelog updated

## Screenshots
(if applicable)

## Related Issues
Closes #123

Review Process
Automated checks must pass (CI/CD)
Code review by at least 2 maintainers
Security review for sensitive changes
Performance review for performance-critical changes
🧪 Testing Guidelines
Test Coverage Requirements
Backend (Go): Minimum 80% coverage
Frontend (Flutter): Minimum 70% coverage
ML (Python): Minimum 75% coverage
Critical paths: 100% coverage required
Test Types
Unit Tests
Test individual functions/methods
Fast execution (< 1 second per test)
No external dependencies
Integration Tests
Test component interactions
Use test containers for dependencies
Execute in CI/CD
E2E Tests
Test complete user flows
Run against staging environment
Execute nightly
Writing Tests

Go Example:

func TestCalculateFare(t *testing.T) {
    tests := []struct {
        name     string
        input    FareInput
        expected float64
    }{
        {
            name:     "short trip",
            input:    FareInput{Distance: 2.0},
            expected: 10.0,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := CalculateFare(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}


Flutter Example:

void main() {
  group('RideBloc', () {
    test('emits RideLoading when RequestRide is added', () {
      final bloc = RideBloc();
      
      bloc.add(RequestRide(/* params */));
      
      expectLater(
        bloc.stream,
        emitsInOrder([
          isA<RideLoading>(),
          isA<RideRequested>(),
        ]),
      );
    });
  });
}

📚 Documentation
Types of Documentation
Code Documentation
Inline comments for complex logic
Docstrings for public APIs
README for each module
API Documentation
OpenAPI/Swagger specs
Postman collections
API examples
Architecture Documentation
System design diagrams
Data flow diagrams
Deployment guides
User Documentation
User guides
FAQs
Troubleshooting
Documentation Standards
Use Markdown for all documentation
Include code examples
Keep documentation up-to-date
Link to related documents
🔒 Security
Security Guidelines
Never commit secrets
Use environment variables
Use AWS Secrets Manager
Use .env.example for templates
Input validation
Validate all user inputs
Sanitize data before storage
Use parameterized queries
Authentication & Authorization
Use JWT for authentication
Implement RBAC
Follow least privilege principle
Encryption
Encrypt data at rest (AES-256)
Encrypt data in transit (TLS 1.3)
Use strong key management
Reporting Security Issues
DO NOT create public GitHub issues for security vulnerabilities.
Instead, email: security@nidaw.com
Include:
Description of vulnerability
Steps to reproduce
Potential impact
Suggested fix (if any)
🆘 Getting Help
Resources
Documentation: https://docs.nidaw.com
Discord: https://discord.gg/nidaw
Email: engineering@nidaw.com
Slack: #engineering (internal)
Asking Questions
Search first - Check documentation and existing issues
Be specific - Include code snippets, error messages, environment details
Show effort - Describe what you've tried
Be patient - Maintainers are volunteers
🎉 Recognition
Contributors will be recognized in:
README.md (top contributors)
Release notes
Annual contributor awards
📄 License
By contributing, you agree that your contributions will be licensed under the project's license.
Thank you for contributing to NIDAW! 🚀