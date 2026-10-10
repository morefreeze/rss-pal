import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('repair',Path(__file__).parents[1]/'repair_recent_math.py')
repair=importlib.util.module_from_spec(spec)
spec.loader.exec_module(repair)

class Args:
    old='old'
    new='new'

class RepairTests(unittest.TestCase):
    def test_summary_does_not_hide_damaged_full_body(self):
        row={'url':'https://example.org','content':'Full $x \\\\in B$ body'}
        with patch.object(repair,'tool',return_value={'content':'Brief summary'}):
            self.assertEqual(repair.compare(row,'<p>Brief summary</p>','fragment',Args())[1],'source_mismatch')
    def test_exact_converter_difference_is_repairable(self):
        row={'url':'https://example.org','content':r'$x \\in B$'}
        def convert(binary,*args): return {'content':row['content'] if binary=='old' else r'$x \in B$'}
        with patch.object(repair,'tool',side_effect=convert):
            plan,status=repair.compare(row,r'<p>$x \in B$</p>','fragment',Args())
        self.assertEqual(status,'repairable')
        self.assertEqual(plan['new_content'],r'$x \in B$')
    def test_changed_article_is_not_overwritten(self):
        row={'url':'https://example.org','content':'newer content'}
        with patch.object(repair,'tool',side_effect=[{'content':'old'},{'content':'$fixed$'}]):
            self.assertEqual(repair.compare(row,'<p>$fixed$</p>','fragment',Args())[1],'source_mismatch')
    def test_backup_is_private_and_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'backup.json'
            repair.durable(path,{'content':'original'})
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            with self.assertRaises(FileExistsError):repair.durable(path,{'content':'replacement'})
    def test_sql_literal_cannot_execute_payload(self):
        value="x'); DROP TABLE articles; --"
        encoded=repair.literal(value)
        self.assertNotIn('DROP',encoded)
        self.assertIn(value.encode().hex(),encoded)

if __name__=='__main__':unittest.main()
