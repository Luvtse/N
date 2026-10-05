import os
import mlflow
import pandas as pd
import numpy as np
import torch
import torch.nn as nn
import torch.optim as optim
from torch.utils.data import Dataset, DataLoader
from sklearn.model_selection import train_test_split
from sklearn.metrics import ndcg_score
import joblib

class RestaurantInteractionDataset(Dataset):
    """Dataset for collaborative filtering on restaurant orders"""
    
    def __init__(self, user_ids, restaurant_ids, ratings, timestamps=None):
        self.user_ids = torch.LongTensor(user_ids)
        self.restaurant_ids = torch.LongTensor(restaurant_ids)
        self.ratings = torch.FloatTensor(ratings)
        self.timestamps = timestamps
        
    def __len__(self):
        return len(self.user_ids)
    
    def __getitem__(self, idx):
        return (
            self.user_ids[idx],
            self.restaurant_ids[idx],
            self.ratings[idx]
        )

class NeuralCollaborativeFiltering(nn.Module):
    """NCF model combining MF and MLP approaches"""
    
    def __init__(self, num_users, num_items, embedding_dim=64, mlp_layers=[128, 64, 32]):
        super().__init__()
        
        # Embeddings
        self.user_embedding = nn.Embedding(num_users, embedding_dim)
        self.item_embedding = nn.Embedding(num_items, embedding_dim)
        
        # MF part
        self.mf_dot = lambda u, i: (u * i).sum(dim=1)
        
        # MLP part
        layers = []
        input_dim = embedding_dim * 2
        for layer_dim in mlp_layers:
            layers.append(nn.Linear(input_dim, layer_dim))
            layers.append(nn.ReLU())
            layers.append(nn.Dropout(0.3))
            input_dim = layer_dim
        layers.append(nn.Linear(input_dim, 1))
        
        self.mlp = nn.Sequential(*layers)
        
        # Final prediction layer
        self.predict_layer = nn.Linear(embedding_dim + mlp_layers[-1], 1)
        
    def forward(self, user_ids, item_ids):
        user_emb = self.user_embedding(user_ids)
        item_emb = self.item_embedding(item_ids)
        
        # MF component
        mf_vector = self.mf_dot(user_emb, item_emb)
        
        # MLP component
        mlp_input = torch.cat([user_emb, item_emb], dim=1)
        mlp_vector = self.mlp(mlp_input).squeeze()
        
        # Combine
        combined = torch.cat([mf_vector, mlp_vector], dim=1)
        prediction = self.predict_layer(combined).squeeze()
        
        return torch.sigmoid(prediction)

class RestaurantRecommender:
    """
    Recommends restaurants based on:
    1. User's order history (collaborative filtering)
    2. Restaurant characteristics (content-based)
    3. Context (time, location, weather)
    """
    
    def __init__(self):
        self.device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        self.model = None
        self.user_encoder = None
        self.restaurant_encoder = None
        
    def prepare_data(self, orders_df: pd.DataFrame):
        """Prepare interaction data"""
        # Encode user and restaurant IDs
        from sklearn.preprocessing import LabelEncoder
        self.user_encoder = LabelEncoder()
        self.restaurant_encoder = LabelEncoder()
        
        orders_df['user_encoded'] = self.user_encoder.fit_transform(orders_df['user_id'])
        orders_df['restaurant_encoded'] = self.restaurant_encoder.fit_transform(orders_df['restaurant_id'])
        
        # Create implicit ratings based on order frequency and recency
        orders_df['rating'] = self._calculate_implicit_rating(orders_df)
        
        num_users = len(self.user_encoder.classes_)
        num_restaurants = len(self.restaurant_encoder.classes_)
        
        # Split data
        train_df, test_df = train_test_split(
            orders_df, test_size=0.2, random_state=42
        )
        
        train_dataset = RestaurantInteractionDataset(
            train_df['user_encoded'].values,
            train_df['restaurant_encoded'].values,
            train_df['rating'].values
        )
        
        test_dataset = RestaurantInteractionDataset(
            test_df['user_encoded'].values,
            test_df['restaurant_encoded'].values,
            test_df['rating'].values
        )
        
        return (
            DataLoader(train_dataset, batch_size=256, shuffle=True),
            DataLoader(test_dataset, batch_size=256),
            num_users,
            num_restaurants
        )
    
    def _calculate_implicit_rating(self, df: pd.DataFrame) -> pd.Series:
        """Calculate implicit rating from order behavior"""
        # Factors: frequency, recency, order value, rating given
        user_rest_counts = df.groupby(['user_id', 'restaurant_id']).size().reset_index(name='count')
        
        # Normalize count to 0-1
        max_count = user_rest_counts['count'].max()
        user_rest_counts['freq_score'] = user_rest_counts['count'] / max_count
        
        # Merge back
        df = df.merge(
            user_rest_counts[['user_id', 'restaurant_id', 'freq_score']],
            on=['user_id', 'restaurant_id'],
            how='left'
        )
        
        # Combine with explicit ratings if available
        if 'user_rating' in df.columns:
            rating_score = df['user_rating'].fillna(3) / 5.0
            implicit_rating = 0.6 * df['freq_score'] + 0.4 * rating_score
        else:
            implicit_rating = df['freq_score']
        
        return implicit_rating
    
    def train(self, train_loader, test_loader, num_users, num_restaurants, epochs=50):
        """Train NCF model"""
        with mlflow.start_run(run_name="restaurant_recommender_v1"):
            self.model = NeuralCollaborativeFiltering(
                num_users=num_users,
                num_items=num_restaurants,
                embedding_dim=64,
                mlp_layers=[128, 64, 32]
            ).to(self.device)
            
            criterion = nn.BCELoss()
            optimizer = optim.Adam(self.model.parameters(), lr=0.001, weight_decay=1e-5)
            scheduler = optim.lr_scheduler.ReduceLROnPlateau(
                optimizer, mode='min', factor=0.5, patience=5
            )
            
            mlflow.log_params({
                'epochs': epochs,
                'embedding_dim': 64,
                'mlp_layers': [128, 64, 32],
                'learning_rate': 0.001,
                'batch_size': 256,
            })
            
            best_loss = float('inf')
            
            for epoch in range(epochs):
                # Training
                self.model.train()
                train_loss = 0
                for user_ids, item_ids, ratings in train_loader:
                    user_ids = user_ids.to(self.device)
                    item_ids = item_ids.to(self.device)
                    ratings = ratings.to(self.device)
                    
                    optimizer.zero_grad()
                    predictions = self.model(user_ids, item_ids)
                    loss = criterion(predictions, ratings)
                    loss.backward()
                    torch.nn.utils.clip_grad_norm_(self.model.parameters(), max_norm=1.0)
                    optimizer.step()
                    train_loss += loss.item()
                
                # Validation
                self.model.eval()
                val_loss = 0
                with torch.no_grad():
                    for user_ids, item_ids, ratings in test_loader:
                        user_ids = user_ids.to(self.device)
                        item_ids = item_ids.to(self.device)
                        ratings = ratings.to(self.device)
                        
                        predictions = self.model(user_ids, item_ids)
                        val_loss += criterion(predictions, ratings).item()
                
                train_loss /= len(train_loader)
                val_loss /= len(test_loader)
                
                scheduler.step(val_loss)
                
                mlflow.log_metrics({
                    'train_loss': train_loss,
                    'val_loss': val_loss,
                    'learning_rate': optimizer.param_groups[0]['lr'],
                }, step=epoch)
                
                if val_loss < best_loss:
                    best_loss = val_loss
                    torch.save(self.model.state_dict(), '/tmp/best_recommender.pt')
                
                if epoch % 10 == 0:
                    print(f"Epoch {epoch}: train_loss={train_loss:.4f}, val_loss={val_loss:.4f}")
            
            # Log model
            mlflow.pytorch.log_model(
                self.model,
                "recommender_model",
                registered_model_name="nidaw-restaurant-recommender",
            )
            
            # Save encoders
            joblib.dump(self.user_encoder, '/tmp/user_encoder.pkl')
            joblib.dump(self.restaurant_encoder, '/tmp/restaurant_encoder.pkl')
            mlflow.log_artifact('/tmp/user_encoder.pkl')
            mlflow.log_artifact('/tmp/restaurant_encoder.pkl')
            
            print(f"✅ Recommender trained! Best val_loss: {best_loss:.4f}")
            return self.model
    
    def recommend_for_user(self, user_id: str, top_k: int = 10, 
                          exclude_ordered: list = None,
                          context: dict = None) -> list:
        """Generate top-K restaurant recommendations for a user"""
        if user_id not in self.user_encoder.classes_:
            return self._get_popular_restaurants(top_k)
        
        user_encoded = self.user_encoder.transform([user_id])[0]
        
        # Score all restaurants
        all_restaurants = list(range(len(self.restaurant_encoder.classes_)))
        user_tensor = torch.LongTensor([user_encoded] * len(all_restaurants)).to(self.device)
        item_tensor = torch.LongTensor(all_restaurants).to(self.device)
        
        self.model.eval()
        with torch.no_grad():
            scores = self.model(user_tensor, item_tensor).cpu().numpy()
        
        # Create recommendations
        recommendations = []
        for idx, score in enumerate(scores):
            restaurant_id = self.restaurant_encoder.inverse_transform([idx])[0]
            
            # Skip already ordered restaurants
            if exclude_ordered and restaurant_id in exclude_ordered:
                continue
            
            recommendations.append({
                'restaurant_id': restaurant_id,
                'score': float(score),
            })
        
        # Sort by score
        recommendations.sort(key=lambda x: x['score'], reverse=True)
        
        # Apply context-based reranking
        if context:
            recommendations = self._apply_context_reranking(recommendations, context)
        
        return recommendations[:top_k]
    
    def _apply_context_reranking(self, recommendations: list, context: dict) -> list:
        """Rerank based on context (time, weather, location)"""
        hour = context.get('hour', 12)
        weather = context.get('weather', 'clear')
        
        for rec in recommendations:
            boost = 1.0
            
            # Time-based boosts
            if 7 <= hour <= 10:  # Breakfast
                if 'breakfast' in rec.get('tags', []):
                    boost *= 1.3
            elif 12 <= hour <= 14:  # Lunch
                if 'lunch' in rec.get('tags', []):
                    boost *= 1.2
            elif 18 <= hour <= 21:  # Dinner
                if 'dinner' in rec.get('tags', []):
                    boost *= 1.3
            
            # Weather-based boosts
            if weather in ['rain', 'snow']:
                if 'delivery_fast' in rec.get('tags', []):
                    boost *= 1.2
            
            rec['score'] *= boost
        
        recommendations.sort(key=lambda x: x['score'], reverse=True)
        return recommendations
    
    def _get_popular_restaurants(self, top_k: int) -> list:
        """Fallback: return popular restaurants"""
        # In production, query from cache or DB
        return []

if __name__ == "__main__":
    # Load order history
    orders_df = pd.read_parquet("data/order_history.parquet")
    
    recommender = RestaurantRecommender()
    train_loader, test_loader, num_users, num_restaurants = recommender.prepare_data(orders_df)
    recommender.train(train_loader, test_loader, num_users, num_restaurants)