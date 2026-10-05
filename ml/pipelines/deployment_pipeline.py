"""
NIDAW Model Deployment Pipeline
Automated model deployment with validation and rollback
"""

import os
import logging
from typing import Dict, Any, Optional
from datetime import datetime
import mlflow
from mlflow.tracking import MlflowClient
import subprocess
import json

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


# ============================================================================
# DEPLOYMENT PIPELINE
# ============================================================================

class ModelDeploymentPipeline:
    """Automated model deployment pipeline"""
    
    def __init__(
        self,
        mlflow_uri: str = "http://mlflow:5000",
        serving_platform: str = "torchserve",  # or "ray"
    ):
        self.mlflow_client = MlflowClient(tracking_uri=mlflow_uri)
        self.serving_platform = serving_platform
    
    def deploy_model(
        self,
        model_name: str,
        stage: str = "Staging",
        validate: bool = True,
    ) -> Dict[str, Any]:
        """Deploy model to specified stage"""
        
        logger.info(f"🚀 Starting deployment of {model_name} to {stage}")
        
        try:
            # Get model version
            model_version = self._get_latest_version(model_name, stage)
            
            if not model_version:
                logger.error(f"❌ No model version found for {model_name} in {stage}")
                return {"status": "error", "message": "Model not found"}
            
            # Validate model
            if validate:
                validation_result = self._validate_model(model_name, model_version.version)
                if not validation_result["passed"]:
                    logger.error(f"❌ Model validation failed: {validation_result['issues']}")
                    return {"status": "error", "message": "Validation failed", "details": validation_result}
            
            # Deploy to serving platform
            deployment_result = self._deploy_to_platform(model_name, model_version.version)
            
            if deployment_result["status"] != "success":
                logger.error(f"❌ Deployment failed: {deployment_result}")
                return deployment_result
            
            # Run smoke tests
            smoke_test_result = self._run_smoke_tests(model_name)
            
            if not smoke_test_result["passed"]:
                logger.warning(f"⚠️ Smoke tests failed, rolling back")
                self._rollback_deployment(model_name)
                return {"status": "error", "message": "Smoke tests failed", "details": smoke_test_result}
            
            # Update model stage
            self.mlflow_client.transition_model_version_stage(
                name=model_name,
                version=model_version.version,
                stage="Production",
                archive_existing_versions=True,
            )
            
            logger.info(f"✅ Successfully deployed {model_name} v{model_version.version}")
            
            return {
                "status": "success",
                "model_name": model_name,
                "version": model_version.version,
                "stage": "Production",
                "timestamp": datetime.utcnow().isoformat(),
            }
            
        except Exception as e:
            logger.error(f"❌ Deployment pipeline failed: {e}")
            return {"status": "error", "message": str(e)}
    
    def _get_latest_version(self, model_name: str, stage: str):
        """Get latest model version for stage"""
        versions = self.mlflow_client.get_latest_versions(model_name, stages=[stage])
        return versions[0] if versions else None
    
    def _validate_model(self, model_name: str, version: str) -> Dict[str, Any]:
        """Validate model before deployment"""
        
        issues = []
        
        try:
            # Load model
            model_uri = f"models:/{model_name}/{version}"
            model = mlflow.pyfunc.load_model(model_uri)
            
            # Check model artifacts
            run = self.mlflow_client.get_run(model.run_id)
            
            # Check metrics
            metrics = run.data.metrics
            if "mae" in metrics and metrics["mae"] > 5.0:
                issues.append(f"MAE too high: {metrics['mae']}")
            
            if "r2_score" in metrics and metrics["r2_score"] < 0.8:
                issues.append(f"R² score too low: {metrics['r2_score']}")
            
            # Check model size
            artifacts = self.mlflow_client.list_artifacts(model.run_id)
            total_size = sum(artifact.file_size or 0 for artifact in artifacts)
            if total_size > 1e9:  # 1GB
                issues.append(f"Model size too large: {total_size / 1e9:.2f}GB")
            
            return {
                "passed": len(issues) == 0,
                "issues": issues,
            }
            
        except Exception as e:
            return {
                "passed": False,
                "issues": [f"Validation error: {str(e)}"],
            }
    
    def _deploy_to_platform(self, model_name: str, version: str) -> Dict[str, Any]:
        """Deploy model to serving platform"""
        
        if self.serving_platform == "torchserve":
            return self._deploy_to_torchserve(model_name, version)
        elif self.serving_platform == "ray":
            return self._deploy_to_ray(model_name, version)
        else:
            return {"status": "error", "message": f"Unknown platform: {self.serving_platform}"}
    
    def _deploy_to_torchserve(self, model_name: str, version: str) -> Dict[str, Any]:
        """Deploy to TorchServe"""
        
        try:
            # Export model to TorchServe format
            model_uri = f"models:/{model_name}/{version}"
            export_path = f"/tmp/{model_name}_v{version}.mar"
            
            # Use mlflow-torchserve plugin
            cmd = [
                "mlflow", "torchserve", "generate-mar-file",
                "--model-uri", model_uri,
                "--output-file", export_path,
            ]
            
            result = subprocess.run(cmd, capture_output=True, text=True, check=True)
            
            # Register model with TorchServe
            cmd = [
                "curl", "-X", "POST",
                "http://torchserve:8081/models",
                "-F", f"url=file://{export_path}",
            ]
            
            result = subprocess.run(cmd, capture_output=True, text=True, check=True)
            
            return {"status": "success", "platform": "torchserve"}
            
        except subprocess.CalledProcessError as e:
            return {"status": "error", "message": e.stderr}
        except Exception as e:
            return {"status": "error", "message": str(e)}
    
    def _deploy_to_ray(self, model_name: str, version: str) -> Dict[str, Any]:
        """Deploy to Ray Serve"""
        
        try:
            # Ray Serve handles deployment automatically via config
            # Just need to update the deployment configuration
            
            config = {
                "model_name": model_name,
                "model_version": version,
                "num_replicas": 4,
            }
            
            # Save config
            config_path = f"/tmp/ray_deployment_{model_name}.json"
            with open(config_path, "w") as f:
                json.dump(config, f)
            
            # Trigger Ray Serve update
            cmd = [
                "python", "-m", "ray.serve.scripts.deploy",
                "--config", config_path,
            ]
            
            result = subprocess.run(cmd, capture_output=True, text=True, check=True)
            
            return {"status": "success", "platform": "ray"}
            
        except subprocess.CalledProcessError as e:
            return {"status": "error", "message": e.stderr}
        except Exception as e:
            return {"status": "error", "message": str(e)}
    
    def _run_smoke_tests(self, model_name: str) -> Dict[str, Any]:
        """Run smoke tests on deployed model"""
        
        try:
            # Send test request
            test_data = self._get_test_data(model_name)
            
            if self.serving_platform == "torchserve":
                url = f"http://torchserve:8080/predictions/{model_name}"
            else:
                url = f"http://ray-serve:8000/{model_name}"
            
            cmd = [
                "curl", "-X", "POST", url,
                "-H", "Content-Type: application/json",
                "-d", json.dumps(test_data),
            ]
            
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)
            
            # Check response
            if result.returncode != 0:
                return {"passed": False, "message": "Request failed"}
            
            response = json.loads(result.stdout)
            
            if "error" in response:
                return {"passed": False, "message": response["error"]}
            
            return {"passed": True, "response": response}
            
        except subprocess.TimeoutExpired:
            return {"passed": False, "message": "Timeout"}
        except Exception as e:
            return {"passed": False, "message": str(e)}
    
    def _get_test_data(self, model_name: str) -> Dict[str, Any]:
        """Get test data for smoke tests"""
        
        test_data = {
            "eta_prediction": {
                "pickup_lat": 40.7128,
                "pickup_lng": -74.0060,
                "dropoff_lat": 40.7589,
                "dropoff_lng": -73.9851,
                "distance_km": 5.2,
            },
            "demand_forecasting": {
                "h3_cell": "h3_40712_-74006",
                "hour": 12,
                "day_of_week": 3,
            },
        }
        
        return test_data.get(model_name, {})
    
    def _rollback_deployment(self, model_name: str):
        """Rollback to previous version"""
        
        try:
            # Get previous production version
            versions = self.mlflow_client.get_latest_versions(model_name, stages=["Production"])
            
            if len(versions) < 2:
                logger.warning("⚠️ No previous version to rollback to")
                return
            
            previous_version = versions[1]
            
            # Redeploy previous version
            self._deploy_to_platform(model_name, previous_version.version)
            
            logger.info(f"✅ Rolled back to {model_name} v{previous_version.version}")
            
        except Exception as e:
            logger.error(f"❌ Rollback failed: {e}")


# ============================================================================
# MAIN
# ============================================================================

if __name__ == "__main__":
    # Example usage
    pipeline = ModelDeploymentPipeline()
    
    # Deploy model
    result = pipeline.deploy_model("eta_prediction", stage="Staging")
    print(json.dumps(result, indent=2))