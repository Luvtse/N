import os
import mlflow
import pandas as pd
import numpy as np
import torch
import torch.nn as nn
from torch.utils.data import Dataset, DataLoader
from sklearn.preprocessing import MinMaxScaler
import joblib

class DemandDataset(Dataset):
    def __init__(self, data, seq_length=24):
        self.data = data
        self.seq_length = seq_length
        
    def __len__(self):
        return len(self.data) - self.seq_length
    
    def __getitem__(self, idx):
        x = self.data[idx:idx + self.seq_length]
        y = self.data[idx + self.seq_length]
        return torch.FloatTensor(x), torch.FloatTensor(y)

class LSTMForecaster(nn.Module):
    def __init__(self, input_size=10, hidden_size=128, num_layers=2, output_size=1):
        super().__init__()
        self.lstm = nn.LSTM(
            input_size=input_size,
            hidden_size=hidden_size,
            num_layers=num_layers,
            batch_first=True,
            dropout=0.2,
        )
        self.fc = nn.Sequential(
            nn.Linear(hidden_size, 64),
            nn.ReLU(),
            nn.Dropout(0.2),
            nn.Linear(64, output_size),
        )
    
    def forward(self, x):
        lstm_out, _ = self.lstm(x)
        last_hidden = lstm_out[:, -1, :]
        return self.fc(last_hidden)

class DemandForecasterTrainer:
    def __init__(self):
        self.device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        self.scaler = MinMaxScaler()
        
    def prepare_data(self, df: pd.DataFrame, seq_length=24):
        """Prepare time-series data for LSTM"""
        # Features: hour, day_of_week, is_rush_hour, weather, traffic, historical_demand, etc.
        feature_cols = [
            'requests_last_hour', 'requests_last_24h', 'available_drivers',
            'demand_supply_ratio', 'hour_of_day', 'day_of_week',
            'is_holiday', 'event_nearby', 'historical_avg_requests',
            'avg_wait_time_minutes'
        ]
        
        data = df[feature_cols].values
        scaled_data = self.scaler.fit_transform(data)
        
        # Create sequences
        dataset = DemandDataset(scaled_data, seq_length)
        train_size = int(len(dataset) * 0.8)
        val_size = int(len(dataset) * 0.1)
        test_size = len(dataset) - train_size - val_size
        
        train_set, val_set, test_set = torch.utils.data.random_split(
            dataset, [train_size, val_size, test_size]
        )
        
        return (
            DataLoader(train_set, batch_size=32, shuffle=True),
            DataLoader(val_set, batch_size=32),
            DataLoader(test_set, batch_size=32),
        )
    
    def train(self, train_loader, val_loader, epochs=100, lr=0.001):
        """Train LSTM model"""
        with mlflow.start_run(run_name="demand_lstm_v1"):
            model = LSTMForecaster().to(self.device)
            criterion = nn.MSELoss()
            optimizer = torch.optim.Adam(model.parameters(), lr=lr)
            scheduler = torch.optim.lr_scheduler.ReduceLROnPlateau(
                optimizer, mode='min', factor=0.5, patience=10
            )
            
            mlflow.log_params({
                'epochs': epochs,
                'lr': lr,
                'hidden_size': 128,
                'num_layers': 2,
                'seq_length': 24,
                'batch_size': 32,
            })
            
            best_val_loss = float('inf')
            
            for epoch in range(epochs):
                # Training
                model.train()
                train_loss = 0
                for x_batch, y_batch in train_loader:
                    x_batch, y_batch = x_batch.to(self.device), y_batch.to(self.device)
                    optimizer.zero_grad()
                    predictions = model(x_batch)
                    loss = criterion(predictions, y_batch)
                    loss.backward()
                    torch.nn.utils.clip_grad_norm_(model.parameters(), max_norm=1.0)
                    optimizer.step()
                    train_loss += loss.item()
                
                # Validation
                model.eval()
                val_loss = 0
                with torch.no_grad():
                    for x_batch, y_batch in val_loader:
                        x_batch, y_batch = x_batch.to(self.device), y_batch.to(self.device)
                        predictions = model(x_batch)
                        val_loss += criterion(predictions, y_batch).item()
                
                train_loss /= len(train_loader)
                val_loss /= len(val_loader)
                
                scheduler.step(val_loss)
                
                mlflow.log_metrics({
                    'train_loss': train_loss,
                    'val_loss': val_loss,
                    'learning_rate': optimizer.param_groups[0]['lr'],
                }, step=epoch)
                
                if val_loss < best_val_loss:
                    best_val_loss = val_loss
                    torch.save(model.state_dict(), '/tmp/best_demand_model.pt')
                    mlflow.log_artifact('/tmp/best_demand_model.pt')
                
                if epoch % 10 == 0:
                    print(f"Epoch {epoch}: train_loss={train_loss:.4f}, val_loss={val_loss:.4f}")
            
            # Log final model
            mlflow.pytorch.log_model(
                model,
                "demand_model",
                registered_model_name="nidaw-demand-forecaster",
            )
            
            # Save scaler
            joblib.dump(self.scaler, '/tmp/demand_scaler.pkl')
            mlflow.log_artifact('/tmp/demand_scaler.pkl')
            
            print(f"✅ Demand forecaster trained! Best val_loss: {best_val_loss:.4f}")
            return model

if __name__ == "__main__":
    # Load historical demand data
    df = pd.read_parquet("data/hourly_demand.parquet")
    
    trainer = DemandForecasterTrainer()
    train_loader, val_loader, test_loader = trainer.prepare_data(df)
    trainer.train(train_loader, val_loader)