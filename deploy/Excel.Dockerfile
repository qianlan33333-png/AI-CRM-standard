FROM python:3.13-slim-bookworm
WORKDIR /app
COPY components/excel-batches/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt && useradd -u 10001 -m crm && mkdir /data && chown crm /data
COPY components/excel-batches/batches.py .
USER 10001:10001
CMD ["python", "batches.py", "--config", "/config.json", "--database", "/data/batches.sqlite", "--port", "8791"]
