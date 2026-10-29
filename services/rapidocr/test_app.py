"""Unit tests for page extraction. The OCR engine is faked; PyMuPDF and Pillow
process synthetic files, so no LibreOffice or model download is required."""

import importlib.util
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

import fitz
from PIL import Image


# Avoid loading inference models; these tests exercise page routing only.
sys.modules["rapidocr"] = types.SimpleNamespace(RapidOCR=lambda: object())
spec = importlib.util.spec_from_file_location("rapidocr_app_under_test", Path(__file__).with_name("app.py"))
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


class PageExtractionTest(unittest.TestCase):
    def test_pdf_pages_are_rendered_in_order(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            document = fitz.open()
            for text in ("第一页", "第二页"):
                page = document.new_page(width=200, height=200)
                page.insert_text((30, 50), text)
            source = root / "sample.pdf"
            document.save(source)
            document.close()
            output = root / "result.jsonl"
            with patch.object(app, "_ocr_image", side_effect=[["第一页"], ["第二页"]]) as ocr:
                pages = app._write_pdf(source, output)
            self.assertEqual(pages, 2)
            rows = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
            self.assertEqual([row["page_no"] for row in rows], [1, 2])
            self.assertEqual([row["texts"] for row in rows], [["第一页"], ["第二页"]])
            self.assertEqual(ocr.call_count, 2)

    def test_image_upload_becomes_one_page(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image = root / "scan.png"
            Image.new("RGB", (100, 100), "white").save(image)
            output = root / "result.jsonl"
            with patch.object(app, "_ocr_image", return_value=["扫描文字"]) as ocr:
                pages = app._write_pdf_pages(image, output)
            self.assertEqual(pages, 1)
            self.assertEqual(
                json.loads(output.read_text(encoding="utf-8")),
                {"page_no": 1, "texts": ["扫描文字"]},
            )
            self.assertEqual(ocr.call_count, 1)

    def test_multi_frame_image_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "frames.tif"
            first = Image.new("RGB", (20, 20), "white")
            second = Image.new("RGB", (20, 20), "black")
            first.save(source, save_all=True, append_images=[second])
            with self.assertRaises(ValueError):
                app._write_pdf_pages(source, root / "result.jsonl")


if __name__ == "__main__":
    unittest.main()
