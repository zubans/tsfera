import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from migrate import connection_env, database_url, dump_hash, ident, literal
from files import safe_file, transfer_files

class MigrationSafetyTests(unittest.TestCase):
    def test_connection_does_not_inherit_libpq_options(self):
        with patch.dict(os.environ, {'PGOPTIONS':'-c search_path=evil','PGSERVICE':'evil'}):
            env=connection_env('postgresql://test:p%40ss@localhost:5433/db?sslmode=require')
        self.assertNotIn('PGOPTIONS',env)
        self.assertNotIn('PGSERVICE',env)
        self.assertEqual(env['PGPASSWORD'],'p@ss')
        self.assertEqual(env['PGSSLMODE'],'require')
        self.assertEqual(env['PGDATABASE'],'db')
        with self.assertRaises(ValueError): connection_env('postgresql://localhost/db?options=bad')
    def test_quoting_and_database_replacement(self):
        self.assertEqual(ident('a"b'),'"a""b"')
        self.assertEqual(literal("a'b"),"'a''b'")
        self.assertEqual(database_url('postgresql://u:p@host/old?sslmode=require','new'),'postgresql://u:p@host/new?sslmode=require')
    def test_files_are_verified_and_existing_destination_refused(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td); src=root/'source'; src.mkdir(); (src/'one').write_bytes(b'payload')
            records=[dict(id=1,storage_path='one',size_bytes=7)]
            dst=root/'destination'
            checks=transfer_files(records,src,dst)
            self.assertEqual(checks[0]['sha256'],dump_hash(dst/'one'))
            with self.assertRaises(FileExistsError): transfer_files(records,src,dst)
            with self.assertRaises(ValueError): transfer_files([dict(id=2,storage_path='missing',size_bytes=1)],src,root/'absent')
            self.assertFalse((root/'absent').exists())
    def test_storage_path_escape_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td); (root/'escape').symlink_to('/tmp')
            for key in ['../secret','/etc/passwd','escape/secret','']:
                with self.subTest(key=key),self.assertRaises(ValueError): safe_file(root,key)

if __name__=='__main__': unittest.main()
