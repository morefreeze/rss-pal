# Build stage
FROM node:22-alpine AS builder

WORKDIR /app

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --registry=https://registry.npmmirror.com --no-audit --no-fund

COPY frontend/ .
RUN npm run build
COPY extension/ ./extension/
RUN node ./scripts/create-extension-zip.mjs ./extension ./rss-pal-extension.zip

# Runtime stage
FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY --from=builder /app/rss-pal-extension.zip /usr/share/nginx/html/rss-pal-extension.zip
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
