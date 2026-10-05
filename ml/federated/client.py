import os
import torch
from typing import Dict
import requests

class FederatedClient:
    """
    Runs on each regional cluster.
    Trains local model, sends only weights to coordinator.
    """
    
    def __init__(self, region_id: str, local_model_path: str, coordinator_url: str):
        self.region_id = region_id
        self.local_model = torch.load(local_model_path)
        self.coordinator_url = coordinator_url
        self.dataset_size = 0
    
    def train_locally(self, train_loader, epochs=5, lr=0.01):
        """Train model on local regional data"""
        self.local_model.train()
        optimizer = torch.optim.Adam(self.local_model.parameters(), lr=lr)
        criterion = torch.nn.MSELoss()
        
        for epoch in range(epochs):
            total_loss = 0
            for x, y in train_loader:
                optimizer.zero_grad()
                outputs = self.local_model(x)
                loss = criterion(outputs, y)
                loss.backward()
                optimizer.step()
                total_loss += loss.item()
            
            self.dataset_size += len(train_loader.dataset)
            print(f"Region {self.region_id} - Epoch {epoch}: loss={total_loss/len(train_loader):.4f}")
    
    def get_model_weights(self) -> Dict[str, torch.Tensor]:
        """Extract model weights for aggregation"""
        return self.local_model.state_dict()
    
    def update_model_weights(self, global_weights: Dict[str, torch.Tensor]):
        """Update local model with aggregated global weights"""
        self.local_model.load_state_dict(global_weights)
    
    def send_to_coordinator(self):
        """Send local weights to global coordinator"""
        weights = self.get_model_weights()
        
        # Serialize weights
        serialized = {k: v.cpu().numpy().tolist() for k, v in weights.items()}
        
        payload = {
            "region_id": self.region_id,
            "weights": serialized,
            "dataset_size": self.dataset_size,
            "round_number": self._get_current_round(),
        }
        
        response = requests.post(
            f"{self.coordinator_url}/federated/submit",
            json=payload,
        )
        
        if response.status_code == 200:
            print(f"✅ Region {self.region_id} submitted weights to coordinator")
            return response.json().get("global_weights")
        else:
            print(f"❌ Failed to submit weights: {response.text}")
            return None
    
    def _get_current_round(self) -> int:
        """Get current federation round from coordinator"""
        response = requests.get(f"{self.coordinator_url}/federated/round")
        return response.json().get("round_number", 1)