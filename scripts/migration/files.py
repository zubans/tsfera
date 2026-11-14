"""Validate and copy file payloads without overwriting existing storage."""
from __future__ import annotations
from pathlib import Path
import hashlib
import shutil


def safe_file(root: Path, key: str) -> Path:
    relative = Path(key)
    if not key or relative.is_absolute() or '..' in relative.parts:
        raise ValueError('Unsafe storage key in dump')
    resolved = (root / relative).resolve()
    if root.resolve() not in resolved.parents:
        raise ValueError('Storage key escapes configured root')
    return resolved


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def transfer_files(records: list[dict], source: Path, destination: Path | None) -> list[dict]:
    """Missing files fail before creating destination. IDs/hashes only in report."""
    verified = []
    for row in records:
        path = safe_file(source, row['storage_path'])
        if not path.is_file() or path.stat().st_size != row['size_bytes']:
            raise ValueError('Missing or size-mismatched file ID '+str(row['id']))
        verified.append(dict(id=row['id'], size=row['size_bytes'], sha256=sha256(path)))
    if destination is not None:
        destination.mkdir(mode=0o700, parents=False, exist_ok=False)
        copied = set()
        for row, check in zip(records, verified):
            src = safe_file(source, row['storage_path'])
            dst = safe_file(destination, row['storage_path'])
            dst.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            if dst not in copied:
                with src.open('rb') as reader, dst.open('xb') as writer:
                    shutil.copyfileobj(reader, writer)
                copied.add(dst)
            if dst.stat().st_size != check['size'] or sha256(dst) != check['sha256']:
                raise ValueError('File changed during copy: ID '+str(row['id']))
    return verified
