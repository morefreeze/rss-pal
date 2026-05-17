# Build stage
FROM node:18-alpine AS builder

WORKDIR /app

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install

COPY frontend/ .
RUN npm run build

# Runtime stage
FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

# Bundle the browser extension as a downloadable zip
RUN apk add --no-cache zip
COPY extension/ /tmp/extension/
RUN cd /tmp && zip -r /usr/share/nginx/html/rss-pal-extension.zip extension/ && rm -rf /tmp/extension

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
