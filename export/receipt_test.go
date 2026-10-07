package export

import "testing"

func TestReceiptCanonicalVectors(t *testing.T) {
	files := []FileRecord{
		{Path: "a.txt", Mode: "100644", Size: 3, SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{Path: "run", Mode: "100755", Size: 3, SHA256: "26a66825c1fc56bd2ed71a51d320cd8be68af137ff91b978b66695af5666f560"},
	}
	const tree = "91e91527ae83d64ab5be68aa1ba610e2e7954209c96ecc29805699f35732771c"
	if got := treeDigest(files); got != tree {
		t.Fatalf("tree protocol drift: %s", got)
	}
	r := Receipt{SchemaVersion: 1, ToolVersion: "v0.1.0", SourceCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ConfigSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PolicySHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Limits: LimitsV1(), Files: files, FileCount: 2, TotalBytes: 6, TreeSHA256: tree}
	const expected = `{
  "schemaVersion": 1,
  "toolVersion": "v0.1.0",
  "sourceCommit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "configSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "policySha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
  "limits": {
    "blobBytes": 67108864,
    "totalBytes": 536870912,
    "files": 100000,
    "pathBytes": 1024,
    "receiptBytes": 16777216,
    "artifactBytes": 1073741824
  },
  "files": [
    {
      "path": "a.txt",
      "mode": "100644",
      "size": 3,
      "sha256": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    },
    {
      "path": "run",
      "mode": "100755",
      "size": 3,
      "sha256": "26a66825c1fc56bd2ed71a51d320cd8be68af137ff91b978b66695af5666f560"
    }
  ],
  "fileCount": 2,
  "totalBytes": 6,
  "treeSha256": "91e91527ae83d64ab5be68aa1ba610e2e7954209c96ecc29805699f35732771c"
}
`
	data, err := receiptBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("receipt byte protocol drift:\n%s", data)
	}
}
