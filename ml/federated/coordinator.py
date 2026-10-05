import os
import torch
import numpy as np
from typing import List, Dict
import mlflow
from collections import OrderedDict

class FederatedAveragingCoordinator:
    """
    Coordinates federated learning across multiple regions.
    Each region trains locally, only model weights are aggregated globally.
    PII never leaves the region.
    """
    
    def __init__(self, global_model_path: str, min_clients: int = 3):
        self.global_model = torch.load(global_model_path)
        self.min_clients = min_clients
        self.round_number = 0
        
    def aggregate_weights(self, client_weights: List[Dict[str, torch.Tensor]], 
                         client_sizes: List[int]) -> Dict[str, torch.Tensor]:
        """
        Federated Averaging (FedAvg) algorithm
        Weighted average based on client dataset sizes
        """
        if len(client_weights) < self.min_clients:
            raise ValueError(f"Need at least {self.min_clients} clients, got {len(client_weights)}")
        
        total_size = sum(client_sizes)
        aggregated = OrderedDict()
        
        for key in client_weights[0].keys():
            aggregated[key] = torch.zeros_like(client_weights[0][key])
            for client_weight, size in zip(client_weights, client_sizes):
                weight = size / total_size
                aggregated[key] += client_weight[key] * weight
        
        return aggregated
    
    def run_federation_round(self, regional_models: List[Dict]) -> Dict:
        """Execute one round of federated learning"""
        self.round_number += 1
        
        client_weights = [m['weights'] for m in regional_models]
        client_sizes = [m['dataset_size'] for m in regional_models]
        
        # Aggregate
        global_weights = self.aggregate_weights(client_weights, client_sizes)
        
        # Evaluate global model (using validation set from each region)
        metrics = self._evaluate_global_model(global_weights, regional_models)
        
        # Log to MLflow
        with mlflow.start_run(run_name=f"federated_round_{self.round_number}"):
            mlflow.log_metrics(metrics)
            mlflow.log_metric("round_number", self.round_number)
            mlflow.log_metric("num_participants", len(regional_models))
        
        print(f"✅ Federation round {self.round_number} complete")
        print(f"Global metrics: {metrics}")
        
        return global_weights
    
    def _evaluate_global_model(self, weights: Dict, regional_models: List[Dict]) -> Dict:
        """Evaluate global model on each region's validation set"""
        # Load global model with new weights
        model = torch.load("model_architecture.pt")
        model.load_state_dict(weights)
        model.eval()
        
        metrics = {}
        for i, regional in enumerate(regional_models):
            val_loader = regional['val_loader']
            loss, accuracy = self._evaluate(model, val_loader)
            metrics[f"region_{i}_val_loss"] = loss
            metrics[f"region_{i}_val_accuracy"] = accuracy
        
        # Average across regions
        metrics["global_val_loss"] = np.mean([metrics[f"region_{i}_val_loss"] for i in range(len(regional_models))])
        metrics["global_val_accuracy"] = np.mean([metrics[f"region_{i}_val_accuracy"] for i in range(len(regional_models))])
        
        return metrics
    
    def _evaluate(self, model, dataloader) -> tuple:
        """Evaluate model on dataloader"""
        criterion = torch.nn.MSELoss()
        total_loss = 0
        total_correct = 0
        total_samples = 0
        
        with torch.no_grad():
            for x, y in dataloader:
                outputs = model(x)
                loss = criterion(outputs, y)
                total_loss += loss.item() * x.size(0)
                total_samples += x.size(0)
        
        return total_loss / total_samples, 0  # Accuracy calculation depends on task