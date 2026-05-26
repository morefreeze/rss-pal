FROM python:3.12-alpine
WORKDIR /app
COPY server.py .
RUN mkdir -p /data
EXPOSE 8090
CMD ["python", "server.py"]
