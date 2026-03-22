# Control panel Dockerfile
# Multi-stage build: Node 20 builder -> Node 20 Alpine runtime with Next.js standalone

# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files first for better layer caching
COPY package.json package-lock.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Build the Next.js app (outputs to .next/standalone with output: 'standalone')
RUN npm run build

# Runtime stage
FROM node:20-alpine AS runner

# OCI image labels
LABEL org.opencontainers.image.source="https://github.com/ImDevinC/minions"
LABEL org.opencontainers.image.description="Control panel for minions"
LABEL org.opencontainers.image.licenses="MIT"

WORKDIR /app

# Don't run as root
RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Copy standalone output
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static

# Create public directory (may be empty if no static assets)
RUN mkdir -p ./public

# Set ownership
RUN chown -R nextjs:nodejs /app

USER nextjs

# Next.js standalone server listens on 3000 by default
EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"
ENV NODE_ENV=production

CMD ["node", "server.js"]
