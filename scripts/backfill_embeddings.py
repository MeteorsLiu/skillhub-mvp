#!/usr/bin/env python3
import argparse
import os
import sys
from typing import Iterable

import psycopg
from psycopg.rows import dict_row
import requests


def parse_args():
    parser = argparse.ArgumentParser(description="Backfill SkillHub pgvector embeddings.")
    parser.add_argument("--database-url", default=os.getenv("DATABASE_URL"), help="PostgreSQL DSN")
    parser.add_argument("--embed-url", default=os.getenv("SKILLHUB_EMBED_URL", "http://127.0.0.1:8397/v1/embed"))
    parser.add_argument("--batch-size", type=int, default=64)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--force", action="store_true", help="Recompute rows that already have embeddings")
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def batched(items: list[dict], size: int) -> Iterable[list[dict]]:
    for i in range(0, len(items), size):
        yield items[i : i + size]


def skill_text(row: dict) -> str:
    parts = []
    if row["id"]:
        parts.append(f"id: {row['id']}")
    if row["name"]:
        parts.append(f"name: {row['name']}")
    if row["description"]:
        parts.append(f"description: {row['description']}")
    if row["tags"]:
        parts.append(f"tags: {', '.join(row['tags'])}")
    return "\n".join(parts)


def vector_literal(values: list[float]) -> str:
    return "[" + ",".join(str(float(value)) for value in values) + "]"


def fetch_rows(conn: psycopg.Connection, force: bool, limit: int) -> list[dict]:
    where = "status = 'approved'"
    if not force:
        where += " AND embedding IS NULL"
    sql = f"""
        SELECT id, name, description, version, tags
        FROM skill_models
        WHERE {where}
        ORDER BY id ASC
    """
    if limit > 0:
        sql += " LIMIT %s"
        params = (limit,)
    else:
        params = ()
    with conn.cursor(row_factory=dict_row) as cur:
        cur.execute(sql, params)
        return list(cur.fetchall())


def embed(embed_url: str, texts: list[str]) -> list[list[float]]:
    resp = requests.post(embed_url, json={"input": texts}, timeout=60)
    resp.raise_for_status()
    payload = resp.json()
    embeddings = payload.get("embeddings")
    if not isinstance(embeddings, list) or len(embeddings) != len(texts):
        raise RuntimeError(f"bad embedding response: {payload!r}")
    return embeddings


def main() -> int:
    args = parse_args()
    if not args.database_url:
        print("DATABASE_URL or --database-url is required", file=sys.stderr)
        return 2
    if args.batch_size <= 0:
        print("--batch-size must be > 0", file=sys.stderr)
        return 2

    with psycopg.connect(args.database_url) as conn:
        rows = fetch_rows(conn, args.force, args.limit)
        print(f"rows to backfill: {len(rows)}")
        if args.dry_run:
            return 0

        updated = 0
        for batch in batched(rows, args.batch_size):
            texts = [skill_text(row) for row in batch]
            embeddings = embed(args.embed_url, texts)
            with conn.cursor() as cur:
                for row, embedding in zip(batch, embeddings):
                    cur.execute(
                        "UPDATE skill_models SET embedding = %s::vector WHERE id = %s",
                        (vector_literal(embedding), row["id"]),
                    )
            conn.commit()
            updated += len(batch)
            print(f"updated {updated}/{len(rows)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
