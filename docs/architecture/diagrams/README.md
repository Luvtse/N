
---

## 📄 File #5: `docs/architecture/diagrams/README.md`

```markdown
# Architecture Diagrams

This directory contains visual representations of the NIDAW architecture.

## 📊 Available Diagrams

| Diagram | Description | Format |
|---------|-------------|--------|
| [architecture.png](architecture.png) | High-level system architecture | PNG |
| [data-flow.png](data-flow.png) | Data flow through the system | PNG |
| [deployment.png](deployment.png) | Deployment topology | PNG |

## 🎨 Diagram Sources

Diagrams are created using:
- **Excalidraw** - Hand-drawn style diagrams
- **Mermaid** - Text-based diagrams (see below)
- **PlantUML** - UML diagrams
- **Draw.io** - Technical diagrams

## 📝 Mermaid Diagrams

For easy editing, we maintain Mermaid source files:

### System Architecture (Mermaid)

```mermaid
graph TB
    subgraph Edge["Global Edge"]
        CF[Cloudflare CDN/WAF]
    end
    
    subgraph NA["NA-East (Primary)"]
        K8S_NA[Kubernetes Cluster]
        GW_NA[API Gateway]
        APP_NA[NIDAW App]
        DB_NA[(PostgreSQL)]
        K_NA[Kafka]
    end
    
    subgraph EU["EU-West (Secondary)"]
        K8S_EU[Kubernetes Cluster]
        GW_EU[API Gateway]
        APP_EU[NIDAW App]
        DB_EU[(PostgreSQL)]
        K_EU[Kafka]
    end
    
    CF --> K8S_NA
    CF --> K8S_EU
    K8S_NA --> GW_NA --> APP_NA
    K8S_EU --> GW_EU --> APP_EU
    APP_NA --> DB_NA
    APP_NA --> K_NA
    APP_EU --> DB_EU
    APP_EU --> K_EU
    K_NA <-->|MirrorMaker 2| K_EU

