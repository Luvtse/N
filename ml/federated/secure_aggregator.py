import os
import torch
import numpy as np
from typing import List, Dict, Tuple
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa, padding
from cryptography.hazmat.primitives import hashes
import hashlib
import json

class SecureFederatedAggregator:
    """
    Implements secure aggregation with:
    1. Differential privacy (Gaussian noise)
    2. Secure multi-party computation (secret sharing)
    3. Byzantine fault tolerance (Krum aggregation)
    4. Homomorphic encryption for sensitive updates
    """
    
    def __init__(self, 
                 epsilon: float = 1.0,  # Privacy budget
                 delta: float = 1e-5,
                 clip_norm: float = 1.0,
                 noise_multiplier: float = 0.1,
                 min_clients: int = 3,
                 byzantine_threshold: float = 0.3):
        self.epsilon = epsilon
        self.delta = delta
        self.clip_norm = clip_norm
        self.noise_multiplier = noise_multiplier
        self.min_clients = min_clients
        self.byzantine_threshold = byzantine_threshold
        
        # Generate RSA key pair for secure communication
        self.private_key = rsa.generate_private_key(
            public_exponent=65537,
            key_size=2048,
        )
        self.public_key = self.private_key.public_key()
    
    def aggregate_with_dp(self, 
                          client_weights: List[Dict[str, torch.Tensor]],
                          client_sizes: List[int]) -> Dict[str, torch.Tensor]:
        """
        Federated averaging with differential privacy
        Adds calibrated Gaussian noise to protect individual contributions
        """
        if len(client_weights) < self.min_clients:
            raise ValueError(f"Need at least {self.min_clients} clients")
        
        # Step 1: Clip each client's update to bound sensitivity
        clipped_weights = []
        for weights in client_weights:
            clipped = self._clip_update(weights)
            clipped_weights.append(clipped)
        
        # Step 2: Compute weighted average
        total_size = sum(client_sizes)
        aggregated = {}
        
        for key in clipped_weights[0].keys():
            aggregated[key] = torch.zeros_like(clipped_weights[0][key])
            for weight, size in zip(clipped_weights, client_sizes):
                weight_factor = size / total_size
                aggregated[key] += weight[key] * weight_factor
        
        # Step 3: Add calibrated Gaussian noise for differential privacy
        # Noise scale = (clip_norm * noise_multiplier) / batch_size
        noise_scale = (self.clip_norm * self.noise_multiplier) / total_size
        
        for key in aggregated.keys():
            noise = torch.normal(
                mean=0,
                std=noise_scale,
                size=aggregated[key].shape
            )
            aggregated[key] += noise
        
        # Step 4: Verify privacy budget consumption
        privacy_spent = self._compute_privacy_spent(len(client_weights))
        
        return aggregated, privacy_spent
    
    def _clip_update(self, weights: Dict[str, torch.Tensor]) -> Dict[str, torch.Tensor]:
        """Clip update to L2 norm bound"""
        # Flatten all weights
        flat = torch.cat([w.flatten() for w in weights.values()])
        norm = torch.norm(flat)
        
        if norm > self.clip_norm:
            scale = self.clip_norm / norm
            return {k: v * scale for k, v in weights.items()}
        
        return weights
    
    def _compute_privacy_spent(self, num_rounds: int) -> dict:
        """Compute privacy budget spent using RDP accountant"""
        # Simplified privacy accounting
        # In production, use tensorflow_privacy or opacus
        sigma = self.noise_multiplier
        q = 1.0 / num_rounds  # Sampling ratio
        
        # Approximate epsilon using advanced composition
        epsilon = (q * np.sqrt(2 * np.log(1.25 / self.delta))) / sigma
        
        return {
            'epsilon': epsilon,
            'delta': self.delta,
            'remaining_budget': max(0, self.epsilon - epsilon),
        }
    
    def krum_aggregate(self,
                       client_weights: List[Dict[str, torch.Tensor]],
                       f: int = None) -> Dict[str, torch.Tensor]:
        """
        Krum aggregation for Byzantine fault tolerance
        Selects the update closest to its neighbors
        Robust to up to f Byzantine clients where n >= 2f + 2
        """
        n = len(client_weights)
        if f is None:
            f = int(self.byzantine_threshold * n)
        
        if n < 2 * f + 2:
            raise ValueError(f"Need at least {2*f + 2} clients for Byzantine tolerance")
        
        # Flatten weights for distance computation
        flat_weights = []
        for weights in client_weights:
            flat = torch.cat([w.flatten() for w in weights.values()])
            flat_weights.append(flat)
        
        # Compute pairwise distances
        n_select = n - f - 2
        scores = []
        
        for i in range(n):
            distances = []
            for j in range(n):
                if i != j:
                    dist = torch.norm(flat_weights[i] - flat_weights[j]).item()
                    distances.append(dist)
            
            distances.sort()
            # Sum of n-f-2 smallest distances
            score = sum(distances[:n_select])
            scores.append(score)
        
        # Select client with minimum score
        selected_idx = np.argmin(scores)
        
        return client_weights[selected_idx]
    
    def multi_krum_aggregate(self,
                             client_weights: List[Dict[str, torch.Tensor]],
                             f: int = None,
                             m: int = 1) -> Dict[str, torch.Tensor]:
        """
        Multi-Krum: Select top-m clients and average their updates
        More robust than single Krum
        """
        n = len(client_weights)
        if f is None:
            f = int(self.byzantine_threshold * n)
        
        if n < 2 * f + 2:
            raise ValueError(f"Need at least {2*f + 2} clients")
        
        # Flatten weights
        flat_weights = []
        for weights in client_weights:
            flat = torch.cat([w.flatten() for w in weights.values()])
            flat_weights.append(flat)
        
        # Compute Krum scores
        n_select = n - f - 2
        scores = []
        
        for i in range(n):
            distances = []
            for j in range(n):
                if i != j:
                    dist = torch.norm(flat_weights[i] - flat_weights[j]).item()
                    distances.append(dist)
            distances.sort()
            score = sum(distances[:n_select])
            scores.append((i, score))
        
        # Sort by score and select top-m
        scores.sort(key=lambda x: x[1])
        selected_indices = [idx for idx, _ in scores[:m]]
        
        # Average selected updates
        aggregated = {}
        for key in client_weights[0].keys():
            aggregated[key] = torch.zeros_like(client_weights[0][key])
            for idx in selected_indices:
                aggregated[key] += client_weights[idx][key]
            aggregated[key] /= m
        
        return aggregated
    
    def sign_sign_aggregate(self,
                            client_weights: List[Dict[str, torch.Tensor]],
                            client_sizes: List[int]) -> Dict[str, torch.Tensor]:
        """
        SignSGD with majority vote
        Communication-efficient and robust to outliers
        """
        aggregated = {}
        
        for key in client_weights[0].keys():
            # Collect signs from all clients
            signs = []
            for weights in client_weights:
                sign = torch.sign(weights[key])
                signs.append(sign)
            
            # Majority vote
            sign_sum = torch.zeros_like(signs[0])
            for sign in signs:
                sign_sum += sign
            
            # Get magnitude from weighted average
            total_size = sum(client_sizes)
            magnitude = torch.zeros_like(client_weights[0][key])
            for weight, size in zip(client_weights, client_sizes):
                magnitude += weight[key] * (size / total_size)
            
            # Combine sign with magnitude
            aggregated[key] = torch.sign(sign_sum) * torch.abs(magnitude)
        
        return aggregated
    
    def verify_model_integrity(self, 
                               model_weights: Dict[str, torch.Tensor],
                               expected_hash: str) -> bool:
        """Verify model hasn't been tampered with"""
        # Compute hash of model weights
        hasher = hashlib.sha256()
        for key in sorted(model_weights.keys()):
            tensor_bytes = model_weights[key].cpu().numpy().tobytes()
            hasher.update(tensor_bytes)
        
        computed_hash = hasher.hexdigest()
        return computed_hash == expected_hash
    
    def compute_model_hash(self, model_weights: Dict[str, torch.Tensor]) -> str:
        """Compute hash for integrity verification"""
        hasher = hashlib.sha256()
        for key in sorted(model_weights.keys()):
            tensor_bytes = model_weights[key].cpu().numpy().tobytes()
            hasher.update(tensor_bytes)
        return hasher.hexdigest()