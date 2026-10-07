import '../../data/repositories/auth_repository.dart';
import '../entities/driver.dart';

class RegisterUseCase {
  final AuthRepository _authRepository;

  RegisterUseCase(this._authRepository);

  Future<Driver> execute(Map<String, dynamic> data) async {
    // Validate required fields
    final requiredFields = ['email', 'password', 'full_name', 'phone'];
    for (final field in requiredFields) {
      if (data[field] == null || data[field].toString().isEmpty) {
        throw Exception('$field is required');
      }
    }

    // Validate email format
    final emailRegex = RegExp(r'^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$');
    if (!emailRegex.hasMatch(data['email'] as String)) {
      throw Exception('Invalid email format');
    }

    // Validate password strength
    final password = data['password'] as String;
    if (password.length < 8) {
      throw Exception('Password must be at least 8 characters');
    }

    // Execute registration
    return await _authRepository.register(data);
  }
}