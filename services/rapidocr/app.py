from __future__ import annotations

import json
import os
import re
import shutil
import threading
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

import fitz
import numpy as np
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import FileResponse
from PIL import Image, ImageOps
from rapidocr import RapidOCR
from starlette.concurrency import run_in_threadpool

MAX_UPLOAD_BYTES = 256 * 1024 * 1024
MAX_PDF_PAGES = 1000
PDF_RENDER_DPI = 220
JOB_ROOT = Path(os.getenv("RAPIDOCR_JOB_DIR", "/var/lib/rapidocr/jobs"))
MAX_WORKERS = max(1, int(os.getenv("RAPIDOCR_WORKERS", "1")))

app = FastAPI(title="Paper Collect RapidOCR", docs_url=None, redoc_url=None)
engine = RapidOCR()
_engine_lock = threading.Lock()
_executor = ThreadPoolExecutor(max_workers=MAX_WORKERS, thread_name_prefix="rapidocr")
_jobs: dict[str, dict[str, Any]] = {}
_jobs_lock = threading.Lock()


def _set_job(job_id: str, **changes: Any) -> None:
    with _jobs_lock:
        _jobs.setdefault(job_id, {}).update(changes)


def _get_job(job_id: str) -> dict[str, Any] | None:
    with _jobs_lock:
        job = _jobs.get(job_id)
        return dict(job) if job else None


def _safe_filename(name: str) -> str:
    name = Path(name or "upload.pdf").name
    name = re.sub(r"[\x00-\x1f]", "_", name)
    return name[:200] or "upload.pdf"


def _ocr_image(image: Image.Image) -> list[str]:
    image = ImageOps.exif_transpose(image).convert("RGB")
    # RapidOCR's inference session is stateful; serialize calls within a
    # process. Scale capacity with separate OCR worker containers.
    with _engine_lock:
        result = engine(np.asarray(image))
    if result.txts is None:
        return []
    return [str(text).strip() for text in result.txts if str(text).strip()]


def _write_pdf_pages(path: Path, output: Path) -> int:
    with Image.open(path) as image:
        if getattr(image, "n_frames", 1) > 1:
            raise ValueError("不支持多页图片，请上传 PDF")
        texts = _ocr_image(image)
    with output.open("w", encoding="utf-8") as stream:
        stream.write(json.dumps({"page_no": 1, "texts": texts}, ensure_ascii=False) + "\n")
    return 1


def _write_pdf(path: Path, output: Path) -> int:
    page_count = 0
    with fitz.open(path) as document:
        if document.page_count == 0:
            raise ValueError("PDF 没有页面")
        if document.page_count > MAX_PDF_PAGES:
            raise ValueError(f"PDF 超过 {MAX_PDF_PAGES} 页")
        with output.open("w", encoding="utf-8") as stream:
            for index, page in enumerate(document):
                pixmap = page.get_pixmap(dpi=PDF_RENDER_DPI, alpha=False)
                image = Image.frombytes("RGB", (pixmap.width, pixmap.height), pixmap.samples)
                texts = _ocr_image(image)
                payload = {"page_no": index + 1, "texts": texts}
                stream.write(json.dumps(payload, ensure_ascii=False) + "\n")
                page_count += 1
    return page_count


def _run_job(job_id: str, path: Path, output: Path) -> None:
    _set_job(job_id, state="running")
    try:
        if path.suffix.lower() == ".pdf":
            pages = _write_pdf(path, output)
        else:
            pages = _write_pdf_pages(path, output)
        _set_job(job_id, state="done", result_url=f"/api/v1/ocr/jobs/{job_id}/result", pages=pages)
    except Exception as exc:
        _set_job(job_id, state="failed", error=str(exc)[:240])
    finally:
        path.unlink(missing_ok=True)


@app.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/api/v1/ocr/jobs", status_code=202)
async def submit(file: UploadFile = File(...), model: str = Form("PP-OCRv5")) -> dict[str, str]:
    del model  # RapidOCR ships a fixed local model; kept for API compatibility.
    job_id = str(uuid.uuid4())
    job_dir = JOB_ROOT / job_id
    job_dir.mkdir(parents=True, exist_ok=False, mode=0o700)
    suffix = Path(_safe_filename(file.filename or "upload")).suffix.lower()
    if suffix not in {".pdf", ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".tif", ".tiff"}:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise HTTPException(status_code=415, detail="仅支持 PDF、PNG、JPEG、WebP、BMP 和 TIFF")
    path = job_dir / ("source" + suffix)
    size = 0
    try:
        with path.open("wb") as stream:
            while chunk := await file.read(1024 * 1024):
                size += len(chunk)
                if size > MAX_UPLOAD_BYTES:
                    raise HTTPException(status_code=413, detail="文件超过 256 MiB")
                await run_in_threadpool(stream.write, chunk)
    except Exception:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise
    finally:
        await file.close()
    _set_job(job_id, state="pending", error="", result_url="")
    _executor.submit(_run_job, job_id, path, job_dir / "result.jsonl")
    return {"job_id": job_id}


@app.get("/api/v1/ocr/jobs/{job_id}")
def status(job_id: str) -> dict[str, Any]:
    job = _get_job(job_id)
    if job is None:
        raise HTTPException(status_code=404, detail="任务不存在")
    return {
        "state": job["state"],
        "error": job.get("error", ""),
        "result_url": job.get("result_url", ""),
        "pages": job.get("pages", 0),
    }


@app.get("/api/v1/ocr/jobs/{job_id}/result")
def result(job_id: str) -> FileResponse:
    job = _get_job(job_id)
    if job is None:
        raise HTTPException(status_code=404, detail="任务不存在")
    if job["state"] != "done":
        raise HTTPException(status_code=409, detail="任务尚未完成")
    path = JOB_ROOT / job_id / "result.jsonl"
    if not path.is_file():
        raise HTTPException(status_code=404, detail="结果不存在")
    return FileResponse(path, media_type="application/x-ndjson", filename=f"{job_id}.jsonl")
