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


# В исходном хранилище часть картинок лежит с другим написанием расширения,
# чем в ref_file.storage_key; содержимое при этом то же (сверяется размер).
EXTENSION_ALIASES = {'.jpg': ('.jpeg',), '.jpeg': ('.jpg',)}


def find_source(root: Path, key: str, size: int) -> tuple[Path | None, bool]:
    """Returns the payload path and whether the extension alias was used."""
    path = safe_file(root, key)
    if path.is_file() and path.stat().st_size == size:
        return path, False
    for alias in EXTENSION_ALIASES.get(path.suffix.lower(), ()):
        candidate = safe_file(root, str(Path(key).with_suffix(alias)))
        if candidate.is_file() and candidate.stat().st_size == size:
            return candidate, True
    return None, False


def transfer_files(records: list[dict], source: Path, destination: Path | None,
                   allow_missing: bool = False) -> list[dict]:
    """Missing files fail before creating destination unless explicitly allowed.

    IDs/hashes only in report. Files are stored under storage_path from the database,
    so an extension alias in the source does not change the path the application reads.
    """
    verified = []
    sources = []
    for row in records:
        path, aliased = find_source(source, row['storage_path'], row['size_bytes'])
        if path is None:
            if not allow_missing:
                raise ValueError('Missing or size-mismatched file ID '+str(row['id']))
            verified.append(dict(id=row['id'], size=row['size_bytes'], missing=True))
            sources.append(None)
            continue
        check = dict(id=row['id'], size=row['size_bytes'], sha256=sha256(path))
        if aliased:
            check['extension_normalized'] = True
        verified.append(check)
        sources.append(path)
    if destination is not None:
        destination.mkdir(mode=0o700, parents=False, exist_ok=False)
        copied = set()
        for row, check, src in zip(records, verified, sources):
            if src is None:
                continue
            dst = safe_file(destination, row['storage_path'])
            dst.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            if dst not in copied:
                with src.open('rb') as reader, dst.open('xb') as writer:
                    shutil.copyfileobj(reader, writer)
                copied.add(dst)
            if dst.stat().st_size != check['size'] or sha256(dst) != check['sha256']:
                raise ValueError('File changed during copy: ID '+str(row['id']))
    return verified
