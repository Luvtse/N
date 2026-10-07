import '../../data/repositories/auth_repository.dart';
import '../entities/driver.dart';

class LoginUseCase {
  final AuthRepository _authRepository;

  LoginUseCase(this._authRepository);

  Future<Driver> execute(String email, String password) async {
    // Validate input
    if (email.isEmpty || password.isEmpty) {
      throw Exception('Email and password are required');
    }

    if (password.length < 8) {
      throw Exception('Password must be at least 8 characters');
    }

    // Execute login
    return await _authRepository.login(email, password);
  }
}