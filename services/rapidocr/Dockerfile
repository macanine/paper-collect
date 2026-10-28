FROM python:3.12-slim-bookworm

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    OMP_NUM_THREADS=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends libglib2.0-0 libgl1 libgomp1 \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --upgrade pip \
    && pip install \
        "fastapi==0.115.12" \
        "uvicorn[standard]==0.34.2" \
        "python-multipart==0.0.20" \
        "PyMuPDF==2.0.0" \
        "numpy<3" \
        "Pillow==11.2.1" \
        "rapidocr==3.10.0" \
        "onnxruntime==1.31.1"

WORKDIR /app
COPY app.py /app/app.py

RUN useradd --system --uid 10001 --home-dir /app --no-create-home rapidocr \
    && mkdir -p /var/lib/rapidocr/jobs \
    && chown -R rapidocr:rapidocr /var/lib/rapidocr /app

USER rapidocr
EXPOSE 9003

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "9003", "--workers", "1", "--no-access-log"]
