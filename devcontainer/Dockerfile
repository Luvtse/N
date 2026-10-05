# ============================================================================
# NIDAW Development Container Image
# Multi-language development environment
# ============================================================================

FROM mcr.microsoft.com/devcontainers/base:ubuntu-22.04

# ============================================================================
# SYSTEM DEPENDENCIES
# ============================================================================

RUN apt-get update && apt-get install -y --no-install-recommends \
    # Build tools
    build-essential \
    gcc \
    g++ \
    make \
    cmake \
    pkg-config \
    # Networking
    curl \
    wget \
    net-tools \
    iputils-ping \
    dnsutils \
    # Version control
    git \
    git-lfs \
    # Editors
    vim \
    nano \
    # Utilities
    jq \
    yq \
    tree \
    htop \
    tmux \
    zip \
    unzip \
    tar \
    gzip \
    # Database clients
    postgresql-client-15 \
    redis-tools \
    # Python
    python3 \
    python3-pip \
    python3-venv \
    # Java (for Kafka tools)
    openjdk-17-jre-headless \
    # Protobuf
    protobuf-compiler \
    # SSL
    ca-certificates \
    openssl \
    # Misc
    sudo \
    less \
    file \
    && rm -rf /var/lib/apt/lists/*

# ============================================================================
# GO (Latest)
# ============================================================================

ENV GO_VERSION=1.21.6
ENV GOPATH=/go
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz \
    && mkdir -p $GOPATH \
    && go install github.com/air-verse/air@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest \
    && go install github.com/swaggo/swag/cmd/swag@latest \
    && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
    && go install github.com/pressly/goose/v3/cmd/goose@latest

# ============================================================================
# FLUTTER
# ============================================================================

ENV FLUTTER_VERSION=3.16.5
ENV FLUTTER_ROOT=/opt/flutter
ENV PATH=$FLUTTER_ROOT/bin:$FLUTTER_ROOT/bin/cache/dart-sdk/bin:$PATH

RUN git clone --depth 1 --branch ${FLUTTER_VERSION} https://github.com/flutter/flutter.git $FLUTTER_ROOT \
    && flutter precache \
    && flutter config --no-analytics \
    && dart --disable-analytics \
    && chown -R vscode:vscode $FLUTTER_ROOT

# ============================================================================
# PYTHON (ML/AI)
# ============================================================================

ENV PYTHON_VERSION=3.11
ENV VIRTUAL_ENV=/opt/venv
ENV PATH=$VIRTUAL_ENV/bin:$PATH

RUN python3 -m venv $VIRTUAL_ENV \
    && pip install --upgrade pip setuptools wheel \
    && pip install \
        # ML/AI
        torch==2.1.0 \
        xgboost==2.0.3 \
        scikit-learn==1.3.2 \
        pandas==2.1.4 \
        numpy==1.26.2 \
        # Feature Store
        feast==0.34.0 \
        # Experiment Tracking
        mlflow==2.9.2 \
        # Monitoring
        evidently==0.4.13 \
        prometheus-client==0.19.0 \
        # Data
        pyarrow==14.0.2 \
        psycopg2-binary==2.9.9 \
        redis==5.0.1 \
        # Utilities
        fastavro==1.9.0 \
        requests==2.31.0 \
        pyyaml==6.0.1 \
        black==23.12.0 \
        pylint==3.0.3 \
        pytest==7.4.3 \
        httpx==0.25.2 \
    && chown -R vscode:vscode $VIRTUAL_ENV

# ============================================================================
# NODE.JS (Blockchain, Frontend Tools)
# ============================================================================

ENV NODE_VERSION=20
ENV NVM_DIR=/home/vscode/.nvm

RUN curl -fsSL https://deb.nodesource.com/setup_${NODE_VERSION}.x | bash - \
    && apt-get install -y nodejs \
    && npm install -g \
        npm@latest \
        yarn@latest \
        pnpm@latest \
        typescript@latest \
        ts-node@latest \
        # Hardhat (Blockchain)
        hardhat@latest \
        # k6 (Load testing)
        @grafana/k6@latest \
    && rm -rf /var/lib/apt/lists/*

# ============================================================================
# KAFKA CLI TOOLS
# ============================================================================

ENV KAFKA_VERSION=3.5.1
ENV KAFKA_HOME=/opt/kafka
ENV PATH=$KAFKA_HOME/bin:$PATH

RUN curl -fsSL "https://archive.apache.org/dist/kafka/${KAFKA_VERSION}/kafka_2.13-${KAFKA_VERSION}.tgz" \
    | tar -xz -C /opt \
    && mv /opt/kafka_2.13-${KAFKA_VERSION} $KAFKA_HOME

# ============================================================================
# ADDITIONAL TOOLS
# ============================================================================

# Trivy (Security scanning)
RUN curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh \
    | sh -s -- -b /usr/local/bin v0.48.0

# Gitleaks (Secret detection)
RUN curl -sfL https://github.com/gitleaks/gitleaks/releases/download/v8.18.1/gitleaks_8.18.1_linux_x64.tar.gz \
    | tar -xz -C /usr/local/bin gitleaks

# Terraform
ARG TERRAFORM_VERSION=1.5.7
RUN curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" -o terraform.zip \
    && unzip terraform.zip -d /usr/local/bin \
    && rm terraform.zip

# kubectl
RUN curl -fsSL "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    -o /usr/local/bin/kubectl \
    && chmod +x /usr/local/bin/kubectl

# Helm
RUN curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# AWS CLI
RUN curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o awscliv2.zip \
    && unzip -q awscliv2.zip \
    && ./aws/install \
    && rm -rf awscliv2.zip aws

# Docker Compose (standalone)
RUN curl -fsSL "https://github.com/docker/compose/releases/download/v2.23.3/docker-compose-linux-x86_64" \
    -o /usr/local/bin/docker-compose \
    && chmod +x /usr/local/bin/docker-compose

# ============================================================================
# USER SETUP
# ============================================================================

# Ensure vscode user has proper permissions
RUN usermod -aG docker vscode \
    && echo "vscode ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers.d/vscode

# Create workspace directory
RUN mkdir -p /workspace && chown vscode:vscode /workspace

# Set default shell
ENV SHELL=/bin/bash

# ============================================================================
# HEALTH CHECK
# ============================================================================

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD echo "healthy" || exit 1

# ============================================================================
# ENTRYPOINT
# ============================================================================

CMD ["sleep", "infinity"]