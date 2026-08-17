FROM python:3.12-slim

WORKDIR /app

COPY frontend_requirements.txt .
RUN pip install --no-cache-dir -r frontend_requirements.txt

COPY frontend_server.py .
COPY templates ./templates
COPY static ./static

ENV PORT=5000 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 5000

CMD ["python", "frontend_server.py"]
