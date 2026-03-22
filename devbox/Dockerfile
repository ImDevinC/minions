# Devbox image for running minion tasks
# Built on golang:1.22-bookworm for Go toolchain + Debian-based packages

FROM golang:1.22-bookworm AS base

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    jq \
    ca-certificates \
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
RUN groupadd -g 1000 minion \
    && useradd -u 1000 -g minion -m -s /bin/bash minion

# Create workspace directory
RUN mkdir -p /workspace && chown minion:minion /workspace

# Set working directory
WORKDIR /workspace

# Switch to non-root user
USER minion

# Verify installations
RUN git --version \
    && curl --version | head -n1 \
    && jq --version \
    && gh --version | head -n1 \
    && opencode --version

# Default command (overridden by entrypoint.sh in production)
CMD ["bash"]
