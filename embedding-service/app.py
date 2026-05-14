import os

from fastembed import TextEmbedding
from flask import Flask, jsonify, request


MODEL_NAME = os.getenv(
    "SKILLHUB_EMBED_MODEL",
    "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
)
CACHE_DIR = os.getenv("FASTEMBED_CACHE_DIR", "/var/lib/skillhub/fastembed-cache")

app = Flask(__name__)
model = TextEmbedding(model_name=MODEL_NAME, cache_dir=CACHE_DIR)


@app.post("/v1/embed")
def embed():
    payload = request.get_json(silent=True) or {}
    value = payload.get("input")
    if isinstance(value, str):
        texts = [value]
    elif isinstance(value, list) and all(isinstance(item, str) for item in value):
        texts = value
    else:
        return jsonify({"error": "input must be a string or an array of strings"}), 400

    if not texts:
        return jsonify({"embeddings": []})

    embeddings = [embedding.tolist() for embedding in model.embed(texts)]
    return jsonify({"embeddings": embeddings})


@app.get("/health")
def health():
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    host = os.getenv("HOST", "127.0.0.1")
    port = int(os.getenv("PORT", "8397"))
    app.run(host=host, port=port)
