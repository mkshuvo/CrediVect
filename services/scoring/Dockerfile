FROM python:3.12.9-slim-bookworm

WORKDIR /app

# Install security updates and build requirements
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

# Run model pre-compilation/initialization
RUN python -c "from server import ScoringService; s = ScoringService()"

EXPOSE 50051

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
    CMD python -c "import socket; s = socket.socket(); s.connect(('127.0.0.1', 50051)); s.close()" || exit 1

CMD ["python", "server.py"]
