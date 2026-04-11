# Devbox image for running minion tasks
# Built on debian:bookworm-slim with explicit Go and Node.js installations

FROM debian:bookworm-slim AS base

# Build arguments for toolchain versions
ARG GO_VERSION=1.26.1
ARG NODE_MAJOR=24

# OCI image labels
LABEL org.opencontainers.image.source="https://github.com/ImDevinC/minions"
LABEL org.opencontainers.image.description="Devbox environment for minions"
LABEL org.opencontainers.image.licenses="MIT"

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    wget \
    gnupg \
    jq \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Docker CLI (client only, no daemon)
# The Docker CLI can connect to remote Docker daemons or be used with DinD sidecars
RUN curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian bookworm stable" \
    | tee /etc/apt/sources.list.d/docker.list > /dev/null \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli docker-buildx-plugin \
    && rm -rf /var/lib/apt/lists/*

# Install Buildah and Skopeo for rootless container image building and inspection
# Buildah can build OCI/Docker images without requiring a daemon or root privileges
# Skopeo can inspect and copy container images for validation
# libcap2-bin provides setcap command for setting file capabilities
RUN apt-get update && apt-get install -y --no-install-recommends \
    buildah \
    skopeo \
    fuse-overlayfs \
    uidmap \
    libcap2-bin \
    && rm -rf /var/lib/apt/lists/*

# Set file capabilities on newuidmap/newgidmap for rootless user namespace mapping
# These binaries need CAP_SETUID and CAP_SETGID to write to /proc/self/uid_map
# File capabilities work with non-root users (unlike pod-level capabilities)
RUN setcap cap_setuid,cap_setgid=ep /usr/bin/newuidmap \
    && setcap cap_setuid,cap_setgid=ep /usr/bin/newgidmap

# Install Go
RUN ARCH=$(dpkg --print-architecture) \
    && case "$ARCH" in \
        amd64) GO_ARCH="amd64" ;; \
        arm64) GO_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac \
    && wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -O /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt

# Install Node.js and npm
RUN curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
# Using official method from https://github.com/cli/cli/blob/trunk/docs/install_linux.md
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Install OpenCode binary
# Using latest release from GitHub
# Asset naming: opencode-linux-x64.tar.gz (amd64), opencode-linux-arm64.tar.gz (arm64)
ARG OPENCODE_VERSION=latest
RUN ARCH=$(dpkg --print-architecture) \
    && case "$ARCH" in \
        amd64) OC_ARCH="x64" ;; \
        arm64) OC_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac \
    && ASSET_NAME="opencode-linux-${OC_ARCH}.tar.gz" \
    && if [ "$OPENCODE_VERSION" = "latest" ]; then \
        DOWNLOAD_URL=$(curl -fsSL https://api.github.com/repos/anomalyco/opencode/releases/latest \
            | jq -r --arg name "$ASSET_NAME" '.assets[] | select(.name == $name) | .browser_download_url'); \
    else \
        DOWNLOAD_URL="https://github.com/anomalyco/opencode/releases/download/${OPENCODE_VERSION}/${ASSET_NAME}"; \
    fi \
    && echo "Downloading OpenCode from: $DOWNLOAD_URL" \
    && curl -fsSL "$DOWNLOAD_URL" -o /tmp/opencode.tar.gz \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin \
    && chmod +x /usr/local/bin/opencode \
    && rm /tmp/opencode.tar.gz

# Create non-root user with UID 1000
# This matches the UID used by most container runtimes and avoids permission issues
# Add subordinate UID/GID ranges for rootless container user namespace mapping
# Gives minion user access to UIDs/GIDs 100000-165535 for buildah/podman
RUN groupadd -g 1000 minion \
    && useradd -u 1000 -g minion -m -s /bin/bash minion \
    && echo "minion:100000:65536" >> /etc/subuid \
    && echo "minion:100000:65536" >> /etc/subgid \
    && echo "root:100000:65536" >> /etc/subuid \
    && echo "root:100000:65536" >> /etc/subgid

# Create workspace directory
RUN mkdir -p /workspace

# Copy OpenCode configuration to /etc/opencode (read-only in image)
# entrypoint.sh copies this to ~/.config/opencode/ at runtime since
# /home/minion is mounted as an emptyDir volume for writable home directory
COPY config/ /etc/opencode/

# Copy entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Set working directory
WORKDIR /workspace

# Verify installations
RUN git --version \
    && curl --version | head -n1 \
    && jq --version \
    && go version \
    && node --version \
    && npm --version \
    && gh --version | head -n1 \
    && opencode --version \
    && docker --version \
    && docker buildx version \
    && buildah --version \
    && skopeo --version

# Use entrypoint script
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
